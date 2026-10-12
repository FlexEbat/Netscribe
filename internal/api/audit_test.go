package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
)

type auditOut struct {
	Entries []model.AuditEntry `json:"entries"`
}

func readAudit(t *testing.T, e *env, s session, query string) []model.AuditEntry {
	t.Helper()
	rec := e.as(s, http.MethodGet, "/api/audit"+query, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/audit%s: %d %s", query, rec.Code, rec.Body)
	}
	var out auditOut
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Entries
}

func seedAudit(t *testing.T, e *env) {
	t.Helper()
	for i, entry := range []struct {
		user, action, result string
		at                   time.Duration
	}{
		{"alice", "login", "ok", 0},
		{"bob", "login_failed", "denied", 24 * time.Hour},
		{"alice", "device_edit", "ok", 48 * time.Hour},
		{"bob", "access_denied", "denied", 72 * time.Hour},
		{"alice", "logout", "ok", 96 * time.Hour},
	} {
		at := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC).Add(entry.at)
		if err := e.store.AddAudit(t.Context(), model.AuditEntry{At: at, Username: entry.user, Action: entry.action, Result: entry.result}); err != nil {
			t.Fatalf("entry %d: %v", i, err)
		}
	}
}

func TestAuditIsReadNewestFirstWithFilters(t *testing.T) {
	e := newEnv(t)
	seedAudit(t, e)
	s := e.mustLogin("admin")

	count := func(query string) int { return len(readAudit(t, e, s, query)) }
	// The sign-in of the test administrator is in the log too, so filter around it.
	if got := readAudit(t, e, s, "?user=alice"); len(got) != 3 || got[0].Action != "logout" || got[2].Action != "login" {
		t.Errorf("user=alice: %+v, want logout, device_edit, login", got)
	}
	if n := count("?user=bob"); n != 2 {
		t.Errorf("user=bob: %d, want 2", n)
	}
	if n := count("?action=access_denied&user=bob"); n != 1 {
		t.Errorf("action and user: %d, want 1", n)
	}
	if n := count("?result=denied&user=bob"); n != 2 {
		t.Errorf("result=denied: %d, want 2", n)
	}
	if n := count("?user=alice&from=2026-01-03&to=2026-01-05"); n != 1 {
		t.Errorf("period by dates: %d, want device_edit only", n)
	}
	if n := count("?user=alice&from=" + url.QueryEscape("2026-01-03T12:00:00Z") + "&to=" + url.QueryEscape("2026-01-05T12:00:00Z")); n != 1 {
		t.Errorf("period by timestamps: %d, want 1 (from is included, to is not)", n)
	}
}

func TestAuditPagesWithBefore(t *testing.T) {
	e := newEnv(t)
	seedAudit(t, e)
	s := e.mustLogin("admin")

	first := readAudit(t, e, s, "?user=alice&limit=2")
	if len(first) != 2 {
		t.Fatalf("first page: %d entries", len(first))
	}
	second := readAudit(t, e, s, "?user=alice&limit=2&before="+strconv.FormatInt(first[1].ID, 10))
	if len(second) != 1 || second[0].Action != "login" {
		t.Errorf("second page: %+v, want the remaining login", second)
	}
	if rest := readAudit(t, e, s, "?user=alice&limit=2&before="+strconv.FormatInt(second[0].ID, 10)); len(rest) != 0 {
		t.Errorf("third page: %+v, want nothing", rest)
	}
	if rec := e.as(s, http.MethodGet, "/api/audit?user=nobody", nil); rec.Body.String() != "{\"entries\":[]}\n" {
		t.Errorf("empty result: %q, want an empty array", rec.Body)
	}
}

func TestAuditParameterValidation(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")
	for query, want := range map[string]string{
		"?limit=0":                         "limit is out of range",
		"?limit=501":                       "limit is out of range",
		"?limit=abc":                       "limit is out of range",
		"?limit=-1":                        "limit is out of range",
		"?before=abc":                      "before is not valid",
		"?before=0":                        "before is not valid",
		"?result=maybe":                    "result is unknown",
		"?from=yesterday":                  "from is not a date",
		"?to=2026-13-45":                   "to is not a date",
		"?user=" + strings.Repeat("u", 80): "filter is too long",
	} {
		rec := e.as(s, http.MethodGet, "/api/audit"+query, nil)
		if rec.Code != http.StatusBadRequest || errorText(t, rec) != want {
			t.Errorf("%s: %d %q, want 400 %q", query, rec.Code, errorText(t, rec), want)
		}
	}
	if rows := readAudit(t, e, s, "?limit=500"); rows == nil {
		t.Error("limit=500 must be accepted")
	}
}

func TestTheAuditLogCannotBeChangedThroughTheAPI(t *testing.T) {
	e := newEnv(t)
	seedAudit(t, e)
	s := e.mustLogin("admin")
	before := len(readAudit(t, e, s, ""))
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		for _, path := range []string{"/api/audit", "/api/audit/1"} {
			rec := e.as(s, method, path, `{}`)
			if rec.Code < 400 {
				t.Errorf("%s %s = %d, want an error: the log has no write routes", method, path, rec.Code)
			}
		}
	}
	if after := len(readAudit(t, e, s, "")); after < before {
		t.Errorf("entries went from %d to %d", before, after)
	}
}

func TestAuditRecordsTheEventsOfSection98(t *testing.T) {
	e := newEnv(t)
	e.addUser("vera", model.RoleViewer)
	admin := e.mustLogin("admin")
	vera := e.mustLogin("vera")
	e.as(vera, http.MethodGet, "/api/users", nil) // access_denied
	e.login("admin", "wrong wrong wrong", from("198.18.5.5:1"))
	e.as(admin, http.MethodPost, "/api/auth/logout", nil)

	seen := map[string]bool{}
	admin = e.mustLogin("admin")
	for _, entry := range readAudit(t, e, admin, "?limit=500") {
		seen[entry.Action] = true
	}
	for _, action := range []string{"login", "login_failed", "logout", "access_denied"} {
		if !seen[action] {
			t.Errorf("no %q entry in the log: %v", action, seen)
		}
	}
}
