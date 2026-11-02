package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/FlexEbat/Netscribe/internal/collector"
	"github.com/FlexEbat/Netscribe/internal/model"
)

// wakeEvery is the shortest time between two wake requests for one device.
const wakeEvery = 10 * time.Second

// wakeDevice answers POST /api/devices/{id}/wake: it sends the magic packet of the stored
// device and answers 202 without saying whether the machine woke. The MAC and the address
// come from the stored device, never from the request.
func (s *server) wakeDevice(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	dev, err := s.repo.GetDevice(r.Context(), id)
	switch {
	case errors.Is(err, model.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
		return
	case err != nil:
		s.internalError(w, "get device to wake", err)
		return
	}
	if !s.reserveWake(id) {
		writeError(w, http.StatusTooManyRequests, "this device was woken a moment ago, try again in a few seconds")
		return
	}
	err = s.waker.Wake(r.Context(), dev.IP, dev.MAC)
	switch {
	case err == nil:
		s.record(r, "device_wake", "ok", "device "+strconv.FormatInt(id, 10))
		w.WriteHeader(http.StatusAccepted)
	case errors.Is(err, collector.ErrNoMAC), errors.Is(err, collector.ErrNoLocalSubnet), errors.Is(err, collector.ErrPublicBroadcast):
		s.releaseWake(id) // nothing was sent, so the refusal does not use up the 10 seconds
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		s.internalError(w, "wake device", err)
	}
}

// reserveWake claims the next wake of a device. It is false while the last one is younger
// than wakeEvery. Entries older than that are dropped, so the map stays small.
func (s *server) reserveWake(id int64) bool {
	now := s.now()
	s.wakeMu.Lock()
	defer s.wakeMu.Unlock()
	for k, at := range s.wakeAt {
		if now.Sub(at) >= wakeEvery {
			delete(s.wakeAt, k)
		}
	}
	if _, busy := s.wakeAt[id]; busy {
		return false
	}
	if s.wakeAt == nil {
		s.wakeAt = make(map[int64]time.Time)
	}
	s.wakeAt[id] = now
	return true
}

func (s *server) releaseWake(id int64) {
	s.wakeMu.Lock()
	delete(s.wakeAt, id)
	s.wakeMu.Unlock()
}
