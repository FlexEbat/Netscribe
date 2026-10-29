package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/FlexEbat/Netscribe/internal/collector"
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

// getDevice answers GET /api/devices/{id} with {"device": {...}, "interfaces": [...], "services": [...]}. A malformed id is a
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
	ifaces, err := s.repo.ListInterfaces(r.Context(), id)
	if err != nil {
		s.internalError(w, "list interfaces", err)
		return
	}
	services, err := s.repo.ListServices(r.Context(), id)
	if err != nil {
		s.internalError(w, "list services", err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Device     model.Device      `json:"device"`
		Interfaces []model.Interface `json:"interfaces"`
		Services   []model.Service   `json:"services"`
	}{device, ifaces, services})
}

// changed tells the open event streams that the topology is different now.
func (s *server) changed() {
	if s.hub != nil {
		s.hub.Publish("topology.changed", struct{}{})
	}
}

func deviceDetail(d model.Device) string {
	name := d.Label
	if name == "" {
		name = d.IP
	}
	if name == "" {
		name = d.MAC
	}
	return "device " + strconv.FormatInt(d.ID, 10) + " " + name
}

// createDevice answers POST /api/devices with 201 and {"device": {...}}.
func (s *server) createDevice(w http.ResponseWriter, r *http.Request) {
	var body deviceBody
	if !decode(r, &body) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	in, err := body.manualDevice()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	p, _ := principal(r)
	d, err := s.repo.CreateDevice(r.Context(), in, p.User.ID)
	if errors.Is(err, model.ErrConflict) {
		writeError(w, http.StatusConflict, "a device with this address already exists")
		return
	}
	if err != nil {
		s.internalError(w, "create device", err)
		return
	}
	s.record(r, "device_create", "ok", deviceDetail(d))
	s.changed()
	writeJSON(w, http.StatusCreated, map[string]model.Device{"device": d})
}

// updateDevice answers PATCH /api/devices/{id}: 200 with the new state, 409 when the
// version the editor saw is no longer the current one.
func (s *server) updateDevice(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var body editBody
	if !decode(r, &body) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	edit, err := body.deviceEdit()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	p, _ := principal(r)
	d, err := s.repo.UpdateDevice(r.Context(), id, edit, p.User.ID)
	switch {
	case errors.Is(err, model.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
		return
	case errors.Is(err, model.ErrVersionConflict):
		writeError(w, http.StatusConflict, "the device was changed by someone else")
		return
	case err != nil:
		s.internalError(w, "update device", err)
		return
	}
	s.record(r, "device_edit", "ok", deviceDetail(d))
	s.changed()
	writeJSON(w, http.StatusOK, map[string]model.Device{"device": d})
}

// deleteDevice answers DELETE /api/devices/{id} with 204.
func (s *server) deleteDevice(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	d, err := s.repo.GetDevice(r.Context(), id)
	if errors.Is(err, model.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		s.internalError(w, "delete device", err)
		return
	}
	if err := s.repo.DeleteDevice(r.Context(), id); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		s.internalError(w, "delete device", err)
		return
	}
	s.record(r, "device_delete", "ok", deviceDetail(d))
	s.changed()
	w.WriteHeader(http.StatusNoContent)
}

// listDevices answers GET /api/devices?online=true|false&trusted=true|false&kind=router&q=text with {"devices": [...]}.
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
	switch v := q.Get("trusted"); v {
	case "":
	case "true", "false":
		trusted := v == "true"
		f.Trusted = &trusted
	default:
		writeError(w, http.StatusBadRequest, "trusted must be true or false")
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

// identifyDevice answers POST /api/devices/{id}/identify with 202 and runs nmap for that
// device in the background. The result shows up when the topology changes.
func (s *server) identifyDevice(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	err = s.identifier.Identify(id)
	switch {
	case err == nil:
		s.record(r, "device_identify", "ok", "device "+strconv.FormatInt(id, 10))
		w.WriteHeader(http.StatusAccepted)
	case errors.Is(err, model.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, collector.ErrBusy):
		writeError(w, http.StatusConflict, "a scan or another identification is running")
	case errors.Is(err, collector.ErrNmapUnavailable):
		writeError(w, http.StatusBadRequest, "nmap is not available")
	case errors.Is(err, collector.ErrNoAddress):
		writeError(w, http.StatusBadRequest, "the device has no IPv4 address")
	case errors.Is(err, collector.ErrOutOfScope):
		writeError(w, http.StatusBadRequest, "the device is outside the scan targets")
	default:
		s.internalError(w, "identify device", err)
	}
}
