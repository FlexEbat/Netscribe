package api

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMetricsAreOffUntilSwitchedOn(t *testing.T) {
	var on atomic.Bool
	e := newEnv(t, func(o *envOptions) { o.metrics = on.Load })
	admin := e.mustLogin("admin")
	if code := e.as(admin, http.MethodGet, "/metrics", nil).Code; code != http.StatusNotFound {
		t.Fatalf("off: status = %d, want 404", code)
	}
	on.Store(true)
	rec := e.as(admin, http.MethodGet, "/metrics", nil)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain; version=0.0.4") {
		t.Fatalf("on: status = %d, type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), "netscribe_devices{status=\"online\"} 0\n") {
		t.Errorf("body:\n%s", rec.Body)
	}
}

func TestMetricsNeedASessionOrAToken(t *testing.T) {
	e := newEnv(t, func(o *envOptions) { o.metrics = func() bool { return true } })
	admin := e.mustLogin("admin")
	if code := e.do(http.MethodGet, "/metrics", nil).Code; code != http.StatusUnauthorized {
		t.Errorf("no credential: status = %d, want 401", code)
	}
	tok := issueToken(t, e, admin, map[string]any{"name": "prom", "scope": "read"})
	if code := e.do(http.MethodGet, "/metrics", nil, bearer(tok.Value)).Code; code != http.StatusOK {
		t.Errorf("read token: status = %d, want 200", code)
	}
}

func TestMetricsAreNotRegisteredWithoutTheSwitch(t *testing.T) {
	e := newEnv(t)
	admin := e.mustLogin("admin")
	// the page of the panel may answer an unknown path, but never with numbers
	if rec := e.as(admin, http.MethodGet, "/metrics", nil); strings.Contains(rec.Body.String(), "netscribe_devices") {
		t.Fatalf("numbers served without MetricsEnabled: %d", rec.Code)
	}
}
