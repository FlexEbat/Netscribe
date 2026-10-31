package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/model"
)

func TestBulkEditChangesEveryListedDeviceAndNoOther(t *testing.T) {
	e := newEnv(t)
	ids := seedServices(t, e)
	s := e.mustLogin("admin")
	g := makeGroup(t, e, s, map[string]any{"name": "Lab"})
	gw, nas := ids["192.168.1.1"], ids["192.168.1.2"]

	rec := e.as(s, http.MethodPost, "/api/devices/bulk", map[string]any{"ids": []int64{gw}, "trusted": true, "kind": "router", "groupId": g.ID})
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"updated":1}` {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	for _, d := range listDevices(t, e, s, "") {
		changed := d.ID == gw
		if d.Trusted != changed || (d.Group != nil && d.Group.ID == g.ID) != changed || (d.Kind == model.KindRouter && d.KindLocked) != changed {
			t.Errorf("device %d (changed=%v): %+v", d.ID, changed, d)
		}
		if d.ID == nas && d.Version != 1 {
			t.Errorf("an unlisted device was touched: version %d", d.Version)
		}
	}
	if got := auditActions(t, e, "device_bulk_edit"); len(got) != 1 || !strings.Contains(got[0].Detail, "1 devices (trusted, kind, groupId)") {
		t.Errorf("audit = %+v", got)
	}
}

func TestBulkEditWithAMissingDeviceChangesNothing(t *testing.T) {
	e := newEnv(t)
	ids := seedServices(t, e)
	s := e.mustLogin("admin")
	rec := e.as(s, http.MethodPost, "/api/devices/bulk", map[string]any{"ids": []int64{ids["192.168.1.1"], 9999}, "trusted": true})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("%d %s, want 404", rec.Code, rec.Body)
	}
	if got := listDevices(t, e, s, "?trusted=true"); len(got) != 0 {
		t.Errorf("%d devices were trusted although the request failed", len(got))
	}
}

func TestBulkEditRefusesBadRequests(t *testing.T) {
	e := newEnv(t)
	seedServices(t, e)
	s := e.mustLogin("admin")
	many := make([]int64, 201)
	for i := range many {
		many[i] = int64(i + 1)
	}
	for name, body := range map[string]map[string]any{
		"no ids":            {"trusted": true},
		"empty ids":         {"ids": []int64{}, "trusted": true},
		"201 ids":           {"ids": many, "trusted": true},
		"duplicate ids":     {"ids": []int64{1, 1}, "trusted": true},
		"zero id":           {"ids": []int64{0}, "trusted": true},
		"nothing to change": {"ids": []int64{1}},
		"empty kind":        {"ids": []int64{1}, "kind": ""},
		"unknown kind":      {"ids": []int64{1}, "kind": "toaster"},
		"negative group":    {"ids": []int64{1}, "groupId": -1},
		"unknown group":     {"ids": []int64{1}, "groupId": 999},
		"unknown field":     {"ids": []int64{1}, "trusted": true, "label": "x"},
	} {
		if rec := e.as(s, http.MethodPost, "/api/devices/bulk", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", name, rec.Code, rec.Body)
		}
	}
}

func TestBulkEditRaisesEachVersionOnce(t *testing.T) {
	e := newEnv(t)
	ids := seedServices(t, e)
	s := e.mustLogin("admin")
	e.as(s, http.MethodPost, "/api/devices/bulk", map[string]any{"ids": []int64{ids["192.168.1.1"], ids["192.168.1.2"]}, "trusted": true})
	for _, d := range listDevices(t, e, s, "") {
		if d.Version != 2 {
			t.Errorf("device %d version = %d, want 2 after one bulk edit", d.ID, d.Version)
		}
	}
	var raw map[string]int
	rec := e.as(s, http.MethodPost, "/api/devices/bulk", map[string]any{"ids": []int64{ids["192.168.1.1"]}, "groupId": 0})
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil || raw["updated"] != 1 {
		t.Errorf("clearing a group that was not set: %d %s", rec.Code, rec.Body)
	}
}
