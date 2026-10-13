package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/FlexEbat/Netscribe/internal/auth"
	"github.com/FlexEbat/Netscribe/internal/model"
)

// userError maps what the auth service returns for bad input to a 400 line. It returns
// false for anything else.
func userError(err error) (string, bool) {
	for _, known := range []error{
		auth.ErrUsernameInvalid, auth.ErrRoleUnknown, auth.ErrPasswordShort,
		auth.ErrPasswordLong, auth.ErrPasswordIsUsername,
	} {
		if errors.Is(err, known) {
			return known.Error(), true
		}
	}
	return "", false
}

func userID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	return id, err == nil && id >= 1
}

// listUsers answers GET /api/users with {"users": [...]}.
func (s *server) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.auth.ListUsers(r.Context())
	if err != nil {
		s.internalError(w, "list users", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]model.User{"users": users})
}

type createUserBody struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
	Password    string `json:"password"`
}

// createUser answers POST /api/users. The password is temporary: the user must change it
// at the first sign-in.
func (s *server) createUser(w http.ResponseWriter, r *http.Request) {
	var body createUserBody
	if !decode(r, &body) || len(body.Username) > maxUsernameBytes || len(body.Password) > maxPasswordBytes {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	u, err := s.auth.CreateUser(r.Context(), auth.NewUser{
		Username: body.Username, DisplayName: cleanLabel(body.DisplayName), Role: model.Role(body.Role),
		Password: body.Password, MustChangePassword: true,
	})
	if msg, ok := userError(err); ok {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if errors.Is(err, model.ErrConflict) {
		writeError(w, http.StatusConflict, "a user with this name already exists")
		return
	}
	if err != nil {
		s.internalError(w, "create user", err)
		return
	}
	s.record(r, "user_create", "ok", "user "+u.Username+" with role "+string(u.Role))
	writeJSON(w, http.StatusCreated, map[string]model.User{"user": u})
}

type updateUserBody struct {
	Role     *string `json:"role"`
	Disabled *bool   `json:"disabled"`
}

// updateUser answers PATCH /api/users/{id}. A change of role or a disabling ends the
// sessions of the user. The last administrator can be neither demoted nor disabled.
func (s *server) updateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := userID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var body updateUserBody
	if !decode(r, &body) || (body.Role == nil && body.Disabled == nil) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	u, err := s.auth.UpdateUser(r.Context(), id, body.Role, body.Disabled)
	switch {
	case errors.Is(err, model.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
		return
	case errors.Is(err, model.ErrLastAdmin):
		writeError(w, http.StatusConflict, model.ErrLastAdmin.Error())
		return
	case err != nil:
		if msg, ok := userError(err); ok {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		s.internalError(w, "update user", err)
		return
	}
	detail := "user " + u.Username + ": role " + string(u.Role) + ", disabled " + strconv.FormatBool(u.Disabled)
	s.record(r, "user_update", "ok", detail)
	writeJSON(w, http.StatusOK, map[string]model.User{"user": u})
}

// resetPassword answers PUT /api/users/{id}/password with 204. The new password is
// temporary and the sessions of the user end.
func (s *server) resetPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := userID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if !decode(r, &body) || len(body.Password) > maxPasswordBytes {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	u, err := s.auth.GetUser(r.Context(), id)
	if errors.Is(err, model.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		s.internalError(w, "reset password", err)
		return
	}
	err = s.auth.ResetPasswordByID(r.Context(), id, body.Password)
	if msg, ok := userError(err); ok {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if errors.Is(err, model.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		s.internalError(w, "reset password", err)
		return
	}
	s.record(r, "user_password_reset", "ok", "user "+u.Username)
	w.WriteHeader(http.StatusNoContent)
}

// deleteUser answers DELETE /api/users/{id} with 204. Sessions and tokens of the user go
// with it, and the last administrator cannot be deleted, not even by themselves.
func (s *server) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := userID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	u, err := s.auth.GetUser(r.Context(), id)
	if errors.Is(err, model.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		s.internalError(w, "delete user", err)
		return
	}
	err = s.auth.DeleteUser(r.Context(), id)
	switch {
	case errors.Is(err, model.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
		return
	case errors.Is(err, model.ErrLastAdmin):
		writeError(w, http.StatusConflict, model.ErrLastAdmin.Error())
		return
	case err != nil:
		s.internalError(w, "delete user", err)
		return
	}
	s.record(r, "user_delete", "ok", "user "+u.Username)
	w.WriteHeader(http.StatusNoContent)
}
