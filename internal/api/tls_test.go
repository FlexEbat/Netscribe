package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/store"
)

type tlsCall struct {
	names   []string
	days    int
	replace bool
}

type fakeTLS struct {
	status    TLSStatus
	statusErr error
	result    TLSResult
	genErr    error
	calls     []tlsCall
}

func (f *fakeTLS) Status(context.Context) (TLSStatus, error) { return f.status, f.statusErr }

func (f *fakeTLS) GenerateSelfSigned(_ context.Context, names []string, days int, replace bool) (TLSResult, error) {
	f.calls = append(f.calls, tlsCall{names, days, replace})
	return f.result, f.genErr
}

func newFakeTLS() *fakeTLS {
	return &fakeTLS{
		status: TLSStatus{
			Enabled: true, SelfSigned: true, Subject: "CN=panel.lan", Names: []string{"panel.lan", "10.0.0.5"},
			NotBefore: time.Date(2026, 11, 26, 11, 55, 0, 0, time.UTC), NotAfter: time.Date(2027, 11, 26, 12, 0, 0, 0, time.UTC),
			Fingerprint: "AA:BB:CC",
		},
		result: TLSResult{
			Subject: "CN=panel.lan", Names: []string{"panel.lan", "10.0.0.5"},
			NotAfter: time.Date(2027, 11, 26, 12, 0, 0, 0, time.UTC), Fingerprint: "AA:BB:CC", NeedRestart: true,
		},
	}
}

func tlsEnv(t *testing.T, ft *fakeTLS) (*env, session, session) {
	t.Helper()
	return systemEnv(t, func(o *envOptions) { o.tls = ft })
}

