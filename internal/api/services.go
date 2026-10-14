package api

import (
	"net/http"
	"strconv"

	"github.com/FlexEbat/Netscribe/internal/model"
)

// listServices answers GET /api/services?deviceId=N with {"services": [...]}. Without
// deviceId it lists the services of every device. A bad deviceId is a 400, a device that
// does not exist simply has no services.
func (s *server) listServices(w http.ResponseWriter, r *http.Request) {
	var deviceID int64
	if v := r.URL.Query().Get("deviceId"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "deviceId must be a positive number")
			return
		}
		deviceID = n
	}
	services, err := s.repo.ListServices(r.Context(), deviceID)
	if err != nil {
		s.internalError(w, "list services", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]model.Service{"services": services})
}
