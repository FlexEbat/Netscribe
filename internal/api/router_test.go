package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/FlexEbat/Netscribe/internal/auth"
	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/store"
)

const goodPassword = "correct horse battery"

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func authConfig() config.AuthConfig {
	return config.AuthConfig{
		SessionIdleTimeout: config.Duration(8 * time.Hour),
		SessionMaxAge:      config.Duration(168 * time.Hour),
		MinPasswordLength:  12,
		MaxFailedLogins:    5,
		LockoutDuration:    config.Duration(15 * time.Minute),
		Argon2:             config.Argon2Config{MemoryKiB: 19456, Iterations: 2, Parallelism: 1},
	}
}

func testStatic() fstest.MapFS {
	return fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>spa</title>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
}

// env is a router over an in-memory database, a shared fake clock and a log buffer.
type env struct {
	t       *testing.T
	store   *store.Store
	svc     *auth.Service
	clk     *fakeClock
	logs    *bytes.Buffer
	handler http.Handler
	logins  int
}

type envOptions struct {
	noUsers bool
	trusted []string
	extra   []Route
	static  fstest.MapFS
	audit   Auditor
}

func newEnv(t *testing.T, mods ...func(*envOptions)) *env {
	t.Helper()
	o := envOptions{static: testStatic()}
	for _, m := range mods {
		m(&o)
	}

	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	clk := &fakeClock{t: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}
	st.SetClock(clk.now)

	svc, err := auth.NewService(st, authConfig(), clk.now)
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	var trusted []netip.Prefix
	for _, p := range o.trusted {
		trusted = append(trusted, netip.MustParsePrefix(p))
	}
	var aud Auditor = st
	if o.audit != nil {
		aud = o.audit
	}
	s := newServer(Options{
		Static: o.static, Auth: svc, Audit: aud, Repo: st, TrustedProxies: trusted, Now: clk.now,
		Logger: slog.New(slog.NewTextHandler(logs, nil)),
	})
	e := &env{t: t, store: st, svc: svc, clk: clk, logs: logs, handler: s.handler(o.static, append(s.routes(), o.extra...))}
	if !o.noUsers {
		e.addUser("admin", model.RoleAdmin)
	}
	return e
}

func (e *env) addUser(name string, role model.Role) model.User {
	e.t.Helper()
	u, err := e.svc.CreateUser(context.Background(), auth.NewUser{Username: name, Role: role, Password: goodPassword})
	if err != nil {
		e.t.Fatal(err)
	}
	return u
}

func (e *env) do(method, target string, body any, mods ...func(*http.Request)) *httptest.ResponseRecorder {
	e.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			e.t.Fatal(err)
		}
		rd = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, target, rd)
	for _, m := range mods {
		m(req)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

// session is a signed-in client.
type session struct {
	cookie *http.Cookie
	csrf   string
}

func (e *env) login(name, password string, mods ...func(*http.Request)) (session, *httptest.ResponseRecorder) {
	e.t.Helper()
	rec := e.do(http.MethodPost, "/api/auth/login", map[string]string{"username": name, "password": password}, mods...)
	var s session
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie && c.Value != "" {
			s.cookie = c
		}
	}
	var me meResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &me) // failed logins have no such body
	s.csrf = me.CSRFToken
	return s, rec
}

// mustLogin signs in from a fresh client address each time, so many sign-ins in one
// test do not run into the per-address limit.
func (e *env) mustLogin(name string) session {
	e.t.Helper()
	e.logins++
	s, rec := e.login(name, goodPassword, from("198.18."+strconv.Itoa(e.logins/250)+"."+strconv.Itoa(e.logins%250+1)+":1"))
	if rec.Code != http.StatusOK || s.cookie == nil {
		e.t.Fatalf("login %s: %d %s", name, rec.Code, rec.Body)
	}
	return s
}

// as sends a request with the session cookie and, for unsafe methods, its CSRF token.
func (e *env) as(s session, method, target string, body any) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.do(method, target, body, func(r *http.Request) {
		r.AddCookie(s.cookie)
		if method != http.MethodGet && method != http.MethodHead {
			r.Header.Set(csrfHeader, s.csrf)
		}
	})
}

