package api

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/auth"
	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/store"
)

// fakeOIDC stands in for the provider side: the flow of the service has its own tests in
// internal/auth, these check what the routes do with its answers.
type fakeOIDC struct {
	target   string
	beginErr error
	result   auth.OIDCResult
	err      error

	gotFlow  auth.Flow
	gotState string
	gotCode  string
	calls    int
}

func (f *fakeOIDC) Name() string { return "Acme" }

func (f *fakeOIDC) Begin(context.Context) (string, auth.Flow, error) {
	return f.target, auth.Flow{State: "st", Nonce: "no", Verifier: "ve"}, f.beginErr
}

func (f *fakeOIDC) Complete(_ context.Context, fl auth.Flow, state, code, _, _ string) (auth.OIDCResult, error) {
	f.calls++
	f.gotFlow, f.gotState, f.gotCode = fl, state, code
	return f.result, f.err
}

func goodResult(created bool) auth.OIDCResult {
	return auth.OIDCResult{
		Session: auth.Session{ID: "session-value-from-oidc", CSRFToken: "csrf", ExpiresAt: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)},
		User:    model.User{ID: 7, Username: "jane-1a2b3c4d", Role: model.RoleViewer},
		Created: created,
	}
}

func flowCookie(v string) func(*http.Request) {
	return func(r *http.Request) { r.AddCookie(&http.Cookie{Name: oidcCookie, Value: v}) }
}

