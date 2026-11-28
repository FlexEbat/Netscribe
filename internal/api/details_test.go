package api

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/model"
)

func patchDevice(e *env, s session, id int64, body map[string]any) (int, model.Device, string) {
	rec := e.as(s, http.MethodPatch, "/api/devices/"+strconv.FormatInt(id, 10), body)
	if rec.Code != http.StatusOK {
		return rec.Code, model.Device{}, rec.Body.String()
	}
	return rec.Code, decodeDevice(e.t, rec), ""
}

func TestDetailsAreSavedShownAndSearched(t *testing.T) {
	e := newEnv(t)
	e.addUser("olga", model.RoleOperator)
	s := e.mustLogin("olga")
	d := createManual(t, e, s, map[string]any{
		"label": "Core switch", "owner": "Ivanova", "location": "Rack 4", "serial": "ZX-900", "warrantyUntil": "2028-02-29",
		"tags": []string{"Core", "lab", "core"}, "fields": []map[string]string{{"name": "VLAN", "value": "20"}},
	})
	if d.Owner != "Ivanova" || d.Location != "Rack 4" || d.Serial != "ZX-900" || d.WarrantyUntil != "2028-02-29" ||
		!slices.Equal(d.Tags, []string{"core", "lab"}) || len(d.Fields) != 1 || d.Fields[0].Name != "VLAN" {
		t.Fatalf("created = %+v", d)
	}
	other := createManual(t, e, s, map[string]any{"label": "Printer"})
	if len(other.Tags) != 0 || other.Tags == nil || other.Fields == nil {
		t.Errorf("a device without details = %+v", other)
	}
	for q, want := range map[string]int{"?q=ivanova": 1, "?q=rack%204": 1, "?q=zx-9": 1, "?q=20": 1, "?tag=core": 1, "?tag=CORE": 1, "?tag=none": 0, "?q=nobody": 0} {
		if got := listDevices(t, e, s, q); len(got) != want {
			t.Errorf("GET /api/devices%s: %d devices, want %d", q, len(got), want)
		}
	}
	if rec := e.as(s, http.MethodGet, "/api/devices?tag=bad.tag", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("a bad tag filter: %d %s", rec.Code, rec.Body)
	}
}

func TestEditChangesDetailsAndKeepsWhatIsNotSent(t *testing.T) {
	e := newEnv(t)
	e.addUser("olga", model.RoleOperator)
	s := e.mustLogin("olga")
	d := createManual(t, e, s, map[string]any{"label": "X", "owner": "Ana", "location": "Rack 1", "tags": []string{"a"}, "fields": []map[string]string{{"name": "n", "value": "v"}}})
	code, got, msg := patchDevice(e, s, d.ID, map[string]any{"owner": "Boris", "tags": []string{"b", "c"}, "version": d.Version})
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, msg)
	}
	if got.Owner != "Boris" || got.Location != "Rack 1" || !slices.Equal(got.Tags, []string{"b", "c"}) || len(got.Fields) != 1 {
		t.Errorf("after the edit: %+v", got)
	}
	code, got, msg = patchDevice(e, s, d.ID, map[string]any{"fields": []map[string]string{}, "warrantyUntil": "", "version": got.Version})
	if code != http.StatusOK || len(got.Fields) != 0 || got.Fields == nil {
		t.Errorf("clearing the fields: %d %s %+v", code, msg, got)
	}
}

