package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/FlexEbat/Netscribe/internal/model"
)

// getOverview answers GET /api/overview with {"overview": {...}}.
func (s *server) getOverview(w http.ResponseWriter, r *http.Request) {
	o, err := s.repo.Overview(r.Context())
	if err != nil {
		s.internalError(w, "overview", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]model.Overview{"overview": o})
}

// getDeviceHistory answers GET /api/devices/{id}/history with {"history": {...}}.
func (s *server) getDeviceHistory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	h, err := s.repo.DeviceHistory(r.Context(), id)
	if errors.Is(err, model.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		s.internalError(w, "device history", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]model.DeviceHistory{"history": h})
}
