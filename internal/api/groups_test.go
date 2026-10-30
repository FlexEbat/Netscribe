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

func decodeGroup(t *testing.T, rec *httptest.ResponseRecorder) model.Group {
	t.Helper()
	var body struct{ Group model.Group }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%v: %s", err, rec.Body)
	}
	return body.Group
}

func makeGroup(t *testing.T, e *env, s session, body map[string]any) model.Group {
	t.Helper()
	rec := e.as(s, http.MethodPost, "/api/groups", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/groups %v: %d %s", body, rec.Code, rec.Body)
	}
	return decodeGroup(t, rec)
}

func TestGroupLifecycle(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")
	g := makeGroup(t, e, s, map[string]any{"name": "Office", "vlan": 10, "ranges": []string{"192.168.1.0/24"}})
	if g.Name != "Office" || g.VLAN == nil || *g.VLAN != 10 || g.Version != 1 {
		t.Fatalf("created: %+v", g)
	}

	id := strconv.FormatInt(g.ID, 10)
	rec := e.as(s, http.MethodPatch, "/api/groups/"+id, map[string]any{"name": "HQ", "vlan": 0, "version": 1})
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH: %d %s", rec.Code, rec.Body)
	}
	if got := decodeGroup(t, rec); got.Name != "HQ" || got.VLAN != nil || len(got.Ranges) != 1 || got.Version != 2 {
		t.Errorf("patched: %+v", got)
	}
	if rec := e.as(s, http.MethodPatch, "/api/groups/"+id, map[string]any{"name": "X", "version": 1}); rec.Code != http.StatusConflict {
		t.Errorf("stale PATCH = %d, want 409", rec.Code)
	}

	var list struct{ Groups []model.Group }
	rec = e.as(s, http.MethodGet, "/api/groups", nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Groups) != 1 {
		t.Fatalf("list: %v %s", err, rec.Body)
	}
	if rec := e.as(s, http.MethodDelete, "/api/groups/"+id, nil); rec.Code != http.StatusNoContent {
		t.Errorf("DELETE = %d", rec.Code)
	}
	if rec := e.as(s, http.MethodDelete, "/api/groups/"+id, nil); rec.Code != http.StatusNotFound {
		t.Errorf("second DELETE = %d, want 404", rec.Code)
	}
	for _, a := range []string{"group_create", "group_edit", "group_delete"} {
		if got := auditActions(t, e, a); len(got) != 1 {
			t.Errorf("audit %s: %d entries, want 1", a, len(got))
		}
	}
}

func TestGroupValues(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")
	makeGroup(t, e, s, map[string]any{"name": "Office"})
	cases := map[string]struct {
		body map[string]any
		want int
	}{
		"name twice in capitals": {map[string]any{"name": "OFFICE"}, http.StatusConflict},
		"no name":                {map[string]any{"vlan": 5}, http.StatusBadRequest},
		"empty name":             {map[string]any{"name": " "}, http.StatusBadRequest},
		"41 characters":          {map[string]any{"name": strings.Repeat("a", 41)}, http.StatusBadRequest},
		"vlan 0 on create":       {map[string]any{"name": "a", "vlan": 0}, http.StatusBadRequest},
		"vlan 4095":              {map[string]any{"name": "b", "vlan": 4095}, http.StatusBadRequest},
		"host bits":              {map[string]any{"name": "c", "ranges": []string{"192.168.1.5/24"}}, http.StatusBadRequest},
		"ipv6 range":             {map[string]any{"name": "d", "ranges": []string{"fd00::/64"}}, http.StatusBadRequest},
		"17 ranges": {map[string]any{"name": "e", "ranges": func() []string {
			var r []string
			for i := 0; i < 17; i++ {
				r = append(r, "10.0."+strconv.Itoa(i)+".0/24")
			}
			return r
		}()}, http.StatusBadRequest},
		"unknown field": {map[string]any{"name": "f", "colour": "red"}, http.StatusBadRequest},
		"fine":          {map[string]any{"name": "Lab", "ranges": []string{}}, http.StatusCreated},
	}
	for name, tc := range cases {
		if rec := e.as(s, http.MethodPost, "/api/groups", tc.body); rec.Code != tc.want {
			t.Errorf("%s: %d %s, want %d", name, rec.Code, rec.Body, tc.want)
		}
	}
}

