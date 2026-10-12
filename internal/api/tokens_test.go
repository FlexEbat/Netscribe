package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
)

type issued struct {
	Value string         `json:"value"`
	Token model.APIToken `json:"token"`
}

func issueToken(t *testing.T, e *env, s session, body map[string]any) issued {
	t.Helper()
	rec := e.as(s, http.MethodPost, "/api/tokens", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/tokens %v: %d %s", body, rec.Code, rec.Body)
	}
	var out issued
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func bearer(token string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
}

func withToken(e *env, token, method, path string, body any) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.do(method, path, body, bearer(token))
}

func TestTokenValueIsShownOnceAndListedOnlyByPrefix(t *testing.T) {
	e := newEnv(t)
	e.addUser("olga", model.RoleOperator)
	s := e.mustLogin("olga")

	got := issueToken(t, e, s, map[string]any{"name": "ci", "scope": "write", "days": 30})
	if !strings.HasPrefix(got.Value, "ndt_") || got.Token.Prefix != got.Value[:8] || got.Token.Scope != "write" {
		t.Errorf("issued = %+v", got)
	}
	if want := e.clk.now().AddDate(0, 0, 30); !got.Token.ExpiresAt.Equal(want) {
		t.Errorf("expires %v, want %v", got.Token.ExpiresAt, want)
	}

	list := e.as(s, http.MethodGet, "/api/tokens", nil)
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), got.Value) || strings.Contains(strings.ToLower(list.Body.String()), "hash") {
		t.Errorf("list leaks the token or its hash: %s", list.Body)
	}
	if !strings.Contains(list.Body.String(), got.Token.Prefix) {
		t.Errorf("list has no prefix: %s", list.Body)
	}
	entries := auditActions(t, e, "token_create")
	if len(entries) != 1 || strings.Contains(entries[0].Detail, got.Value) {
		t.Errorf("audit = %+v: must exist and must not hold the token", entries)
	}
	if strings.Contains(e.logs.String(), got.Value) {
		t.Error("the token value reached the log")
	}
}

func TestTokenDefaultsAndValidation(t *testing.T) {
	e := newEnv(t)
	e.addUser("olga", model.RoleOperator)
	e.addUser("vera", model.RoleViewer)
	op, viewer := e.mustLogin("olga"), e.mustLogin("vera")

	def := issueToken(t, e, op, map[string]any{"name": "default"})
	if def.Token.Scope != "read" || !def.Token.ExpiresAt.Equal(e.clk.now().AddDate(0, 0, 90)) {
		t.Errorf("defaults = %+v, want read and 90 days", def.Token)
	}
	issueToken(t, e, op, map[string]any{"name": "year", "days": 365})

	for name, tc := range map[string]struct {
		s    session
		body any
		code int
		want string
	}{
		"366 days":        {op, map[string]any{"name": "x", "days": 366}, 400, "token lifetime is out of range"},
		"negative days":   {op, map[string]any{"name": "x", "days": -5}, 400, "token lifetime is out of range"},
		"no name":         {op, map[string]any{"scope": "read"}, 400, "token name is invalid"},
		"long name":       {op, map[string]any{"name": strings.Repeat("n", 65)}, 400, "token name is invalid"},
		"unknown scope":   {op, map[string]any{"name": "x", "scope": "admin"}, 400, "scope is unknown"},
		"write by viewer": {viewer, map[string]any{"name": "x", "scope": "write"}, 400, "scope is above your role"},
		"unknown field":   {op, map[string]any{"name": "x", "expires": "never"}, 400, "invalid request body"},
		"not json":        {op, `x`, 400, "invalid request body"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := e.as(tc.s, http.MethodPost, "/api/tokens", tc.body)
			if rec.Code != tc.code || errorText(t, rec) != tc.want {
				t.Errorf("status %d, error %q, want %d %q", rec.Code, errorText(t, rec), tc.code, tc.want)
			}
		})
	}
}

