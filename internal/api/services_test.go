package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/collector"
	"github.com/FlexEbat/Netscribe/internal/model"
)

func seedServices(t *testing.T, e *env) map[string]int64 {
	t.Helper()
	ctx := context.Background()
	a := device("aa:aa:aa:aa:aa:01", "192.168.1.1", "gw", model.KindRouter)
	b := device("aa:aa:aa:aa:aa:02", "192.168.1.2", "nas", model.KindNAS)
	scan, err := e.store.StartScan(ctx, "192.168.1.0/24")
	if err != nil {
		t.Fatal(err)
	}
	err = e.store.ApplyResult(ctx, scan.ID, collector.Result{
		Devices: []model.DeviceInput{a, b},
		Services: []model.ServiceInput{
			{Device: b.Key, Port: 445, Proto: "tcp", Name: "microsoft-ds"},
			{Device: a.Key, Port: 443, Proto: "tcp", Name: "https"},
			{Device: a.Key, Port: 22, Proto: "tcp", Name: "ssh"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.FinishScan(ctx, scan.ID, "done", ""); err != nil {
		t.Fatal(err)
	}
	ids := map[string]int64{}
	for _, d := range listDevices(t, e, e.mustLogin("admin"), "") {
		ids[d.IP] = d.ID
	}
	return ids
}

func getServices(t *testing.T, e *env, query string) (int, []model.Service, string) {
	t.Helper()
	rec := e.as(e.mustLogin("admin"), http.MethodGet, "/api/services"+query, nil)
	var body struct {
		Services []model.Service `json:"services"`
	}
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
	}
	return rec.Code, body.Services, rec.Body.String()
}

func TestListServicesOfAllDevicesAndOfOne(t *testing.T) {
	e := newEnv(t)
	ids := seedServices(t, e)

	code, all, raw := getServices(t, e, "")
	if code != http.StatusOK || len(all) != 3 {
		t.Fatalf("all: %d %s", code, raw)
	}
	if all[0].Port != 22 || all[1].Port != 443 || all[2].Port != 445 || all[0].Name != "ssh" || all[0].Proto != "tcp" {
		t.Errorf("services = %+v, want ordered by device and port", all)
	}

	code, one, raw := getServices(t, e, "?deviceId="+strconv.FormatInt(ids["192.168.1.2"], 10))
	if code != http.StatusOK || len(one) != 1 || one[0].Port != 445 || one[0].DeviceID != ids["192.168.1.2"] {
		t.Errorf("one device: %d %s", code, raw)
	}
}

func TestListServicesIsAListNeverNull(t *testing.T) {
	e := newEnv(t)
	code, got, raw := getServices(t, e, "?deviceId=999")
	if code != http.StatusOK || len(got) != 0 || strings.TrimSpace(raw) != `{"services":[]}` {
		t.Errorf("%d %q", code, raw)
	}
}

func TestListServicesRejectsABadDeviceID(t *testing.T) {
	e := newEnv(t)
	for _, q := range []string{"?deviceId=abc", "?deviceId=0", "?deviceId=-4", "?deviceId=1.5"} {
		if code, _, raw := getServices(t, e, q); code != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", q, code, raw)
		}
	}
}

func TestListServicesNeedsASession(t *testing.T) {
	e := newEnv(t)
	if code := e.do(http.MethodGet, "/api/services", nil).Code; code != http.StatusUnauthorized {
		t.Errorf("without a session: %d, want 401", code)
	}
}

func TestGetDeviceIncludesItsServices(t *testing.T) {
	e := newEnv(t)
	ids := seedServices(t, e)
	rec := e.as(e.mustLogin("admin"), http.MethodGet, "/api/devices/"+strconv.FormatInt(ids["192.168.1.1"], 10), nil)
	var body struct {
		Services []model.Service `json:"services"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if len(body.Services) != 2 || body.Services[0].Port != 22 || body.Services[1].Port != 443 {
		t.Errorf("services = %+v", body.Services)
	}
	if !strings.Contains(rec.Body.String(), `"services":[`) {
		t.Errorf("services must be a list: %s", rec.Body)
	}
}
