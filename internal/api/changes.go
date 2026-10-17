package api

import (
	"net/http"
	"strconv"

	"github.com/FlexEbat/Netscribe/internal/model"
)

const (
	defaultChangesLimit = 100
	maxChangesLimit     = 500
)

// listChanges answers GET /api/changes?limit= with the newest changes first.
func (s *server) listChanges(w http.ResponseWriter, r *http.Request) {
	limit := defaultChangesLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxChangesLimit {
			writeError(w, http.StatusBadRequest, "limit must be a number from 1 to "+strconv.Itoa(maxChangesLimit))
			return
		}
		limit = n
	}
	changes, err := s.repo.ListChanges(r.Context(), limit)
	if err != nil {
		s.internalError(w, "list changes", err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Changes []model.Change `json:"changes"`
	}{changes})
}