func jsonKeys(t *testing.T, body []byte) []string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestGetTLSIsForAdministratorsAndHoldsNoKeyOrPath(t *testing.T) {
	ft := newFakeTLS()
	e, admin, viewer := tlsEnv(t, ft)

	rec := e.as(admin, http.MethodGet, "/api/tls", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if got := strings.Join(jsonKeys(t, rec.Body.Bytes()), ","); got != "enabled,fingerprint,names,notAfter,notBefore,selfSigned,subject" {
		t.Errorf("keys = %s", got)
	}
	if body := rec.Body.String(); strings.Contains(body, "PRIVATE KEY") || strings.Contains(body, ".key") || strings.Contains(body, "/etc") {
		t.Errorf("the answer holds key material or a path: %s", body)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
	}
	if code := e.as(viewer, http.MethodGet, "/api/tls", nil).Code; code != http.StatusForbidden {
		t.Errorf("viewer: status = %d, want 403", code)
	}
	if code := e.do(http.MethodGet, "/api/tls", nil).Code; code != http.StatusUnauthorized {
		t.Errorf("no session: status = %d, want 401", code)
	}
	tok := issueToken(t, e, admin, map[string]any{"name": "ci", "scope": "write"})
	if code := e.do(http.MethodGet, "/api/tls", nil, bearer(tok.Value)).Code; code != http.StatusForbidden {
		t.Errorf("token: status = %d, want 403", code)
	}
}

func TestGetTLSAnswersAnEmptyListWhenNothingIsSet(t *testing.T) {
	ft := &fakeTLS{}
	e, admin, _ := tlsEnv(t, ft)
	rec := e.as(admin, http.MethodGet, "/api/tls", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["enabled"] != false {
		t.Errorf("enabled = %v", got["enabled"])
	}
	if names, ok := got["names"].([]any); !ok || len(names) != 0 {
		t.Errorf("names = %v, want an empty list and not null", got["names"])
	}
}

func TestGetTLSServerErrorHidesTheCause(t *testing.T) {
	ft := newFakeTLS()
	ft.statusErr = errors.New("open /etc/netscribe/tls/tls.crt: permission denied")
	e, admin, _ := tlsEnv(t, ft)
	rec := e.as(admin, http.MethodGet, "/api/tls", nil)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "/etc") {
		t.Errorf("status = %d, body %s", rec.Code, rec.Body)
	}
}

func TestSelfSignedMakesTheCertificateAndWritesTheAudit(t *testing.T) {
	ft := newFakeTLS()
	e, admin, _ := tlsEnv(t, ft)
	rec := asWith(e, admin, http.MethodPost, "/api/tls/self-signed",
		map[string]any{"names": []string{"panel.lan", "10.0.0.5"}, "days": 90, "replace": true}, withPassword(goodPassword))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if len(ft.calls) != 1 || ft.calls[0].days != 90 || !ft.calls[0].replace || strings.Join(ft.calls[0].names, ",") != "panel.lan,10.0.0.5" {
		t.Errorf("calls = %+v", ft.calls)
	}
	// The whole body: nothing but the five fields, and no key.
	if got := strings.Join(jsonKeys(t, rec.Body.Bytes()), ","); got != "fingerprint,names,needRestart,notAfter,subject" {
		t.Errorf("keys = %s", got)
	}
	if strings.Contains(rec.Body.String(), "PRIVATE KEY") {
		t.Error("the answer holds a private key")
	}
	var got TLSResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || !got.NeedRestart || got.Fingerprint != "AA:BB:CC" {
		t.Errorf("answer = %+v, %v", got, err)
	}
	entries, err := e.store.ListAudit(context.Background(), store.AuditFilter{Action: "tls_generate", Limit: 5})
	if err != nil || len(entries) != 1 || entries[0].Result != "ok" {
		t.Fatalf("audit = %+v, %v", entries, err)
	}
	if d := entries[0].Detail; !strings.Contains(d, "AA:BB:CC") || !strings.Contains(d, "panel.lan") || strings.Contains(d, "PRIVATE KEY") {
		t.Errorf("audit detail = %q", d)
	}
}

func TestSelfSignedAcceptsAnEmptyBody(t *testing.T) {
	ft := newFakeTLS()
	e, admin, _ := tlsEnv(t, ft)
	rec := asWith(e, admin, http.MethodPost, "/api/tls/self-signed", nil, withPassword(goodPassword))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if len(ft.calls) != 1 || len(ft.calls[0].names) != 0 || ft.calls[0].days != 0 || ft.calls[0].replace {
		t.Errorf("calls = %+v, want the defaults", ft.calls)
	}
}

func TestSelfSignedNeedsTheAdministratorsPassword(t *testing.T) {
	ft := newFakeTLS()
	e, admin, viewer := tlsEnv(t, ft)

	if code := asWith(e, admin, http.MethodPost, "/api/tls/self-signed", nil, withPassword("not the password")).Code; code != http.StatusForbidden {
		t.Errorf("wrong password: status = %d, want 403", code)
	}
	if code := asWith(e, admin, http.MethodPost, "/api/tls/self-signed", nil).Code; code != http.StatusBadRequest {
		t.Errorf("no password: status = %d, want 400", code)
	}
	if code := asWith(e, viewer, http.MethodPost, "/api/tls/self-signed", nil, withPassword(goodPassword)).Code; code != http.StatusForbidden {
		t.Errorf("viewer: status = %d, want 403", code)
	}
	if code := e.do(http.MethodPost, "/api/tls/self-signed", nil, withPassword(goodPassword)).Code; code != http.StatusUnauthorized {
		t.Errorf("no session: status = %d, want 401", code)
	}
	if len(ft.calls) != 0 {
		t.Errorf("a certificate was made %d times without a right password", len(ft.calls))
	}
}

func TestSelfSignedIsNotForTokens(t *testing.T) {
	ft := newFakeTLS()
	e, admin, _ := tlsEnv(t, ft)
	tok := issueToken(t, e, admin, map[string]any{"name": "ci", "scope": "write"})
	if code := e.do(http.MethodPost, "/api/tls/self-signed", map[string]any{}, bearer(tok.Value), withPassword(goodPassword)).Code; code != http.StatusForbidden {
		t.Errorf("token: status = %d, want 403", code)
	}
	if len(ft.calls) != 0 {
		t.Error("a token made a certificate")
	}
}

func TestSelfSignedAnswersARefusalAndAConflict(t *testing.T) {
	ft := newFakeTLS()
	e, admin, _ := tlsEnv(t, ft)

	ft.genErr = TLSRefusal(`"bad_name!": name is not a host name or an address`)
	rec := asWith(e, admin, http.MethodPost, "/api/tls/self-signed", map[string]any{"names": []string{"bad_name!"}}, withPassword(goodPassword))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "not a host name or an address") {
		t.Errorf("refusal: status = %d, body %s", rec.Code, rec.Body)
	}

	ft.genErr = TLSConflict("a certificate is set already: send replace to make a new one")
	rec = asWith(e, admin, http.MethodPost, "/api/tls/self-signed", map[string]any{}, withPassword(goodPassword))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "replace") {
		t.Errorf("conflict: status = %d, body %s", rec.Code, rec.Body)
	}
	entries, err := e.store.ListAudit(context.Background(), store.AuditFilter{Action: "tls_generate", Limit: 5})
	if err != nil || len(entries) != 2 {
		t.Fatalf("audit = %+v, %v", entries, err)
	}
	for _, en := range entries {
		if en.Result != "denied" {
			t.Errorf("result = %q, want denied", en.Result)
		}
	}
}

func TestSelfSignedServerErrorHidesTheCause(t *testing.T) {
	ft := newFakeTLS()
	ft.genErr = errors.New("write /etc/netscribe/tls/tls.key: no space left on device")
	e, admin, _ := tlsEnv(t, ft)
	rec := asWith(e, admin, http.MethodPost, "/api/tls/self-signed", map[string]any{}, withPassword(goodPassword))
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "/etc") {
		t.Errorf("status = %d, body %s", rec.Code, rec.Body)
	}
	entries, err := e.store.ListAudit(context.Background(), store.AuditFilter{Action: "tls_generate", Limit: 5})
	if err != nil || len(entries) != 1 || entries[0].Result != "error" {
		t.Fatalf("audit = %+v, %v", entries, err)
	}
}

func TestSelfSignedRefusesABrokenBody(t *testing.T) {
	ft := newFakeTLS()
	e, admin, _ := tlsEnv(t, ft)
	rec := asWith(e, admin, http.MethodPost, "/api/tls/self-signed", "{not json", withPassword(goodPassword))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if len(ft.calls) != 0 {
		t.Error("a broken body made a certificate")
	}
}

func TestTLSRoutesNeedTheSystemManagePermission(t *testing.T) {
	for _, rt := range fullServer().routes() {
		if rt.Path == "/api/tls" || rt.Path == "/api/tls/self-signed" {
			if rt.Permission != model.PermSystemManage {
				t.Errorf("%s %s needs %s, want system:manage", rt.Method, rt.Path, rt.Permission)
			}
		}
	}
}
