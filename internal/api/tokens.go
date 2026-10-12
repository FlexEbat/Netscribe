package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/FlexEbat/Netscribe/internal/auth"
	"github.com/FlexEbat/Netscribe/internal/model"
)

// listTokens answers GET /api/tokens with the caller's own tokens: {"tokens": [...]}.
// The value of a token is never part of it, only the first characters.
func (s *server) listTokens(w http.ResponseWriter, r *http.Request) {
	p, _ := principal(r)
	tokens, err := s.auth.ListTokens(r.Context(), p.User.ID)
	if err != nil {
		s.internalError(w, "list tokens", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]model.APIToken{"tokens": tokens})
}

type createTokenBody struct {
	Name  string `json:"name"`
	Scope string `json:"scope"`
	Days  int    `json:"days"`
}

type createTokenResponse struct {
	Value string         `json:"value"` // shown once
	Token model.APIToken `json:"token"`
}

// createToken answers POST /api/tokens with 201 and the value of the token, which is
// never shown again. 409 means the user holds the most tokens allowed.
func (s *server) createToken(w http.ResponseWriter, r *http.Request) {
	p, _ := principal(r)
	var body createTokenBody
	if !decode(r, &body) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	value, tok, err := s.auth.IssueToken(r.Context(), p.User, cleanLabel(body.Name), body.Scope, body.Days)
	for _, known := range []error{auth.ErrTokenNameInvalid, auth.ErrTokenScope, auth.ErrScopeAboveRole, auth.ErrTokenLifetime} {
		if errors.Is(err, known) {
			writeError(w, http.StatusBadRequest, known.Error())
			return
		}
	}
	if errors.Is(err, model.ErrConflict) {
		writeError(w, http.StatusConflict, "too many active tokens")
		return
	}
	if err != nil {
		s.internalError(w, "create token", err)
		return
	}
	s.record(r, "token_create", "ok", "token "+strconv.FormatInt(tok.ID, 10)+" "+tok.Prefix+" scope "+tok.Scope)
	writeJSON(w, http.StatusCreated, createTokenResponse{Value: value, Token: tok})
}

// revokeToken answers DELETE /api/tokens/{id} with 204. Only the owner can revoke a token.
func (s *server) revokeToken(w http.ResponseWriter, r *http.Request) {
	p, _ := principal(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	err = s.auth.RevokeToken(r.Context(), id, p.User.ID)
	if errors.Is(err, model.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		s.internalError(w, "revoke token", err)
		return
	}
	s.record(r, "token_revoke", "ok", "token "+strconv.FormatInt(id, 10))
	w.WriteHeader(http.StatusNoContent)
}
