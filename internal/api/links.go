package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/FlexEbat/Netscribe/internal/model"
)

// createLink answers POST /api/links with 201 and {"link": {...}}.
func (s *server) createLink(w http.ResponseWriter, r *http.Request) {
	var body linkBody
	if !decode(r, &body) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	in, err := body.manualLink()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	l, err := s.repo.CreateLink(r.Context(), in)
	switch {
	case errors.Is(err, model.ErrNotFound):
		writeError(w, http.StatusNotFound, "device not found")
		return
	case errors.Is(err, model.ErrConflict):
		writeError(w, http.StatusConflict, "this link already exists")
		return
	case err != nil:
		s.internalError(w, "create link", err)
		return
	}
	s.record(r, "link_create", "ok", "link "+strconv.FormatInt(l.ID, 10)+" between devices "+
		strconv.FormatInt(l.ADeviceID, 10)+" and "+strconv.FormatInt(l.BDeviceID, 10))
	s.changed()
	writeJSON(w, http.StatusCreated, map[string]model.Link{"link": l})
}

// deleteLink answers DELETE /api/links/{id}: 204, 404, or 409 for a link a scan found.
func (s *server) deleteLink(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	err = s.repo.DeleteLink(r.Context(), id)
	switch {
	case errors.Is(err, model.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
		return
	case errors.Is(err, model.ErrNotManual):
		writeError(w, http.StatusConflict, "the link was found by a scan and cannot be deleted by hand")
		return
	case err != nil:
		s.internalError(w, "delete link", err)
		return
	}
	s.record(r, "link_delete", "ok", "link "+strconv.FormatInt(id, 10))
	s.changed()
	w.WriteHeader(http.StatusNoContent)
}