func from(addr string) func(*http.Request) {
	return func(r *http.Request) { r.RemoteAddr = addr }
}

func withHeader(k, v string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set(k, v) }
}

func okHandler(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }

// permissionRoutes adds one route per right of section 9.3.
func permissionRoutes() []Route {
	return []Route{
		route(http.MethodGet, "/api/test/topology", model.PermTopologyRead, okHandler),
		route(http.MethodGet, "/api/test/export", model.PermExportRun, okHandler),
		route(http.MethodGet, "/api/test/tokens", model.PermTokensManage, okHandler),
		route(http.MethodPost, "/api/test/scans", model.PermScansRun, okHandler),
		route(http.MethodPut, "/api/test/devices", model.PermDevicesWrite, okHandler),
		route(http.MethodGet, "/api/test/audit", model.PermAuditRead, okHandler),
		route(http.MethodDelete, "/api/test/users", model.PermUsersManage, okHandler),
	}
}

func withRoutes(r ...Route) func(*envOptions) {
	return func(o *envOptions) { o.extra = append(o.extra, r...) }
}

func TestRegisterPanicsOnRouteWithoutPermission(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("register() accepted a route without a permission")
		}
	}()
	e := newEnv(t)
	s := newServer(Options{Auth: e.svc, Audit: e.store})
	s.register(chi.NewRouter(), []Route{route(http.MethodGet, "/x", "", okHandler)})
}

func TestNewRouterRequiresAuth(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewRouter() accepted a missing auth service")
		}
	}()
	NewRouter(Options{Static: testStatic()})
}

func TestEveryRegisteredRouteHasAPermission(t *testing.T) {
	e := newEnv(t)
	s := newServer(Options{Auth: e.svc, Audit: e.store, Repo: e.store})
	for _, rt := range s.routes() {
		if rt.Permission == "" {
			t.Errorf("%s %s has no permission", rt.Method, rt.Path)
		}
	}
}

// TestPermissionsAcrossTheWholeRouteTable walks every route and checks the three cases
// of section 10: no sign-in is 401, a role without the right is 403, a role with it is neither.
func TestPermissionsAcrossTheWholeRouteTable(t *testing.T) {
	e := newEnv(t, withRoutes(permissionRoutes()...))
	s := newServer(Options{Auth: e.svc, Audit: e.store, Repo: e.store})
	table := append(s.routes(), permissionRoutes()...)

	roles := []model.Role{model.RoleViewer, model.RoleOperator, model.RoleAdmin}
	for _, r := range roles {
		e.addUser("u-"+string(r), r)
	}

	for _, rt := range table {
		name := rt.Method + " " + rt.Path
		t.Run(name, func(t *testing.T) {
			if rt.Permission == permPublic {
				if code := e.do(rt.Method, rt.Path, nil).Code; code == http.StatusUnauthorized || code == http.StatusForbidden {
					t.Errorf("public route answered %d without a session", code)
				}
				return
			}

			if code := e.do(rt.Method, rt.Path, nil).Code; code != http.StatusUnauthorized {
				t.Errorf("without a session: %d, want 401", code)
			}
			for _, role := range roles {
				sess := e.mustLogin("u-" + string(role)) // a fresh session each time: logout is one of the routes
				code := e.as(sess, rt.Method, rt.Path, nil).Code
				allowed := rt.Permission == permSession || auth.Can(role, rt.Permission)
				switch {
				case allowed && (code == http.StatusUnauthorized || code == http.StatusForbidden):
					t.Errorf("role %s holds %s but got %d", role, rt.Permission, code)
				case !allowed && code != http.StatusForbidden:
					t.Errorf("role %s lacks %s but got %d, want 403", role, rt.Permission, code)
				}
			}
		})
	}
}