// Acceptance 1: a value over its limit is refused with the existing error style.
func TestDetailsOverTheirLimitsAreRefused(t *testing.T) {
	e := newEnv(t)
	e.addUser("olga", model.RoleOperator)
	s := e.mustLogin("olga")
	d := createManual(t, e, s, map[string]any{"label": "X"})
	twelve13 := make([]string, 13)
	for i := range twelve13 {
		twelve13[i] = "t" + strconv.Itoa(i)
	}
	eleven := make([]map[string]string, 11)
	for i := range eleven {
		eleven[i] = map[string]string{"name": "n" + strconv.Itoa(i), "value": ""}
	}
	cases := map[string]struct {
		body map[string]any
		want string
	}{
		"owner":       {map[string]any{"owner": strings.Repeat("o", 101)}, "owner is longer than 100 characters"},
		"location":    {map[string]any{"location": strings.Repeat("l", 101)}, "location is longer than 100 characters"},
		"serial":      {map[string]any{"serial": strings.Repeat("s", 65)}, "serial is longer than 64 characters"},
		"warranty":    {map[string]any{"warrantyUntil": "2027-02-29"}, "warranty must be a date as YYYY-MM-DD, or empty"},
		"tag chars":   {map[string]any{"tags": []string{"a.b"}}, "a tag is 1 to 30 characters of a-z, 0-9, space, _ and -"},
		"tag long":    {map[string]any{"tags": []string{strings.Repeat("t", 31)}}, "a tag is 1 to 30 characters of a-z, 0-9, space, _ and -"},
		"13 tags":     {map[string]any{"tags": twelve13}, "a device has at most 12 tags"},
		"11 fields":   {map[string]any{"fields": eleven}, "a device has at most 10 fields of your own"},
		"field name":  {map[string]any{"fields": []map[string]string{{"name": "", "value": "v"}}}, "the name of a field is 1 to 30 characters"},
		"field value": {map[string]any{"fields": []map[string]string{{"name": "n", "value": strings.Repeat("v", 201)}}}, "the value of a field is longer than 200 characters"},
		"same name":   {map[string]any{"fields": []map[string]string{{"name": "n", "value": "1"}, {"name": "n", "value": "2"}}}, "two fields have the same name"},
	}
	for name, c := range cases {
		patch := map[string]any{"version": d.Version}
		create := map[string]any{"label": "Y"}
		for k, v := range c.body {
			patch[k], create[k] = v, v
		}
		for what, rec := range map[string]*httptest.ResponseRecorder{
			"PATCH": e.as(s, http.MethodPatch, "/api/devices/"+strconv.FormatInt(d.ID, 10), patch),
			"POST":  e.as(s, http.MethodPost, "/api/devices", create),
		} {
			if rec.Code != http.StatusBadRequest || errorText(t, rec) != c.want {
				t.Errorf("%s %s: %d %q, want 400 %q", what, name, rec.Code, rec.Body, c.want)
			}
		}
	}
	if got := listDevices(t, e, s, ""); len(got) != 1 {
		t.Errorf("a refused create left %d devices, want 1", len(got))
	}
}

func TestBulkEditChangesTagsAndRefusesTooMany(t *testing.T) {
	e := newEnv(t)
	ids := seedServices(t, e)
	s := e.mustLogin("admin")
	gw, nas := ids["192.168.1.1"], ids["192.168.1.2"]
	rec := e.as(s, http.MethodPost, "/api/devices/bulk", map[string]any{"ids": []int64{gw, nas}, "addTags": []string{"Lab", "edge"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	rec = e.as(s, http.MethodPost, "/api/devices/bulk", map[string]any{"ids": []int64{gw}, "removeTags": []string{"lab"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	for _, d := range listDevices(t, e, s, "") {
		want := []string{"edge", "lab"}
		if d.ID == gw {
			want = []string{"edge"}
		}
		if !slices.Equal(d.Tags, want) {
			t.Errorf("device %d tags = %v, want %v", d.ID, d.Tags, want)
		}
	}
	if got := auditActions(t, e, "device_bulk_edit"); len(got) != 2 || !strings.Contains(got[1].Detail+got[0].Detail, "addTags") {
		t.Errorf("audit = %+v", got)
	}
	many := make([]string, 13)
	for i := range many {
		many[i] = "x" + strconv.Itoa(i)
	}
	for name, body := range map[string]map[string]any{
		"13 tags in one request": {"ids": []int64{gw}, "addTags": many},
		"a bad tag":              {"ids": []int64{gw}, "addTags": []string{"no!"}},
		"a bad removed tag":      {"ids": []int64{gw}, "removeTags": []string{""}},
	} {
		if rec := e.as(s, http.MethodPost, "/api/devices/bulk", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", name, rec.Code, rec.Body)
		}
	}
	// 12 tags on the gateway, then one more through a bulk edit that also lists the nas.
	var twelve []string
	for i := 0; i < 11; i++ {
		twelve = append(twelve, "f"+strconv.Itoa(i))
	}
	if rec := e.as(s, http.MethodPost, "/api/devices/bulk", map[string]any{"ids": []int64{gw}, "addTags": twelve}); rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	rec = e.as(s, http.MethodPost, "/api/devices/bulk", map[string]any{"ids": []int64{nas, gw}, "addTags": []string{"one-more"}})
	if rec.Code != http.StatusBadRequest || !strings.Contains(errorText(t, rec), "more than 12 tags") {
		t.Fatalf("%d %s, want 400 with the tag limit", rec.Code, rec.Body)
	}
	for _, d := range listDevices(t, e, s, "") {
		if slices.Contains(d.Tags, "one-more") {
			t.Errorf("device %d got the tag although the bulk edit failed", d.ID)
		}
	}
}