func TestOnlyTwentyActiveTokens(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")
	for i := 0; i < 20; i++ {
		issueToken(t, e, s, map[string]any{"name": "t" + strconv.Itoa(i)})
	}
	rec := e.as(s, http.MethodPost, "/api/tokens", map[string]any{"name": "one too many"})
	if rec.Code != http.StatusConflict || errorText(t, rec) != "too many active tokens" {
		t.Errorf("21st token: %d %q, want 409", rec.Code, errorText(t, rec))
	}
}

func TestATokenSignsInWithoutCookieOrCSRF(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")
	tok := issueToken(t, e, s, map[string]any{"name": "script", "scope": "write"})

	if rec := withToken(e, tok.Value, http.MethodGet, "/api/devices", nil); rec.Code != http.StatusOK {
		t.Errorf("GET with a token: %d %s", rec.Code, rec.Body)
	}
	// No X-CSRF-Token and no cookie, and a state change still works.
	rec := withToken(e, tok.Value, http.MethodPost, "/api/devices", map[string]string{"label": "from a script"})
	if rec.Code != http.StatusCreated {
		t.Errorf("POST with a write token: %d %s", rec.Code, rec.Body)
	}
	me := withToken(e, tok.Value, http.MethodGet, "/api/auth/me", nil)
	got := decodeMe(t, me)
	if me.Code != http.StatusOK || got.CSRFToken != "" || len(got.Permissions) != 4 {
		t.Errorf("me via token: %d %+v, want 4 rights and no CSRF token", me.Code, got)
	}
}

func TestAReadTokenCannotChangeAnything(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")
	tok := issueToken(t, e, s, map[string]any{"name": "ro", "scope": "read"})

	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/api/devices", map[string]string{"label": "x"}},
		{http.MethodPut, "/api/devices/1/position", map[string]float64{"x": 1, "y": 1}},
		{http.MethodPost, "/api/links", map[string]any{"aDeviceId": 1, "bDeviceId": 2}},
	} {
		if rec := withToken(e, tok.Value, tc.method, tc.path, tc.body); rec.Code != http.StatusForbidden {
			t.Errorf("read token %s %s = %d, want 403", tc.method, tc.path, rec.Code)
		}
	}
	if rec := withToken(e, tok.Value, http.MethodGet, "/api/topology", nil); rec.Code != http.StatusOK {
		t.Errorf("read token GET /api/topology = %d, want 200", rec.Code)
	}
	if got := listDevices(t, e, s, ""); len(got) != 0 {
		t.Errorf("the read token created %d devices", len(got))
	}
}

func TestNoTokenOpensUsersAuditTokensOrAuthRoutes(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin") // an administrator, whose token must still stay out of these
	for _, scope := range []string{"read", "write"} {
		tok := issueToken(t, e, s, map[string]any{"name": scope, "scope": scope})
		for _, tc := range []struct {
			method, path string
			body         any
		}{
			{http.MethodGet, "/api/users", nil},
			{http.MethodPost, "/api/users", map[string]string{"username": "evil", "role": "admin", "password": goodPassword}},
			{http.MethodPatch, "/api/users/1", map[string]any{"role": "viewer"}},
			{http.MethodPut, "/api/users/1/password", map[string]string{"password": goodPassword}},
			{http.MethodDelete, "/api/users/1", nil},
			{http.MethodGet, "/api/audit", nil},
			{http.MethodGet, "/api/tokens", nil},
			{http.MethodPost, "/api/tokens", map[string]any{"name": "more"}},
			{http.MethodDelete, "/api/tokens/1", nil},
			{http.MethodPost, "/api/auth/logout", nil},
			{http.MethodPut, "/api/auth/password", map[string]string{"currentPassword": goodPassword, "newPassword": "another long password"}},
		} {
			if rec := withToken(e, tok.Value, tc.method, tc.path, tc.body); rec.Code != http.StatusForbidden {
				t.Errorf("%s token %s %s = %d, want 403", scope, tc.method, tc.path, rec.Code)
			}
		}
	}
	if users := e.as(s, http.MethodGet, "/api/users", nil); strings.Contains(users.Body.String(), "evil") {
		t.Error("a token created a user")
	}
}