func TestEveryRightHasARouteThatDeniesSomeRole(t *testing.T) {
	// Guards the test above: a table where every right is allowed for everybody proves nothing.
	denied := map[model.Permission]bool{}
	for _, rt := range permissionRoutes() {
		for _, role := range []model.Role{model.RoleViewer, model.RoleOperator, model.RoleAdmin} {
			if !auth.Can(role, rt.Permission) {
				denied[rt.Permission] = true
			}
		}
	}
	for _, p := range []model.Permission{model.PermScansRun, model.PermDevicesWrite, model.PermAuditRead, model.PermUsersManage} {
		if !denied[p] {
			t.Errorf("no role is denied %s in the test table", p)
		}
	}
}

func TestHealthzIsPublicAndNeedsNoUsers(t *testing.T) {
	e := newEnv(t, func(o *envOptions) { o.noUsers = true })
	rec := e.do(http.MethodGet, "/healthz", nil)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "ok" {
		t.Errorf("GET /healthz = %d %q", rec.Code, rec.Body)
	}
}

func TestNoUsersEverythingElseIs503(t *testing.T) {
	e := newEnv(t, func(o *envOptions) { o.noUsers = true })
	for _, tt := range []struct{ method, path string }{
		{"GET", "/"}, {"GET", "/inventory"}, {"GET", "/assets/app.js"}, {"GET", "/login"},
		{"GET", "/api/auth/me"}, {"POST", "/api/auth/login"}, {"GET", "/api/nope"}, {"DELETE", "/api/auth/logout"},
	} {
		rec := e.do(tt.method, tt.path, nil)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s = %d, want 503", tt.method, tt.path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `no users: run "netscribe user add"`) {
			t.Errorf("%s %s body = %q", tt.method, tt.path, rec.Body)
		}
	}

	e.addUser("admin", model.RoleAdmin)
	if code := e.do(http.MethodGet, "/", nil).Code; code != http.StatusOK {
		t.Errorf("GET / after the first user = %d, want 200", code)
	}
}

func TestSPAFallback(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/", "/inventory", "/devices/3", "/index.html", "/assets/missing.js", "/login"} {
		rec := e.do(http.MethodGet, p, nil)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<title>spa</title>") {
			t.Errorf("GET %s = %d %q, want index.html", p, rec.Code, rec.Body)
		}
	}
}

func TestStaticAssetIsServed(t *testing.T) {
	rec := newEnv(t).do(http.MethodGet, "/assets/app.js", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "console.log(1)" {
		t.Fatalf("GET /assets/app.js = %d %q", rec.Code, rec.Body)
	}
}

func TestPathTraversalDoesNotEscapeStaticRoot(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/../go.mod", "/assets/../../go.mod", "/%2e%2e/go.mod", "/..%2fgo.mod"} {
		rec := e.do(http.MethodGet, p, nil)
		if strings.Contains(rec.Body.String(), "module ") {
			t.Errorf("GET %s leaked a file outside the static root", p)
		}
	}
}

func TestUnknownAPIPathIsJSON404(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/api/nope", "/api"} {
		rec := e.do(http.MethodGet, p, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s = %d, want 404", p, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("GET %s Content-Type = %q", p, ct)
		}
		if strings.Contains(rec.Body.String(), "<title>") {
			t.Errorf("GET %s returned the web interface", p)
		}
	}
}

