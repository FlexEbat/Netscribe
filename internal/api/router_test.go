package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/go-chi/chi/v5"

	"github.com/FlexEbat/Netscribe/internal/model"
)

func testStatic() fstest.MapFS {
	return fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>spa</title>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
}

func do(h http.Handler, method, target string, body io.Reader) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, body))
	return rec
}

func TestRegisterPanicsOnRouteWithoutPermission(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("register() accepted a route without a permission")
		}
	}()
	register(chi.NewRouter(), []Route{route(http.MethodGet, "/x", "", healthz)})
}

func TestRegisterAcceptsPublicAndPermissionRoutes(t *testing.T) {
	register(chi.NewRouter(), []Route{
		route(http.MethodGet, "/a", permPublic, healthz),
		route(http.MethodGet, "/b", model.PermTopologyRead, healthz),
	})
}

func TestProtectedRouteStaysClosedWithoutAuth(t *testing.T) {
	r := chi.NewRouter()
	register(r, []Route{route(http.MethodGet, "/secret", model.PermTopologyRead, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("data"))
	})})
	rec := do(r, http.MethodGet, "/secret", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "data") {
		t.Error("the handler of a protected route ran")
	}
}

func TestHealthzIsPublic(t *testing.T) {
	rec := do(NewRouter(Options{Static: testStatic()}), http.MethodGet, "/healthz", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestEveryRegisteredRouteHasAPermission(t *testing.T) {
	for _, rt := range routes() {
		if rt.Permission == "" {
			t.Errorf("%s %s has no permission", rt.Method, rt.Path)
		}
	}
}

func TestSPAFallback(t *testing.T) {
	h := NewRouter(Options{Static: testStatic()})
	for _, p := range []string{"/", "/inventory", "/devices/3", "/index.html", "/assets/missing.js"} {
		rec := do(h, http.MethodGet, p, nil)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<title>spa</title>") {
			t.Errorf("GET %s = %d %q, want index.html", p, rec.Code, rec.Body.String())
		}
	}
}

func TestStaticAssetIsServed(t *testing.T) {
	rec := do(NewRouter(Options{Static: testStatic()}), http.MethodGet, "/assets/app.js", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "console.log(1)" {
		t.Fatalf("GET /assets/app.js = %d %q", rec.Code, rec.Body.String())
	}
}

func TestPathTraversalDoesNotEscapeStaticRoot(t *testing.T) {
	h := NewRouter(Options{Static: testStatic()})
	for _, p := range []string{"/../go.mod", "/assets/../../go.mod", "/%2e%2e/go.mod"} {
		rec := do(h, http.MethodGet, p, nil)
		if strings.Contains(rec.Body.String(), "module ") {
			t.Errorf("GET %s leaked a file outside the static root", p)
		}
	}
}

func TestUnknownAPIPathIsJSON404(t *testing.T) {
	rec := do(NewRouter(Options{Static: testStatic()}), http.MethodGet, "/api/nope", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
	if strings.Contains(rec.Body.String(), "<title>") {
		t.Error("an API path returned the web interface")
	}
	if got := do(NewRouter(Options{Static: testStatic()}), http.MethodGet, "/api", nil).Code; got != http.StatusNotFound {
		t.Errorf("GET /api = %d, want 404", got)
	}
}

func TestPostToUnknownPathIs404NotIndex(t *testing.T) {
	rec := do(NewRouter(Options{Static: testStatic()}), http.MethodPost, "/inventory", strings.NewReader("x"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestMissingBuildGives503(t *testing.T) {
	rec := do(NewRouter(Options{Static: fstest.MapFS{}}), http.MethodGet, "/", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := NewRouter(Options{Static: testStatic()})
	want := map[string]string{
		"Content-Security-Policy": "default-src 'self'; script-src 'self'; style-src 'self'; style-src-attr 'unsafe-inline'; " +
			"img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'",
		"X-Content-Type-Options":     "nosniff",
		"Referrer-Policy":            "no-referrer",
		"X-Frame-Options":            "DENY",
		"Permissions-Policy":         "camera=(), microphone=(), geolocation=()",
		"Cross-Origin-Opener-Policy": "same-origin",
	}
	// The page, the health check, an API error, a static asset and a SPA fallback.
	for _, p := range []string{"/", "/healthz", "/api/nope", "/assets/app.js", "/inventory"} {
		rec := do(h, http.MethodGet, p, nil)
		for k, v := range want {
			if got := rec.Header().Get(k); got != v {
				t.Errorf("GET %s: %s = %q, want %q", p, k, got, v)
			}
		}
	}
	rec := do(h, http.MethodPost, "/api/nope", strings.NewReader("x"))
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Error("error response has no security headers")
	}
}

func TestCacheControlNoStoreOnAPIOnly(t *testing.T) {
	h := NewRouter(Options{Static: testStatic()})
	if got := do(h, http.MethodGet, "/api/nope", nil).Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("/api/nope Cache-Control = %q, want no-store", got)
	}
	if got := do(h, http.MethodGet, "/api", nil).Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("/api Cache-Control = %q, want no-store", got)
	}
	if got := do(h, http.MethodGet, "/", nil).Header().Get("Cache-Control"); got == "no-store" {
		t.Error("/ must stay cacheable")
	}
	if got := do(h, http.MethodGet, "/apix", nil).Header().Get("Cache-Control"); got == "no-store" {
		t.Error("/apix is not an API path")
	}
}

func TestHSTSOnlyOverTLS(t *testing.T) {
	h := NewRouter(Options{Static: testStatic()})
	if got := do(h, http.MethodGet, "/healthz", nil).Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS over plain HTTP = %q", got)
	}
	req := httptest.NewRequest(http.MethodGet, "https://example.test/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Strict-Transport-Security"); got != "max-age=31536000" {
		t.Errorf("HSTS over TLS = %q", got)
	}
}

func TestBodyLimit(t *testing.T) {
	var readErr error
	h := limitBody(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}))
	do(h, http.MethodPost, "/", strings.NewReader(strings.Repeat("a", maxBodyBytes+1)))
	if readErr == nil {
		t.Fatal("a body above 1 MiB was read without error")
	}
	readErr = nil
	do(h, http.MethodPost, "/", strings.NewReader(strings.Repeat("a", maxBodyBytes)))
	if readErr != nil {
		t.Fatalf("a body of exactly 1 MiB failed: %v", readErr)
	}
}
