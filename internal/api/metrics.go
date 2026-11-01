package api

import (
	"bytes"
	"context"
	"net/http"
	"time"

	"github.com/FlexEbat/Netscribe/internal/metrics"
)

// metricsCacheFor is how long one reading of the database serves every request, so a
// scraper that is configured too eagerly does not keep the database busy.
const metricsCacheFor = 5 * time.Second

// getMetrics answers GET /metrics in the Prometheus text format. Without metrics.enabled
// the route is there but says nothing: it answers 404 as if it did not exist.
func (s *server) getMetrics(w http.ResponseWriter, r *http.Request) {
	if !s.metricsOn() {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	body, err := s.metricsBody(r.Context())
	if err != nil {
		s.internalError(w, "metrics", err)
		return
	}
	w.Header().Set("Content-Type", metrics.ContentType)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body)
}

func (s *server) metricsBody(ctx context.Context) ([]byte, error) {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	if s.metricsText != nil && s.now().Sub(s.metricsAt) < metricsCacheFor {
		return s.metricsText, nil
	}
	o, err := s.repo.Overview(ctx)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := metrics.Write(&b, s.version, o); err != nil {
		return nil, err
	}
	s.metricsText, s.metricsAt = b.Bytes(), s.now()
	return s.metricsText, nil
}
