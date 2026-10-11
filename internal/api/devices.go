package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/store"
)

const maxQueryLength = 100

func validKind(k model.DeviceKind) bool {
	switch k {
	case model.KindRouter, model.KindSwitch, model.KindAP, model.KindFirewall, model.KindServer,
		model.KindNAS, model.KindPrinter, model.KindCamera, model.KindIoT, model.KindHost, model.KindUnknown:
		return true
	}
	return false
}

// getDevice answers GET /api/devices/{id} with {"device": {...}}. A malformed id is a
// missing device, not a bad request (section 7.2 of the spec).
func (s *server) getDevice(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	device, err := s.repo.GetDevice(r.Context(), id)
	if errors.Is(err, model.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		s.internalError(w, "get device", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]model.Device{"device": device})
}

// listDevices answers GET /api/devices?online=true|false&kind=router&q=text with {"devices": [...]}.
func (s *server) listDevices(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var f store.DeviceFilter

	switch v := q.Get("online"); v {
	case "":
	case "true", "false":
		online := v == "true"
		f.Online = &online
	default:
		writeError(w, http.StatusBadRequest, "online must be true or false")
		return
	}
	if v := q.Get("kind"); v != "" {
		kind := model.DeviceKind(v)
		if !validKind(kind) {
			writeError(w, http.StatusBadRequest, "kind is unknown")
			return
		}
		f.Kind = kind
	}
	f.Query = strings.TrimSpace(q.Get("q"))
	if len(f.Query) > maxQueryLength {
		writeError(w, http.StatusBadRequest, "q is too long")
		return
	}

	devices, err := s.repo.ListDevices(r.Context(), f)
	if err != nil {
		s.internalError(w, "list devices", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]model.Device{"devices": devices})
}
