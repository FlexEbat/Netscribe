package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/store"
)

// listFilters answers GET /api/filters?page=inventory with {"filters": [...]}: the filters
// of the caller, newest first.
func (s *server) listFilters(w http.ResponseWriter, r *http.Request) {
	page := r.URL.Query().Get("page")
	if !model.ValidFilterPage(page) {
		writeError(w, http.StatusBadRequest, model.ErrFilterPage.Error())
		return
	}
	p, _ := principal(r)
	filters, err := s.repo.ListFilters(r.Context(), p.User.ID, page)
	if err != nil {
		s.internalError(w, "list filters", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]model.SavedFilter{"filters": filters})
}

type filterBody struct {
	Page  string `json:"page"`
	Name  string `json:"name"`
	Query string `json:"query"`
}

// createFilter answers POST /api/filters with 201 and {"filter": {...}}. The query is
// stored in its canonical form.
func (s *server) createFilter(w http.ResponseWriter, r *http.Request) {
	var body filterBody
	if !decode(r, &body) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !model.ValidFilterPage(body.Page) {
		writeError(w, http.StatusBadRequest, model.ErrFilterPage.Error())
		return
	}
	name, err := model.CheckFilterName(body.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	query, err := model.CheckFilterQuery(body.Page, body.Query)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	p, _ := principal(r)
	f, err := s.repo.CreateFilter(r.Context(), p.User.ID, body.Page, name, query)
	switch {
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "you already have a filter with this name")
		return
	case errors.Is(err, store.ErrTooManyFilters):
		writeError(w, http.StatusBadRequest, fmt.Sprintf("you can keep at most %d filters per page", model.MaxFiltersPerPage))
		return
	case err != nil:
		s.internalError(w, "create filter", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]model.SavedFilter{"filter": f})
}

// deleteFilter answers DELETE /api/filters/{id} with 204. A filter of someone else is 404.
func (s *server) deleteFilter(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	p, _ := principal(r)
	switch err := s.repo.DeleteFilter(r.Context(), p.User.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case err != nil:
		s.internalError(w, "delete filter", err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
