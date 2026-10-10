package api

import (
	"net/http"
	"strings"

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

// listDevices answers GET /api/devices?online=true|false&kind=router&q=text with a JSON array.
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
	writeJSON(w, http.StatusOK, devices)
}
