package api

import (
	"errors"
	"math"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/render"
)

// maxCoordinate bounds a canvas position (section 9.5 of the spec).
const maxCoordinate = 100000

// getTopology answers GET /api/topology: devices, links and containers in one answer,
// every device with a position.
func (s *server) getTopology(w http.ResponseWriter, r *http.Request) {
	t, err := s.repo.Topology(r.Context())
	if err != nil {
		s.internalError(w, "read topology", err)
		return
	}
	t.Devices = render.Layout(t.Devices)
	writeJSON(w, http.StatusOK, t)
}

type positionBody struct {
	X *float64 `json:"x"`
	Y *float64 `json:"y"`
}

// setPosition answers PUT /api/devices/{id}/position with 204.
func (s *server) setPosition(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var body positionBody
	if !decode(r, &body) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validCoordinate(body.X) || !validCoordinate(body.Y) {
		writeError(w, http.StatusBadRequest, "x and y must be numbers between -100000 and 100000")
		return
	}
	err = s.repo.SetDevicePosition(r.Context(), id, *body.X, *body.Y)
	if errors.Is(err, model.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		s.internalError(w, "set position", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validCoordinate(v *float64) bool {
	return v != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0) && math.Abs(*v) <= maxCoordinate
}