func TestPostToUnknownPathIs404NotIndex(t *testing.T) {
	rec := newEnv(t).do(http.MethodPost, "/inventory", "x")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestMissingBuildGives503(t *testing.T) {
	e := newEnv(t, func(o *envOptions) { o.static = fstest.MapFS{} })
	if rec := e.do(http.MethodGet, "/", nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestSecurityHeadersOnEveryKindOfResponse(t *testing.T) {
	e := newEnv(t, withRoutes(permissionRoutes()...))
	viewer := e.addUser("vera", model.RoleViewer)
	_ = viewer
	vs := e.mustLogin("vera")

	want := map[string]string{
		"Content-Security-Policy": "default-src 'self'; script-src 'self'; style-src 'self'; style-src-attr 'unsafe-inline'; " +
			"img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'",
		"X-Content-Type-Options":     "nosniff",
		"Referrer-Policy":            "no-referrer",
		"X-Frame-Options":            "DENY",
		"Permissions-Policy":         "camera=(), microphone=(), geolocation=()",
		"Cross-Origin-Opener-Policy": "same-origin",
	}
	cases := map[string]*httptest.ResponseRecorder{
		"page":               e.do(http.MethodGet, "/", nil),
		"spa route":          e.do(http.MethodGet, "/inventory", nil),
		"static asset":       e.do(http.MethodGet, "/assets/app.js", nil),
		"healthz":            e.do(http.MethodGet, "/healthz", nil),
		"api 404":            e.do(http.MethodGet, "/api/nope", nil),
		"api 401":            e.do(http.MethodGet, "/api/auth/me", nil),
		"api 403":            e.as(vs, http.MethodPost, "/api/test/scans", nil),
		"api 400":            e.do(http.MethodPost, "/api/auth/login", "not json"),
		"login failure":      e.do(http.MethodPost, "/api/auth/login", map[string]string{"username": "x", "password": "y"}),
		"login success":      func() *httptest.ResponseRecorder { _, r := e.login("admin", goodPassword); return r }(),
		"method not allowed": e.do(http.MethodDelete, "/api/auth/login", nil),
		"api 200":            e.as(vs, http.MethodGet, "/api/auth/me", nil),
	}
	for name, rec := range cases {
		for k, v := range want {
			if got := rec.Header().Get(k); got != v {
				t.Errorf("%s: %s = %q, want %q", name, k, got, v)
			}
		}
		if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
			t.Errorf("%s: HSTS over plain HTTP = %q", name, got)
		}
	}
	for _, name := range []string{"api 404", "api 401", "api 403", "api 400", "login failure", "login success", "api 200"} {
		if got := cases[name].Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control = %q, want no-store", name, got)
		}
	}
	for _, name := range []string{"page", "spa route", "static asset"} {
		if got := cases[name].Header().Get("Cache-Control"); got == "no-store" {
			t.Errorf("%s must stay cacheable", name)
		}
	}
}

func TestSecurityHeadersOnTheNoUsersAnswer(t *testing.T) {
	rec := newEnv(t, func(o *envOptions) { o.noUsers = true }).do(http.MethodGet, "/", nil)
	if rec.Header().Get("X-Frame-Options") != "DENY" || rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("the 503 answer has no security headers")
	}
}

func TestHSTSOnlyOverHTTPS(t *testing.T) {
	e := newEnv(t, func(o *envOptions) { o.trusted = []string{"10.0.0.0/8"} })
	if got := e.do(http.MethodGet, "/healthz", nil).Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS over plain HTTP = %q", got)
	}
	tls := httptest.NewRequest(http.MethodGet, "https://example.test/healthz", nil)
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, tls)
	if got := rec.Header().Get("Strict-Transport-Security"); got != "max-age=31536000" {
		t.Errorf("HSTS over TLS = %q", got)
	}
	viaProxy := e.do(http.MethodGet, "/healthz", nil, from("10.1.2.3:5000"), withHeader("X-Forwarded-Proto", "https"))
	if got := viaProxy.Header().Get("Strict-Transport-Security"); got == "" {
		t.Error("no HSTS behind a trusted proxy that reports https")
	}
	spoofed := e.do(http.MethodGet, "/healthz", nil, from("203.0.113.9:5000"), withHeader("X-Forwarded-Proto", "https"))
	if got := spoofed.Header().Get("Strict-Transport-Security"); got != "" {
		t.Error("an untrusted client could switch HSTS on")
	}
}

func TestBodyLimit(t *testing.T) {
	var readErr error
	h := limitBody(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("a", maxBodyBytes+1))))
	if readErr == nil {
		t.Fatal("a body above 1 MiB was read without error")
	}
	readErr = nil
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("a", maxBodyBytes))))
	if readErr != nil {
		t.Fatalf("a body of exactly 1 MiB failed: %v", readErr)
	}
}

// failingAudit makes every audit write fail.
type failingAudit struct{}

func (failingAudit) AddAudit(context.Context, model.AuditEntry) error { return errors.New("disk full") }
