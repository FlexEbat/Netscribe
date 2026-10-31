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

func saveFilter(t *testing.T, e *env, s session, body map[string]any) (*httptest.ResponseRecorder, model.SavedFilter) {
	t.Helper()
	rec := e.as(s, http.MethodPost, "/api/filters", body)
	var out struct{ Filter model.SavedFilter }
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out.Filter
}

func listFilterNames(t *testing.T, e *env, s session, page string) []string {
	t.Helper()
	rec := e.as(s, http.MethodGet, "/api/filters?page="+page, nil)
	var out struct{ Filters []model.SavedFilter }
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
		t.Fatalf("GET filters: %d %s", rec.Code, rec.Body)
	}
	names := []string{}
	for _, f := range out.Filters {
		names = append(names, f.Name)
	}
	return names
}

func TestAFilterIsStoredInCanonicalFormAndListedNewestFirst(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")
	rec, f := saveFilter(t, e, s, map[string]any{"page": "inventory", "name": " Unknown offline ", "query": "trust=unknown&status=offline&group="})
	if rec.Code != http.StatusCreated || f.Name != "Unknown offline" || f.Query != "status=offline&trust=unknown" || f.Page != "inventory" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	saveFilter(t, e, s, map[string]any{"page": "inventory", "name": "Cameras", "query": "q=cam"})
	if got := strings.Join(listFilterNames(t, e, s, "inventory"), ","); got != "Cameras,Unknown offline" {
		t.Errorf("list = %s, want newest first", got)
	}
	if got := listFilterNames(t, e, s, "services"); len(got) != 0 {
		t.Errorf("services list = %v", got)
	}
}

func TestAnotherPersonCannotSeeOrDeleteMyFilter(t *testing.T) {
	e := newEnv(t)
	admin := e.mustLogin("admin")
	_, f := saveFilter(t, e, admin, map[string]any{"page": "inventory", "name": "Mine", "query": "q=x"})
	e.addUser("vera", model.RoleViewer)
	vera := e.mustLogin("vera")

	if got := listFilterNames(t, e, vera, "inventory"); len(got) != 0 {
		t.Errorf("vera sees %v", got)
	}
	if rec := e.as(vera, http.MethodDelete, "/api/filters/"+strconv.FormatInt(f.ID, 10), nil); rec.Code != http.StatusNotFound {
		t.Errorf("vera deleting admin's filter = %d, want 404", rec.Code)
	}
	if got := listFilterNames(t, e, admin, "inventory"); len(got) != 1 {
		t.Errorf("the filter is gone after a refused delete: %v", got)
	}
	if rec := e.as(admin, http.MethodDelete, "/api/filters/"+strconv.FormatInt(f.ID, 10), nil); rec.Code != http.StatusNoContent {
		t.Errorf("owner deleting = %d, want 204", rec.Code)
	}
	if rec := e.as(admin, http.MethodDelete, "/api/filters/"+strconv.FormatInt(f.ID, 10), nil); rec.Code != http.StatusNotFound {
		t.Errorf("second delete = %d, want 404", rec.Code)
	}
}

func TestAViewerKeepsHisOwnFiltersAndANoSessionRequestIsRefused(t *testing.T) {
	e := newEnv(t)
	e.addUser("vera", model.RoleViewer)
	vera := e.mustLogin("vera")
	if rec, _ := saveFilter(t, e, vera, map[string]any{"page": "services", "name": "ssh", "query": "q=ssh"}); rec.Code != http.StatusCreated {
		t.Errorf("viewer POST = %d %s, want 201", rec.Code, rec.Body)
	}
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/filters?page=inventory"},
		{http.MethodPost, "/api/filters"},
		{http.MethodDelete, "/api/filters/1"},
	} {
		if rec := e.do(c.method, c.path, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("no session %s %s = %d, want 401", c.method, c.path, rec.Code)
		}
	}
}

