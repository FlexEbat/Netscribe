package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/model"
)

type userOut struct {
	User model.User `json:"user"`
}

func decodeUser(t *testing.T, rec *httptest.ResponseRecorder) model.User {
	t.Helper()
	var out userOut
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body %q: %v", rec.Body, err)
	}
	return out.User
}

func userPath(id int64, suffix string) string {
	return "/api/users/" + strconv.FormatInt(id, 10) + suffix
}

func adminCreates(t *testing.T, e *env, s session, name, role string) model.User {
	t.Helper()
	rec := e.as(s, http.MethodPost, "/api/users", map[string]string{"username": name, "role": role, "password": goodPassword})
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/users %s: %d %s", name, rec.Code, rec.Body)
	}
	return decodeUser(t, rec)
}

func TestAdminCreatesUsersWhoSeeExactlyWhatTheirRoleAllows(t *testing.T) {
	e := newEnv(t)
	admin := e.mustLogin("admin")
	for _, role := range []model.Role{model.RoleViewer, model.RoleOperator, model.RoleAdmin} {
		name := "u-" + string(role)
		u := adminCreates(t, e, admin, name, string(role))
		if u.Role != role || !u.MustChangePassword || u.Disabled {
			t.Errorf("created %s = %+v", role, u)
		}
		s, rec := e.login(name, goodPassword, from("198.18.9."+strconv.Itoa(len(name))+":1"))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s cannot sign in: %d %s", role, rec.Code, rec.Body)
		}
		me := e.as(s, http.MethodGet, "/api/auth/me", nil)
		var got meResponse
		if err := json.Unmarshal(me.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.User.Role != role || !got.User.MustChangePassword {
			t.Errorf("%s: me = %+v", role, got.User)
		}
		want := map[model.Role]int{model.RoleViewer: 3, model.RoleOperator: 5, model.RoleAdmin: 7}[role]
		if len(got.Permissions) != want {
			t.Errorf("%s holds %d rights %v, want %d", role, len(got.Permissions), got.Permissions, want)
		}
	}
	if entries := auditActions(t, e, "user_create"); len(entries) != 3 || !strings.Contains(entries[0].Detail, "u-admin") {
		t.Errorf("audit = %+v", entries)
	}
}