func oidcAudit(t *testing.T, e *env) []string {
	t.Helper()
	entries, err := e.store.ListAudit(context.Background(), store.AuditFilter{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, en := range entries {
		out = append(out, en.Action+":"+en.Result+":"+en.Detail)
	}
	return out
}

func cookieNamed(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestProvidersAnswersAnEmptyObjectWithoutSingleSignOn(t *testing.T) {
	e := newEnv(t)
	rec := e.do(http.MethodGet, "/api/auth/providers", nil)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "{}" {
		t.Fatalf("got %d %q, want 200 {}", rec.Code, rec.Body)
	}
	for _, p := range []string{"/api/auth/oidc/login", "/api/auth/oidc/callback"} {
		if rec := e.do(http.MethodGet, p, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s answered %d with single sign-on off, want 404", p, rec.Code)
		}
	}
}

func TestProvidersNamesTheButton(t *testing.T) {
	e := newEnv(t, func(o *envOptions) { o.oidc = &fakeOIDC{} })
	rec := e.do(http.MethodGet, "/api/auth/providers", nil)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `"oidc"`) || !strings.Contains(body, `"Acme"`) {
		t.Fatalf("got %d %q", rec.Code, body)
	}
	if strings.Contains(body, "http") {
		t.Errorf("the page learns an address from the providers answer: %s", body)
	}
}

func TestOIDCLoginRedirectsAndKeepsTheFlowInAShortCookie(t *testing.T) {
	f := &fakeOIDC{target: "https://idp.example.com/authorize?client_id=x"}
	e := newEnv(t, func(o *envOptions) { o.oidc = f })
	rec := e.do(http.MethodGet, "/api/auth/oidc/login", nil)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != f.target {
		t.Fatalf("got %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	c := cookieNamed(rec, oidcCookie)
	if c == nil {
		t.Fatal("no flow cookie")
	}
	if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/api/auth/oidc/" || c.MaxAge != 600 || c.Value != "st.no.ve" {
		t.Errorf("cookie = %+v", c)
	}
	if c.Secure {
		t.Error("the cookie is Secure over plain HTTP")
	}
	https := e.do(http.MethodGet, "/api/auth/oidc/login", nil, func(r *http.Request) { r.TLS = &tls.ConnectionState{} })
	if c := cookieNamed(https, oidcCookie); c == nil || !c.Secure {
		t.Errorf("over HTTPS the cookie must be Secure: %+v", c)
	}
}

func TestOIDCLoginWhenTheProviderIsDownGoesToTheFailedPage(t *testing.T) {
	e := newEnv(t, func(o *envOptions) { o.oidc = &fakeOIDC{beginErr: errors.New("dial tcp: refused")} })
	rec := e.do(http.MethodGet, "/api/auth/oidc/login", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != ssoFailedPath {
		t.Fatalf("got %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	if cookieNamed(rec, oidcCookie) != nil && cookieNamed(rec, oidcCookie).Value != "" {
		t.Error("a flow cookie was set although nothing began")
	}
	if strings.Contains(rec.Body.String(), "refused") {
		t.Errorf("the page shows the error: %s", rec.Body)
	}
}

func TestOIDCCallbackSuccessOpensASessionAndGoesHome(t *testing.T) {
	f := &fakeOIDC{result: goodResult(true)}
	e := newEnv(t, func(o *envOptions) { o.oidc = f })
	rec := e.do(http.MethodGet, "/api/auth/oidc/callback?state=st&code=abc&next=https://evil.example.com&redirect=/x", nil, flowCookie("st.no.ve"))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("got %d to %q, want 303 to /", rec.Code, rec.Header().Get("Location"))
	}
	sc := cookieNamed(rec, sessionCookie)
	if sc == nil || sc.Value != "session-value-from-oidc" || !sc.HttpOnly || sc.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie = %+v", sc)
	}
	if oc := cookieNamed(rec, oidcCookie); oc == nil || oc.MaxAge >= 0 {
		t.Errorf("the flow cookie was not cleared: %+v", oc)
	}
	if f.gotFlow != (auth.Flow{State: "st", Nonce: "no", Verifier: "ve"}) || f.gotState != "st" || f.gotCode != "abc" {
		t.Errorf("the service got %+v %q %q", f.gotFlow, f.gotState, f.gotCode)
	}
	got := strings.Join(oidcAudit(t, e), " | ")
	if !strings.Contains(got, "oidc_login:ok") || !strings.Contains(got, "oidc_user_create:ok") {
		t.Errorf("audit = %s", got)
	}
}

func TestOIDCCallbackOfAKnownUserWritesNoCreateEntry(t *testing.T) {
	e := newEnv(t, func(o *envOptions) { o.oidc = &fakeOIDC{result: goodResult(false)} })
	e.do(http.MethodGet, "/api/auth/oidc/callback?state=st&code=abc", nil, flowCookie("st.no.ve"))
	got := strings.Join(oidcAudit(t, e), " | ")
	if !strings.Contains(got, "oidc_login:ok") || strings.Contains(got, "oidc_user_create") {
		t.Errorf("audit = %s", got)
	}
}

func TestOIDCCallbackRefusalsAllEndAtTheSamePage(t *testing.T) {
	cases := map[string]struct {
		url    string
		cookie func(*http.Request)
		err    error
	}{
		"no cookie":        {url: "/api/auth/oidc/callback?state=st&code=abc"},
		"malformed cookie": {url: "/api/auth/oidc/callback?state=st&code=abc", cookie: flowCookie("junk")},
		"provider error":   {url: "/api/auth/oidc/callback?error=access_denied&state=st", cookie: flowCookie("st.no.ve")},
		"refused":          {url: "/api/auth/oidc/callback?state=st&code=abc", cookie: flowCookie("st.no.ve"), err: auth.ErrOIDCDenied},
		"internal error":   {url: "/api/auth/oidc/callback?state=st&code=abc", cookie: flowCookie("st.no.ve"), err: errors.New("database is locked")},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := &fakeOIDC{err: tc.err}
			e := newEnv(t, func(o *envOptions) { o.oidc = f })
			var mods []func(*http.Request)
			if tc.cookie != nil {
				mods = append(mods, tc.cookie)
			}
			rec := e.do(http.MethodGet, tc.url, nil, mods...)
			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != ssoFailedPath {
				t.Fatalf("got %d to %q", rec.Code, rec.Header().Get("Location"))
			}
			if sc := cookieNamed(rec, sessionCookie); sc != nil && sc.Value != "" {
				t.Error("a refused sign in set a session cookie")
			}
			if strings.Contains(rec.Body.String(), "database") {
				t.Errorf("the page shows the error: %s", rec.Body)
			}
			if got := strings.Join(oidcAudit(t, e), " | "); !strings.Contains(got, "oidc_login_failed:denied") {
				t.Errorf("audit = %s", got)
			}
		})
	}
}

func TestOIDCCallbackWithoutACookieNeverAsksTheService(t *testing.T) {
	f := &fakeOIDC{result: goodResult(false)}
	e := newEnv(t, func(o *envOptions) { o.oidc = f })
	e.do(http.MethodGet, "/api/auth/oidc/callback?state=st&code=abc", nil)
	if f.calls != 0 {
		t.Fatalf("the service was asked %d times without a flow cookie", f.calls)
	}
}

func TestOIDCCallbackIsLimitedLikeASignIn(t *testing.T) {
	f := &fakeOIDC{err: auth.ErrOIDCDenied}
	e := newEnv(t, func(o *envOptions) { o.oidc = f })
	for i := 0; i < loginAttemptsPerMinute+3; i++ {
		e.do(http.MethodGet, "/api/auth/oidc/callback?state=st&code=abc", nil, flowCookie("st.no.ve"))
	}
	if f.calls > loginAttemptsPerMinute {
		t.Fatalf("the service was asked %d times, the limit is %d", f.calls, loginAttemptsPerMinute)
	}
}