func TestATokenOfScopeReadCannotSaveAFilter(t *testing.T) {
	e := newEnv(t)
	admin := e.mustLogin("admin")
	tok := issueToken(t, e, admin, map[string]any{"name": "ro", "scope": "read", "days": 30})
	if rec := withToken(e, tok.Value, http.MethodGet, "/api/filters?page=inventory", nil); rec.Code != http.StatusOK {
		t.Errorf("token GET = %d, want 200", rec.Code)
	}
	if rec := withToken(e, tok.Value, http.MethodPost, "/api/filters", map[string]any{"page": "inventory", "name": "x", "query": "q=x"}); rec.Code != http.StatusForbidden {
		t.Errorf("read token POST = %d %s, want 403", rec.Code, rec.Body)
	}
}

func TestFilterValues(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")
	saveFilter(t, e, s, map[string]any{"page": "inventory", "name": "Taken", "query": "q=x"})
	cases := map[string]struct {
		body map[string]any
		want int
	}{
		"unknown page":    {map[string]any{"page": "topology", "name": "a", "query": "q=x"}, http.StatusBadRequest},
		"no page":         {map[string]any{"name": "a", "query": "q=x"}, http.StatusBadRequest},
		"empty name":      {map[string]any{"page": "inventory", "name": " ", "query": "q=x"}, http.StatusBadRequest},
		"41 characters":   {map[string]any{"page": "inventory", "name": strings.Repeat("a", 41), "query": "q=x"}, http.StatusBadRequest},
		"control in name": {map[string]any{"page": "inventory", "name": "a\u0000b", "query": "q=x"}, http.StatusBadRequest},
		"unknown key":     {map[string]any{"page": "inventory", "name": "a", "query": "colour=red"}, http.StatusBadRequest},
		"key of a page":   {map[string]any{"page": "services", "name": "a", "query": "trust=unknown"}, http.StatusBadRequest},
		"repeated key":    {map[string]any{"page": "inventory", "name": "a", "query": "q=a&q=b"}, http.StatusBadRequest},
		"bad status":      {map[string]any{"page": "inventory", "name": "a", "query": "status=up"}, http.StatusBadRequest},
		"bad trust":       {map[string]any{"page": "inventory", "name": "a", "query": "trust=yes"}, http.StatusBadRequest},
		"bad group":       {map[string]any{"page": "inventory", "name": "a", "query": "group=x"}, http.StatusBadRequest},
		"query of 301":    {map[string]any{"page": "inventory", "name": "a", "query": "q=" + strings.Repeat("a", 299)}, http.StatusBadRequest},
		"only empties":    {map[string]any{"page": "inventory", "name": "a", "query": "q=&status="}, http.StatusBadRequest},
		"same name":       {map[string]any{"page": "inventory", "name": "TAKEN", "query": "q=y"}, http.StatusConflict},
		"unknown field":   {map[string]any{"page": "inventory", "name": "a", "query": "q=x", "shared": true}, http.StatusBadRequest},
		"fine":            {map[string]any{"page": "containers", "name": "nginx", "query": "q=nginx"}, http.StatusCreated},
	}
	for name, tc := range cases {
		if rec := e.as(s, http.MethodPost, "/api/filters", tc.body); rec.Code != tc.want {
			t.Errorf("%s: %d %s, want %d", name, rec.Code, rec.Body, tc.want)
		}
	}
	if rec := e.as(s, http.MethodGet, "/api/filters", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("GET without a page = %d, want 400", rec.Code)
	}
}

func TestThe21stFilterIsRefused(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")
	for i := 0; i < model.MaxFiltersPerPage; i++ {
		if rec, _ := saveFilter(t, e, s, map[string]any{"page": "services", "name": "f" + strconv.Itoa(i), "query": "q=x"}); rec.Code != http.StatusCreated {
			t.Fatalf("filter %d: %d %s", i, rec.Code, rec.Body)
		}
	}
	rec, _ := saveFilter(t, e, s, map[string]any{"page": "services", "name": "extra", "query": "q=x"})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "at most 20") {
		t.Errorf("21st filter: %d %s", rec.Code, rec.Body)
	}
}
