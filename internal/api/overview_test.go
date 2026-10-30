package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/model"
)

func TestOverviewOnAnEmptyDatabaseHasZerosAndNoNulls(t *testing.T) {
	e := newEnv(t)
	rec := e.as(e.mustLogin("admin"), http.MethodGet, "/api/overview", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	want := `{"overview":{"devices":{"total":0,"online":0,"offline":0,"unknown":0,"newThisWeek":0},"risky":0,"lastScan":null,"recentScans":{"total":0,"failed":0},"changes":[]}}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s\nwant   %s", got, want)
	}
}

func TestOverviewAfterAScan(t *testing.T) {
	e := newEnv(t)
	seedServices(t, e)
	var body struct{ Overview model.Overview }
	rec := e.as(e.mustLogin("admin"), http.MethodGet, "/api/overview", nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	o := body.Overview
	if o.Devices.Total != 2 || o.Devices.Online != 2 || o.Devices.Unknown != 2 || o.Risky != 1 || o.LastScan == nil || o.RecentScans.Total != 1 {
		t.Errorf("overview = %+v", o)
	}
}

func TestOverviewAndHistoryNeedASessionButNotARole(t *testing.T) {
	e := newEnv(t)
	ids := seedServices(t, e)
	e.addUser("vera", model.RoleViewer)
	paths := []string{"/api/overview", "/api/devices/" + strconv.FormatInt(ids["192.168.1.1"], 10) + "/history"}
	for _, p := range paths {
		if rec := e.do(http.MethodGet, p, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("no session GET %s = %d, want 401", p, rec.Code)
		}
		if rec := e.as(e.mustLogin("vera"), http.MethodGet, p, nil); rec.Code != http.StatusOK {
			t.Errorf("viewer GET %s = %d, want 200", p, rec.Code)
		}
	}
}

func TestDeviceHistory(t *testing.T) {
	e := newEnv(t)
	ids := seedServices(t, e)
	s := e.mustLogin("admin")

	rec := e.as(s, http.MethodGet, "/api/devices/"+strconv.FormatInt(ids["192.168.1.1"], 10)+"/history", nil)
	var body struct{ History model.DeviceHistory }
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	h := body.History
	if h.Scans != 1 || h.Online != 1 || h.Uptime == nil || *h.Uptime != 100 || len(h.IPs) != 1 || h.IPs[0].IP != "192.168.1.1" || len(h.Points) != 1 {
		t.Errorf("history = %+v", h)
	}

	for _, id := range []string{"999", "abc", "0", "-1"} {
		if rec := e.as(s, http.MethodGet, "/api/devices/"+id+"/history", nil); rec.Code != http.StatusNotFound {
			t.Errorf("history of %s = %d, want 404", id, rec.Code)
		}
	}
}