func TestGroupRoutesAreForEditorsAndReadableByViewers(t *testing.T) {
	e := newEnv(t)
	admin := e.mustLogin("admin")
	g := makeGroup(t, e, admin, map[string]any{"name": "Office"})
	e.addUser("vera", model.RoleViewer)
	e.addUser("olga", model.RoleOperator)
	id := strconv.FormatInt(g.ID, 10)

	if rec := e.do(http.MethodGet, "/api/groups", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("no session GET = %d, want 401", rec.Code)
	}
	if rec := e.as(e.mustLogin("vera"), http.MethodGet, "/api/groups", nil); rec.Code != http.StatusOK {
		t.Errorf("viewer GET = %d, want 200", rec.Code)
	}
	vera := e.mustLogin("vera")
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/api/groups", map[string]any{"name": "Nope"}},
		{http.MethodPatch, "/api/groups/" + id, map[string]any{"name": "Nope", "version": 1}},
		{http.MethodDelete, "/api/groups/" + id, nil},
		{http.MethodPost, "/api/devices/bulk", map[string]any{"ids": []int64{1}, "trusted": true}},
		{http.MethodPost, "/api/devices/import", "label\nx\n"},
	} {
		if rec := e.as(vera, c.method, c.path, c.body); rec.Code != http.StatusForbidden {
			t.Errorf("viewer %s %s = %d, want 403", c.method, c.path, rec.Code)
		}
		if rec := e.do(c.method, c.path, c.body); rec.Code != http.StatusUnauthorized {
			t.Errorf("no session %s %s = %d, want 401", c.method, c.path, rec.Code)
		}
	}
	if rec := e.as(e.mustLogin("olga"), http.MethodPost, "/api/groups", map[string]any{"name": "Ops"}); rec.Code != http.StatusCreated {
		t.Errorf("operator POST = %d, want 201", rec.Code)
	}
	if got := e.as(admin, http.MethodGet, "/api/groups", nil); !strings.Contains(got.Body.String(), `"Office"`) {
		t.Errorf("the viewer's attempts changed the group: %s", got.Body)
	}
}

func TestADeviceTakesItsGroupFromTheRangeAndFromAChoice(t *testing.T) {
	e := newEnv(t)
	ids := seedServices(t, e)
	s := e.mustLogin("admin")
	office := makeGroup(t, e, s, map[string]any{"name": "Office", "ranges": []string{"192.168.1.0/24"}})
	lab := makeGroup(t, e, s, map[string]any{"name": "Lab"})
	gw := ids["192.168.1.1"]

	byID := func(query string) []model.Device { return listDevices(t, e, s, query) }
	if got := byID("?group=" + strconv.FormatInt(office.ID, 10)); len(got) != 2 || got[0].Group == nil || !got[0].GroupAuto {
		t.Fatalf("by range: %+v", got)
	}
	var version int64
	for _, d := range byID("") {
		if d.ID == gw {
			version = d.Version
		}
	}
	rec := e.as(s, http.MethodPatch, "/api/devices/"+strconv.FormatInt(gw, 10), map[string]any{"groupId": lab.ID, "version": version})
	if rec.Code != http.StatusOK || decodeDevice(t, rec).Group.ID != lab.ID {
		t.Fatalf("PATCH groupId: %d %s", rec.Code, rec.Body)
	}
	if got := byID("?group=" + strconv.FormatInt(lab.ID, 10)); len(got) != 1 || got[0].ID != gw || got[0].GroupAuto {
		t.Errorf("chosen group: %+v", got)
	}
	if got := byID("?group=none"); len(got) != 0 {
		t.Errorf("none filter = %+v, want no device (the other one is in the range)", got)
	}
	for _, bad := range []string{"?group=abc", "?group=0", "?group=-1"} {
		if rec := e.as(s, http.MethodGet, "/api/devices"+bad, nil); rec.Code != http.StatusBadRequest {
			t.Errorf("GET %s = %d, want 400", bad, rec.Code)
		}
	}
	rec = e.as(s, http.MethodPatch, "/api/devices/"+strconv.FormatInt(gw, 10), map[string]any{"groupId": 999, "version": version + 1})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown group = %d %s, want 400", rec.Code, rec.Body)
	}
}