func TestCreateUserValidationAndConflict(t *testing.T) {
	e := newEnv(t)
	admin := e.mustLogin("admin")
	adminCreates(t, e, admin, "taken", "viewer")
	for name, tc := range map[string]struct {
		body string
		code int
		want string
	}{
		"short name":       {`{"username":"ab","role":"viewer","password":"` + goodPassword + `"}`, 400, "username is invalid"},
		"bad characters":   {`{"username":"a b c","role":"viewer","password":"` + goodPassword + `"}`, 400, "username is invalid"},
		"unknown role":     {`{"username":"newbie","role":"root","password":"` + goodPassword + `"}`, 400, "role is unknown"},
		"no role":          {`{"username":"newbie","password":"` + goodPassword + `"}`, 400, "role is unknown"},
		"short password":   {`{"username":"newbie","role":"viewer","password":"short"}`, 400, "password is shorter than the minimum"},
		"password is name": {`{"username":"newbie-user","role":"viewer","password":"newbie-user"}`, 400, "password is shorter than the minimum"},
		"duplicate":        {`{"username":"TAKEN","role":"viewer","password":"` + goodPassword + `"}`, 409, "a user with this name already exists"},
		"unknown field":    {`{"username":"newbie","role":"viewer","password":"` + goodPassword + `","mustChangePassword":false}`, 400, "invalid request body"},
		"not json":         {`x`, 400, "invalid request body"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := e.as(admin, http.MethodPost, "/api/users", tc.body)
			if rec.Code != tc.code || errorText(t, rec) != tc.want {
				t.Errorf("status %d, error %q, want %d %q", rec.Code, errorText(t, rec), tc.code, tc.want)
			}
		})
	}
	list := e.as(admin, http.MethodGet, "/api/users", nil)
	var body struct {
		Users []model.User `json:"users"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &body); err != nil || len(body.Users) != 2 {
		t.Errorf("users = %+v, %v, want admin and taken only", body.Users, err)
	}
	if strings.Contains(list.Body.String(), "assword") || strings.Contains(list.Body.String(), "argon") {
		t.Errorf("the list shows credential material: %s", list.Body)
	}
}

func TestChangingARoleDisablingAndDeletingEndTheSessions(t *testing.T) {
	e := newEnv(t)
	admin := e.mustLogin("admin")
	for name, change := range map[string]func(id int64) *httptest.ResponseRecorder{
		"role": func(id int64) *httptest.ResponseRecorder {
			return e.as(admin, http.MethodPatch, userPath(id, ""), map[string]any{"role": "viewer"})
		},
		"disable": func(id int64) *httptest.ResponseRecorder {
			return e.as(admin, http.MethodPatch, userPath(id, ""), map[string]any{"disabled": true})
		},
		"delete": func(id int64) *httptest.ResponseRecorder {
			return e.as(admin, http.MethodDelete, userPath(id, ""), nil)
		},
		"password reset": func(id int64) *httptest.ResponseRecorder {
			return e.as(admin, http.MethodPut, userPath(id, "/password"), map[string]string{"password": "a brand new password"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			u := e.addUser("victim-"+strings.ReplaceAll(name, " ", "-"), model.RoleOperator)
			s := e.mustLogin(u.Username)
			if rec := e.as(s, http.MethodGet, "/api/auth/me", nil); rec.Code != http.StatusOK {
				t.Fatalf("before: %d", rec.Code)
			}
			if rec := change(u.ID); rec.Code >= 300 {
				t.Fatalf("change: %d %s", rec.Code, rec.Body)
			}
			if rec := e.as(s, http.MethodGet, "/api/auth/me", nil); rec.Code != http.StatusUnauthorized {
				t.Errorf("after: %d, want 401: the session must end", rec.Code)
			}
		})
	}
}

func TestPatchReturnsTheNewStateAndIsAudited(t *testing.T) {
	e := newEnv(t)
	admin := e.mustLogin("admin")
	u := adminCreates(t, e, admin, "olga", "viewer")
	rec := e.as(admin, http.MethodPatch, userPath(u.ID, ""), map[string]any{"role": "operator"})
	if got := decodeUser(t, rec); rec.Code != http.StatusOK || got.Role != model.RoleOperator {
		t.Errorf("PATCH: %d %+v", rec.Code, got)
	}
	if entries := auditActions(t, e, "user_update"); len(entries) != 1 || !strings.Contains(entries[0].Detail, "olga") {
		t.Errorf("audit = %+v", entries)
	}
}

func TestPatchValidation(t *testing.T) {
	e := newEnv(t)
	admin := e.mustLogin("admin")
	u := adminCreates(t, e, admin, "olga", "viewer")
	for name, tc := range map[string]struct {
		body string
		code int
		want string
	}{
		"nothing to change": {`{}`, 400, "invalid request body"},
		"unknown role":      {`{"role":"root"}`, 400, "role is unknown"},
		"unknown field":     {`{"username":"x"}`, 400, "invalid request body"},
		"role as a number":  {`{"role":1}`, 400, "invalid request body"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := e.as(admin, http.MethodPatch, userPath(u.ID, ""), tc.body)
			if rec.Code != tc.code || errorText(t, rec) != tc.want {
				t.Errorf("status %d, error %q, want %d %q", rec.Code, errorText(t, rec), tc.code, tc.want)
			}
		})
	}
	for _, id := range []string{"999", "abc", "0"} {
		if rec := e.as(admin, http.MethodPatch, "/api/users/"+id, map[string]any{"role": "viewer"}); rec.Code != http.StatusNotFound {
			t.Errorf("PATCH /api/users/%s = %d, want 404", id, rec.Code)
		}
	}
}

func TestTheLastAdministratorIsProtectedEvenFromThemselves(t *testing.T) {
	e := newEnv(t)
	admin := e.mustLogin("admin")
	me := e.as(admin, http.MethodGet, "/api/auth/me", nil)
	self := decodeMe(t, me).User

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"demote":  e.as(admin, http.MethodPatch, userPath(self.ID, ""), map[string]any{"role": "viewer"}),
		"disable": e.as(admin, http.MethodPatch, userPath(self.ID, ""), map[string]any{"disabled": true}),
		"delete":  e.as(admin, http.MethodDelete, userPath(self.ID, ""), nil),
	} {
		if rec.Code != http.StatusConflict || errorText(t, rec) != "cannot remove the last administrator" {
			t.Errorf("%s: %d %q, want 409", name, rec.Code, errorText(t, rec))
		}
	}
	if rec := e.as(admin, http.MethodGet, "/api/auth/me", nil); rec.Code != http.StatusOK {
		t.Error("the refused changes still ended the session")
	}

	// With a second administrator the first may leave.
	adminCreates(t, e, admin, "second", "admin")
	if rec := e.as(admin, http.MethodPatch, userPath(self.ID, ""), map[string]any{"role": "viewer"}); rec.Code != http.StatusOK {
		t.Errorf("demoting one of two administrators: %d %s", rec.Code, rec.Body)
	}
}

func decodeMe(t *testing.T, rec *httptest.ResponseRecorder) meResponse {
	t.Helper()
	var out meResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body %q: %v", rec.Body, err)
	}
	return out
}

func TestResetPasswordIsTemporaryAndValidated(t *testing.T) {
	e := newEnv(t)
	admin := e.mustLogin("admin")
	u := e.addUser("olga", model.RoleOperator)

	for name, tc := range map[string]struct {
		body string
		code int
		want string
	}{
		"short":         {`{"password":"short"}`, 400, "password is shorter than the minimum"},
		"empty":         {`{"password":""}`, 400, "password is shorter than the minimum"},
		"unknown field": {`{"password":"a brand new password","force":true}`, 400, "invalid request body"},
	} {
		rec := e.as(admin, http.MethodPut, userPath(u.ID, "/password"), tc.body)
		if rec.Code != tc.code || errorText(t, rec) != tc.want {
			t.Errorf("%s: %d %q, want %d %q", name, rec.Code, errorText(t, rec), tc.code, tc.want)
		}
	}
	if rec := e.as(admin, http.MethodPut, userPath(999, "/password"), map[string]string{"password": "a brand new password"}); rec.Code != http.StatusNotFound {
		t.Errorf("unknown user: %d, want 404", rec.Code)
	}

	if rec := e.as(admin, http.MethodPut, userPath(u.ID, "/password"), map[string]string{"password": "a brand new password"}); rec.Code != http.StatusNoContent {
		t.Fatalf("reset: %d %s", rec.Code, rec.Body)
	}
	if _, rec := e.login("olga", goodPassword, from("198.18.7.1:1")); rec.Code == http.StatusOK {
		t.Error("the old password still works")
	}
	s, rec := e.login("olga", "a brand new password", from("198.18.7.2:1"))
	if rec.Code != http.StatusOK || !decodeMe(t, rec).User.MustChangePassword {
		t.Fatalf("login with the temporary password: %d %s", rec.Code, rec.Body)
	}
	if rec := e.as(s, http.MethodGet, "/api/devices", nil); rec.Code != http.StatusForbidden {
		t.Errorf("a temporary password may only be changed, got %d for the device list", rec.Code)
	}
	entries := auditActions(t, e, "user_password_reset")
	if len(entries) != 1 || strings.Contains(entries[0].Detail, "brand new") {
		t.Errorf("audit = %+v: must exist and must not hold the password", entries)
	}
}

func TestDeletedUserIsGone(t *testing.T) {
	e := newEnv(t)
	admin := e.mustLogin("admin")
	u := adminCreates(t, e, admin, "olga", "viewer")
	if rec := e.as(admin, http.MethodDelete, userPath(u.ID, ""), nil); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE: %d %s", rec.Code, rec.Body)
	}
	if rec := e.as(admin, http.MethodDelete, userPath(u.ID, ""), nil); rec.Code != http.StatusNotFound {
		t.Errorf("DELETE again: %d, want 404", rec.Code)
	}
	if _, rec := e.login("olga", goodPassword, from("198.18.8.1:1")); rec.Code != http.StatusUnauthorized {
		t.Errorf("deleted user signs in: %d", rec.Code)
	}
	if entries := auditActions(t, e, "user_delete"); len(entries) != 1 {
		t.Errorf("audit = %+v", entries)
	}
}

func TestOnlyAdministratorsManageUsers(t *testing.T) {
	e := newEnv(t)
	e.addUser("olga", model.RoleOperator)
	e.addUser("vera", model.RoleViewer)
	for _, name := range []string{"olga", "vera"} {
		s := e.mustLogin(name)
		for _, tc := range []struct {
			method, path string
			body         any
		}{
			{http.MethodGet, "/api/users", nil},
			{http.MethodPost, "/api/users", map[string]string{"username": "x-user", "role": "viewer", "password": goodPassword}},
			{http.MethodPatch, "/api/users/1", map[string]any{"role": "viewer"}},
			{http.MethodPut, "/api/users/1/password", map[string]string{"password": goodPassword}},
			{http.MethodDelete, "/api/users/1", nil},
			{http.MethodGet, "/api/audit", nil},
		} {
			if rec := e.as(s, tc.method, tc.path, tc.body); rec.Code != http.StatusForbidden {
				t.Errorf("%s %s %s = %d, want 403", name, tc.method, tc.path, rec.Code)
			}
		}
	}
}
