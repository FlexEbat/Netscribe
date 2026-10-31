package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/FlexEbat/Netscribe/internal/model"
)

type groupBody struct {
	Name        *string   `json:"name"`
	Description *string   `json:"description"`
	VLAN        *int      `json:"vlan"`
	Ranges      *[]string `json:"ranges"`
	Version     int64     `json:"version"`
}

// checked turns the body into the checked pieces of a group. A nil piece was not sent.
func (b groupBody) checked() (name, desc *string, ranges *[]string, err error) {
	if b.Name != nil {
		n, e := model.CheckGroupName(*b.Name)
		if e != nil {
			return nil, nil, nil, e
		}
		name = &n
	}
	if b.Description != nil {
		d, e := model.CheckGroupDescription(*b.Description)
		if e != nil {
			return nil, nil, nil, e
		}
		desc = &d
	}
	if b.Ranges != nil {
		r, e := model.CheckGroupRanges(*b.Ranges)
		if e != nil {
			return nil, nil, nil, e
		}
		ranges = &r
	}
	return name, desc, ranges, nil
}

// listGroups answers GET /api/groups with {"groups": [...]}.
func (s *server) listGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := s.repo.ListGroups(r.Context())
	if err != nil {
		s.internalError(w, "list groups", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]model.Group{"groups": groups})
}

// createGroup answers POST /api/groups with 201 and {"group": {...}}.
func (s *server) createGroup(w http.ResponseWriter, r *http.Request) {
	var body groupBody
	if !decode(r, &body) || body.Name == nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// A new group has no VLAN number of 0: that value only means "remove it" on an edit.
	if err := model.CheckGroupVLAN(body.VLAN); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name, desc, ranges, err := body.checked()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	in := model.ManualGroup{Name: *name, VLAN: body.VLAN, Ranges: []string{}}
	if desc != nil {
		in.Description = *desc
	}
	if ranges != nil {
		in.Ranges = *ranges
	}
	g, err := s.repo.CreateGroup(r.Context(), in)
	if errors.Is(err, model.ErrConflict) {
		writeError(w, http.StatusConflict, "a group with this name already exists")
		return
	}
	if err != nil {
		s.internalError(w, "create group", err)
		return
	}
	s.record(r, "group_create", "ok", "group "+strconv.FormatInt(g.ID, 10)+" "+g.Name)
	s.changed()
	writeJSON(w, http.StatusCreated, map[string]model.Group{"group": g})
}

// updateGroup answers PATCH /api/groups/{id}. A vlan of 0 removes the VLAN number.
func (s *server) updateGroup(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var body groupBody
	if !decode(r, &body) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Version < 1 {
		writeError(w, http.StatusBadRequest, errVersionNeeded.Error())
		return
	}
	if body.VLAN != nil && *body.VLAN != 0 {
		if err := model.CheckGroupVLAN(body.VLAN); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	name, desc, ranges, err := body.checked()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	g, err := s.repo.UpdateGroup(r.Context(), id, model.GroupEdit{Name: name, Description: desc, VLAN: body.VLAN, Ranges: ranges, Version: body.Version})
	switch {
	case errors.Is(err, model.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
		return
	case errors.Is(err, model.ErrVersionConflict):
		writeError(w, http.StatusConflict, "the group was changed by someone else")
		return
	case errors.Is(err, model.ErrConflict):
		writeError(w, http.StatusConflict, "a group with this name already exists")
		return
	case err != nil:
		s.internalError(w, "update group", err)
		return
	}
	s.record(r, "group_edit", "ok", "group "+strconv.FormatInt(g.ID, 10)+" "+g.Name)
	s.changed()
	writeJSON(w, http.StatusOK, map[string]model.Group{"group": g})
}

// deleteGroup answers DELETE /api/groups/{id} with 204. The devices of the group stay.
func (s *server) deleteGroup(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	g, err := s.repo.GetGroup(r.Context(), id)
	if errors.Is(err, model.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err == nil {
		err = s.repo.DeleteGroup(r.Context(), id)
	}
	if errors.Is(err, model.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		s.internalError(w, "delete group", err)
		return
	}
	s.record(r, "group_delete", "ok", "group "+strconv.FormatInt(g.ID, 10)+" "+g.Name)
	s.changed()
	w.WriteHeader(http.StatusNoContent)
}
