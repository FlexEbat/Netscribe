package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/health"
)

type fakeHealth struct {
	runs   int
	checks []health.Check
}

func (f *fakeHealth) Run(context.Context) []health.Check {
	f.runs++
	return f.checks
}

func healthEnv(t *testing.T, h *fakeHealth) (*env, session, session) {
	t.Helper()
	return systemEnv(t, func(o *envOptions) { o.health = h })
}

func TestHealthShowsTheChecksToAnAdministratorOnly(t *testing.T) {
	h := &fakeHealth{checks: []health.Check{
		{ID: "a", Status: health.OK, Title: "A"},
		{ID: "b", Status: health.Warn, Title: "B", Detail: "d", Fix: "f"},
	}}
	e, admin, viewer := healthEnv(t, h)
	e.addUser("olga", "operator")
	operator := e.mustLogin("olga")

	rec := asWith(e, admin, http.MethodGet, "/api/health", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"worst":"warn"`) || !strings.Contains(rec.Body.String(), `"fix":"f"`) {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	for name, s := range map[string]session{"viewer": viewer, "operator": operator} {
		if code := asWith(e, s, http.MethodGet, "/api/health", nil).Code; code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", name, code)
		}
	}
	if code := e.do(http.MethodGet, "/api/health", nil).Code; code != http.StatusUnauthorized {
		t.Errorf("no session: status = %d, want 401", code)
	}
	tok := issueToken(t, e, admin, map[string]any{"name": "ci", "scope": "write"})
	if code := e.do(http.MethodGet, "/api/health", nil, bearer(tok.Value)).Code; code != http.StatusForbidden {
		t.Errorf("token: status = %d, want 403", code)
	}
}

func TestHealthRunsOnceUntilAskedAgainAndNotTwiceInTenSeconds(t *testing.T) {
	h := &fakeHealth{checks: []health.Check{{ID: "a", Status: health.OK, Title: "A"}}}
	e, admin, _ := healthEnv(t, h)
	asWith(e, admin, http.MethodGet, "/api/health", nil)
	asWith(e, admin, http.MethodGet, "/api/health", nil)
	if h.runs != 1 {
		t.Fatalf("GET ran the checks %d times, want 1", h.runs)
	}
	// the first GET ran them a moment ago, so a run on request is too soon
	rec := asWith(e, admin, http.MethodPost, "/api/health/run", nil)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("status = %d, want 429 with Retry-After", rec.Code)
	}
	if h.runs != 1 {
		t.Errorf("a refused run still ran the checks")
	}
}

func TestHealthRunOnRequestAfterTheWait(t *testing.T) {
	h := &fakeHealth{checks: []health.Check{{ID: "a", Status: health.OK, Title: "A"}}}
	e, admin, _ := healthEnv(t, h)
	e.clk.advance(11 * time.Second)
	if code := asWith(e, admin, http.MethodPost, "/api/health/run", nil).Code; code != http.StatusOK || h.runs != 1 {
		t.Fatalf("status = %d, runs = %d", code, h.runs)
	}
}

func TestHealthRoutesAreMissingWithoutARunner(t *testing.T) {
	e, admin, _ := systemEnv(t)
	if code := asWith(e, admin, http.MethodPost, "/api/health/run", nil).Code; code == http.StatusOK || code == http.StatusAccepted {
		t.Errorf("status = %d", code)
	}
}
