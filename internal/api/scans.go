package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/FlexEbat/Netscribe/internal/model"
)

// getScan answers GET /api/scans/{id}. A malformed id is a missing scan: section 7.2 of
// the spec wants 404 there, not 400.
func (s *server) getScan(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	scan, err := s.repo.GetScan(r.Context(), id)
	if errors.Is(err, model.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		s.internalError(w, "get scan", err)
		return
	}
	writeJSON(w, http.StatusOK, scan)
}
