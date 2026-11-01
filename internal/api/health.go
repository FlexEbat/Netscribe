package api

import (
	"context"
	"net/http"
	"time"

	"github.com/FlexEbat/Netscribe/internal/health"
)

// healthEvery is the shortest time between two runs of the checks.
const healthEvery = 10 * time.Second

// HealthRunner runs the health checks of this install. cmd/netscribe implements it.
type HealthRunner interface {
	Run(ctx context.Context) []health.Check
}

type healthAnswer struct {
	Checks []health.Check `json:"checks"`
	RanAt  time.Time      `json:"ranAt"`
	Worst  health.Status  `json:"worst"`
}

// getHealth answers GET /api/health with the last result, and runs the checks the first
// time it is asked.
func (s *server) getHealth(w http.ResponseWriter, r *http.Request) {
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	if s.healthRan.IsZero() {
		s.runHealth(r.Context())
	}
	writeJSON(w, http.StatusOK, s.healthAnswerLocked())
}

// runHealthNow answers POST /api/health/run with a fresh result, at most once in 10 seconds.
func (s *server) runHealthNow(w http.ResponseWriter, r *http.Request) {
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	if !s.healthRan.IsZero() && s.now().Sub(s.healthRan) < healthEvery {
		w.Header().Set("Retry-After", "10")
		writeError(w, http.StatusTooManyRequests, "the checks ran a moment ago: wait a few seconds")
		return
	}
	s.runHealth(r.Context())
	writeJSON(w, http.StatusOK, s.healthAnswerLocked())
}

func (s *server) runHealth(ctx context.Context) {
	s.healthLast = s.health.Run(ctx)
	s.healthRan = s.now()
}

func (s *server) healthAnswerLocked() healthAnswer {
	checks := s.healthLast
	if checks == nil {
		checks = []health.Check{}
	}
	return healthAnswer{Checks: checks, RanAt: s.healthRan, Worst: health.Worst(checks)}
}
