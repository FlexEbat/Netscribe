package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func listOf(t *testing.T, e *env, s session) []sessionView {
	t.Helper()
	rec := asWith(e, s, http.MethodGet, "/api/auth/sessions", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var out []sessionView
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSessionsListsOnlyYourOwnAndMarksTheCurrentOne(t *testing.T) {
	e := newEnv(t)
	first := e.mustLogin("admin")
	e.mustLogin("admin")
	e.addUser("olga", "viewer")
	other := e.mustLogin("olga")

	got := listOf(t, e, first)
	if len(got) != 2 {
		t.Fatalf("%d sessions, want 2: %+v", len(got), got)
	}
	current := 0
	for _, v := range got {
		if v.Current {
			current++
		}
		if len(v.ID) != 20 || strings.Contains(v.ID, first.cookie.Value) {
			t.Errorf("odd id %q", v.ID)
		}
	}
	if current != 1 {
		t.Errorf("%d sessions marked current, want 1", current)
	}
	if n := len(listOf(t, e, other)); n != 1 {
		t.Errorf("the other user sees %d sessions, want 1", n)
	}

}

func TestSessionsCanBeEndedOneByOne(t *testing.T) {
	e := newEnv(t)
	first := e.mustLogin("admin")
	second := e.mustLogin("admin")
	var otherID string
	for _, v := range listOf(t, e, first) {
		if !v.Current {
			otherID = v.ID
		}
	}
	if code := asWith(e, first, http.MethodDelete, "/api/auth/sessions/"+otherID, nil).Code; code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", code)
	}
	if code := asWith(e, second, http.MethodGet, "/api/auth/me", nil).Code; code != http.StatusUnauthorized {
		t.Errorf("an ended session still works: status = %d", code)
	}
	if code := asWith(e, first, http.MethodGet, "/api/auth/me", nil).Code; code != http.StatusOK {
		t.Errorf("the session that ended the other one stopped: status = %d", code)
	}
	if code := asWith(e, first, http.MethodDelete, "/api/auth/sessions/"+otherID, nil).Code; code != http.StatusNotFound {
		t.Errorf("a second end of the same session: status = %d, want 404", code)
	}
}

func TestASessionOfAnotherPersonIsNotFound(t *testing.T) {
	e := newEnv(t)
	admin := e.mustLogin("admin")
	e.addUser("olga", "viewer")
	olga := e.mustLogin("olga")
	theirs := listOf(t, e, olga)[0].ID
	if code := asWith(e, admin, http.MethodDelete, "/api/auth/sessions/"+theirs, nil).Code; code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", code)
	}
	if code := asWith(e, olga, http.MethodGet, "/api/auth/me", nil).Code; code != http.StatusOK {
		t.Errorf("the other person's session was ended")
	}
}

func TestAllOtherSessionsEndAtOnceAndTheCurrentOneStays(t *testing.T) {
	e := newEnv(t)
	first := e.mustLogin("admin")
	second := e.mustLogin("admin")
	third := e.mustLogin("admin")
	rec := asWith(e, first, http.MethodDelete, "/api/auth/sessions", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ended":2`) {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	for name, s := range map[string]session{"second": second, "third": third} {
		if code := asWith(e, s, http.MethodGet, "/api/auth/me", nil).Code; code != http.StatusUnauthorized {
			t.Errorf("%s still works: %d", name, code)
		}
	}
	if code := asWith(e, first, http.MethodGet, "/api/auth/me", nil).Code; code != http.StatusOK {
		t.Errorf("the current session ended: %d", code)
	}
}

func TestSessionRoutesAreNotForTokensOrAnonymous(t *testing.T) {
	e := newEnv(t)
	admin := e.mustLogin("admin")
	tok := issueToken(t, e, admin, map[string]any{"name": "x", "scope": "write"})
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/auth/sessions"}, {http.MethodDelete, "/api/auth/sessions"}, {http.MethodDelete, "/api/auth/sessions/abc"},
	} {
		if code := e.do(c.method, c.path, nil, bearer(tok.Value)).Code; code != http.StatusForbidden {
			t.Errorf("%s %s with a token: %d, want 403", c.method, c.path, code)
		}
		if code := e.do(c.method, c.path, nil).Code; code != http.StatusUnauthorized {
			t.Errorf("%s %s without a session: %d, want 401", c.method, c.path, code)
		}
	}
}

func TestClipAgentKeepsPrintableCharactersOnly(t *testing.T) {
	got := clipAgent("Mozilla\x00/5.0\n" + strings.Repeat("x", 300))
	if strings.ContainsAny(got, "\x00\n") || len([]rune(got)) > 120 {
		t.Fatalf("%q", got)
	}
}