func TestRevokedExpiredAndOrphanedTokensAreRefused(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")

	revoked := issueToken(t, e, s, map[string]any{"name": "revoked"})
	if rec := e.as(s, http.MethodDelete, "/api/tokens/"+strconv.FormatInt(revoked.Token.ID, 10), nil); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d", rec.Code)
	}
	if rec := withToken(e, revoked.Value, http.MethodGet, "/api/devices", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("revoked token: %d, want 401", rec.Code)
	}

	short := issueToken(t, e, s, map[string]any{"name": "short", "days": 1})
	if rec := withToken(e, short.Value, http.MethodGet, "/api/devices", nil); rec.Code != http.StatusOK {
		t.Fatalf("fresh token: %d", rec.Code)
	}
	e.clk.advance(24*time.Hour + time.Second)
	if rec := withToken(e, short.Value, http.MethodGet, "/api/devices", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("expired token: %d, want 401", rec.Code)
	}
	s = e.mustLogin("admin") // the sessions ended with the day

	e.addUser("olga", model.RoleOperator)
	owner := e.mustLogin("olga")
	mine := issueToken(t, e, owner, map[string]any{"name": "mine"})
	var olga model.User
	for _, u := range listUsers(t, e, s) {
		if u.Username == "olga" {
			olga = u
		}
	}
	if rec := e.as(s, http.MethodPatch, userPath(olga.ID, ""), map[string]any{"disabled": true}); rec.Code != http.StatusOK {
		t.Fatalf("disable: %d", rec.Code)
	}
	if rec := withToken(e, mine.Value, http.MethodGet, "/api/devices", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("token of a disabled user: %d, want 401", rec.Code)
	}
	if entries := auditActions(t, e, "token_revoke"); len(entries) != 1 {
		t.Errorf("token_revoke audit = %+v", entries)
	}
}

func listUsers(t *testing.T, e *env, s session) []model.User {
	t.Helper()
	rec := e.as(s, http.MethodGet, "/api/users", nil)
	var body struct {
		Users []model.User `json:"users"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Users
}

func TestBadAuthorizationHeadersAreRefused(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")
	for name, header := range map[string]string{
		"wrong scheme":      "Token ndt_abc",
		"basic":             "Basic YWRtaW46cGFzcw==",
		"no value":          "Bearer",
		"garbage":           "Bearer not-a-token",
		"empty bearer":      "Bearer ",
		"session id as one": "Bearer " + s.cookie.Value,
	} {
		rec := e.do(http.MethodGet, "/api/devices", nil, func(r *http.Request) { r.Header.Set("Authorization", header) })
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: %d, want 401", name, rec.Code)
		}
	}
	// A request that presents a token is judged by the token, not by a cookie beside it.
	rec := e.do(http.MethodGet, "/api/devices", nil, func(r *http.Request) {
		r.AddCookie(s.cookie)
		r.Header.Set("Authorization", "Bearer ndt_wrong")
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("good cookie with a bad token: %d, want 401", rec.Code)
	}
}

func TestOnlyTheOwnerCanRevokeAToken(t *testing.T) {
	e := newEnv(t)
	e.addUser("olga", model.RoleOperator)
	admin, olga := e.mustLogin("admin"), e.mustLogin("olga")
	tok := issueToken(t, e, olga, map[string]any{"name": "mine"})
	path := "/api/tokens/" + strconv.FormatInt(tok.Token.ID, 10)

	if rec := e.as(admin, http.MethodDelete, path, nil); rec.Code != http.StatusNotFound {
		t.Errorf("an administrator revoking someone else's token: %d, want 404", rec.Code)
	}
	if rec := withToken(e, tok.Value, http.MethodGet, "/api/devices", nil); rec.Code != http.StatusOK {
		t.Error("the token stopped working")
	}
	for _, id := range []string{"abc", "0", "999"} {
		if rec := e.as(olga, http.MethodDelete, "/api/tokens/"+id, nil); rec.Code != http.StatusNotFound {
			t.Errorf("DELETE /api/tokens/%s = %d, want 404", id, rec.Code)
		}
	}
	if own := e.as(admin, http.MethodGet, "/api/tokens", nil); strings.Contains(own.Body.String(), tok.Token.Prefix) {
		t.Error("the list of an administrator shows someone else's token")
	}
}
