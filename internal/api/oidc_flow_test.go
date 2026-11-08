package api

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/auth"
	"github.com/FlexEbat/Netscribe/internal/auth/authtest"
	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/store"
)

const flowClient = "netscribe-flow"

// flowEnv is the real router, service and store with a real single sign-on over a provider
// that runs in the test: the pieces the other tests replace by fakes.
func flowEnv(t *testing.T, edit func(*config.OIDCConfig)) (*env, *authtest.IDP) {
	t.Helper()
	idp := authtest.New(t)
	cfg := config.OIDCConfig{
		Issuer: idp.URL, ClientID: flowClient, ClientSecret: "flow-secret",
		RedirectURL: "http://127.0.0.1/api/auth/oidc/callback", Scopes: []string{"openid", "email"},
		AllowedEmailDomains: []string{"example.com"}, AutoCreate: true, DefaultRole: "viewer",
		RoleClaim: "groups", RoleMap: map[string]string{"ops": "operator"},
	}
	if edit != nil {
		edit(&cfg)
	}
	e := newEnv(t, func(o *envOptions) {
		o.oidcFor = func(svc *auth.Service, st *store.Store) OIDCService {
			return auth.NewOIDC(cfg, st, svc, nil, nil)
		}
	})
	return e, idp
}

// startFlow follows /login to the provider and hands back what the provider received.
func startFlow(t *testing.T, e *env) (cookie *http.Cookie, q url.Values) {
	t.Helper()
	rec := e.do(http.MethodGet, "/api/auth/oidc/login", nil)
	if rec.Code != http.StatusFound {
		t.Fatalf("login answered %d, want 302: %s", rec.Code, rec.Body)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if c := cookieNamed(rec, oidcCookie); c != nil {
		cookie = c
	} else {
		t.Fatal("no flow cookie")
	}
	return cookie, loc.Query()
}

func TestSingleSignOnFromTheFirstClickToASession(t *testing.T) {
	e, idp := flowEnv(t, nil)
	ctx := context.Background()

	cookie, q := startFlow(t, e)
	claims := idp.Claims(q.Get("nonce"), "subject-1", "Jane@Example.com", flowClient)
	claims["groups"] = []string{"ops"}
	idp.Register("the-code", q.Get("code_challenge"), claims, nil)

	rec := e.do(http.MethodGet, "/api/auth/oidc/callback?state="+url.QueryEscape(q.Get("state"))+"&code=the-code", nil,
		func(r *http.Request) { r.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value}) })
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("callback answered %d to %q, want 303 to /", rec.Code, rec.Header().Get("Location"))
	}
	sc := cookieNamed(rec, sessionCookie)
	if sc == nil || sc.Value == "" {
		t.Fatal("no session cookie after a good sign in")
	}

	me := e.do(http.MethodGet, "/api/auth/me", nil, func(r *http.Request) { r.AddCookie(sc) })
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), `"operator"`) || !strings.Contains(me.Body.String(), "jane-") {
		t.Fatalf("me answered %d %s", me.Code, me.Body)
	}

	// The account is in the real store, found by issuer and subject, and has no password.
	u, err := e.store.FindIdentityUser(ctx, idp.URL, "subject-1")
	if err != nil {
		t.Fatalf("the identity was not stored: %v", err)
	}
	if _, rec := e.login(u.Username, goodPassword); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a password sign in to an account of single sign-on answered %d, want 401", rec.Code)
	}

	got := strings.Join(oidcAudit(t, e), " | ")
	if !strings.Contains(got, "oidc_user_create:ok") || !strings.Contains(got, "oidc_login:ok") {
		t.Errorf("audit = %s", got)
	}

	// The same code does not work twice, even with a fresh flow cookie of the same sign in.
	again := e.do(http.MethodGet, "/api/auth/oidc/callback?state="+url.QueryEscape(q.Get("state"))+"&code=the-code", nil,
		func(r *http.Request) { r.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value}) })
	if again.Code != http.StatusSeeOther || again.Header().Get("Location") != ssoFailedPath {
		t.Fatalf("a replayed code answered %d to %q", again.Code, again.Header().Get("Location"))
	}
}

func TestSingleSignOnRefusesAStateFromAnotherSignIn(t *testing.T) {
	e, idp := flowEnv(t, nil)
	cookie, q := startFlow(t, e)
	idp.Register("c", q.Get("code_challenge"), idp.Claims(q.Get("nonce"), "s", "s@example.com", flowClient), nil)

	rec := e.do(http.MethodGet, "/api/auth/oidc/callback?state=somebody-elses-state&code=c", nil,
		func(r *http.Request) { r.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value}) })
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != ssoFailedPath {
		t.Fatalf("answered %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	if sc := cookieNamed(rec, sessionCookie); sc != nil && sc.Value != "" {
		t.Fatal("a session was opened for a state that is not the flow's")
	}
	if n, _ := e.store.CountUsers(context.Background()); n != 1 { // only the admin of the environment
		t.Fatalf("users = %d, a refused sign in created one", n)
	}
}

func TestSingleSignOnKeepsAnExistingLocalUserApart(t *testing.T) {
	e, idp := flowEnv(t, nil)
	cookie, q := startFlow(t, e)
	// The e-mail address looks like the local administrator's name; the account must stay alone.
	idp.Register("c", q.Get("code_challenge"), idp.Claims(q.Get("nonce"), "s-admin", "admin@example.com", flowClient), nil)
	rec := e.do(http.MethodGet, "/api/auth/oidc/callback?state="+url.QueryEscape(q.Get("state"))+"&code=c", nil,
		func(r *http.Request) { r.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value}) })
	sc := cookieNamed(rec, sessionCookie)
	if rec.Code != http.StatusSeeOther || sc == nil {
		t.Fatalf("answered %d", rec.Code)
	}
	me := e.do(http.MethodGet, "/api/auth/me", nil, func(r *http.Request) { r.AddCookie(sc) })
	if strings.Contains(me.Body.String(), `"admin"`) {
		t.Fatalf("the person became the local administrator: %s", me.Body)
	}
}
