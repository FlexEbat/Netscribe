package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/collector"
	"github.com/FlexEbat/Netscribe/internal/model"
)

// seedScan stores one finished scan over 192.168.1.0/24 with the given devices.
func seedScan(t *testing.T, e *env, devs ...model.DeviceInput) model.Scan {
	t.Helper()
	ctx := context.Background()
	scan, err := e.store.StartScan(ctx, "192.168.1.0/24")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.ApplyResult(ctx, scan.ID, collector.Result{Devices: devs}); err != nil {
		t.Fatal(err)
	}
	if err := e.store.FinishScan(ctx, scan.ID, "done", ""); err != nil {
		t.Fatal(err)
	}
	done, err := e.store.GetScan(ctx, scan.ID)
	if err != nil {
		t.Fatal(err)
	}
	return done
}

func device(mac, ip, host string, kind model.DeviceKind) model.DeviceInput {
	return model.DeviceInput{Key: model.DeviceKey{MAC: mac, IP: ip}, Hostname: host, Kind: kind, Source: "arp"}
}

type devicesBody struct {
	Devices []model.Device `json:"devices"`
}

func listDevices(t *testing.T, e *env, s session, query string) []model.Device {
	t.Helper()
	rec := e.as(s, http.MethodGet, "/api/devices"+query, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/devices%s: %d %s", query, rec.Code, rec.Body)
	}
	var out devicesBody
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Devices
}

func TestDevicesAndScansNeedASession(t *testing.T) {
	e := newEnv(t)
	for _, target := range []string{"/api/devices", "/api/scans/1"} {
		if rec := e.do(http.MethodGet, target, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s without a session: %d, want 401", target, rec.Code)
		}
	}
}

func TestListDevicesIsEmptyArrayWithoutData(t *testing.T) {
	e := newEnv(t)
	rec := e.as(e.mustLogin("admin"), http.MethodGet, "/api/devices", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "{\"devices\":[]}\n" {
		t.Errorf("empty list: %d %q", rec.Code, rec.Body)
	}
}

func TestListDevicesOrdersAndFilters(t *testing.T) {
	e := newEnv(t)
	seedScan(t, e,
		device("aa:aa:aa:aa:aa:10", "192.168.1.10", "nas", model.KindUnknown),
		device("aa:aa:aa:aa:aa:02", "192.168.1.2", "Gateway", model.KindRouter),
		device("aa:aa:aa:aa:aa:30", "192.168.1.30", "printer", model.KindPrinter),
	)
	// The second scan sees only the gateway: the other two go offline.
	seedScan(t, e, device("aa:aa:aa:aa:aa:02", "192.168.1.2", "Gateway", model.KindRouter))
	e.addUser("vera", model.RoleViewer)
	s := e.mustLogin("vera")

	ips := func(devs []model.Device) []string {
		var out []string
		for _, d := range devs {
			out = append(out, d.IP)
		}
		return out
	}
	eq := func(got, want []string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	for _, tc := range []struct {
		name, query string
		want        []string
	}{
		{"all, numeric IP order", "", []string{"192.168.1.2", "192.168.1.10", "192.168.1.30"}},
		{"online only", "?online=true", []string{"192.168.1.2"}},
		{"offline only", "?online=false", []string{"192.168.1.10", "192.168.1.30"}},
		{"by kind", "?kind=printer", []string{"192.168.1.30"}},
		{"text search ignores case", "?q=" + url.QueryEscape("GATEWAY"), []string{"192.168.1.2"}},
		{"filters combine", "?online=false&q=nas", []string{"192.168.1.10"}},
		{"no match", "?q=nothing-like-this", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ips(listDevices(t, e, s, tc.query)); !eq(got, tc.want) {
				t.Errorf("devices%s = %v, want %v", tc.query, got, tc.want)
			}
		})
	}
}

func TestListDevicesRejectsBadFilters(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")
	long := make([]byte, maxQueryLength+1)
	for i := range long {
		long[i] = 'a'
	}
	for name, query := range map[string]string{
		"online is not a bool": "?online=maybe",
		"unknown kind":         "?kind=toaster",
		"query too long":       "?q=" + string(long),
	} {
		t.Run(name, func(t *testing.T) {
			if rec := e.as(s, http.MethodGet, "/api/devices"+query, nil); rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (%s)", rec.Code, rec.Body)
			}
		})
	}
}

func TestRepoRoutesAreAbsentWithoutARepo(t *testing.T) {
	e := newEnv(t)
	s := newServer(Options{Auth: e.svc, Audit: e.store})
	for _, rt := range s.routes() {
		if rt.Path == "/api/devices" || rt.Path == "/api/scans/{id}" {
			t.Errorf("route %s registered without a repo", rt.Path)
		}
	}
}

func TestGetDevice(t *testing.T) {
	e := newEnv(t)
	seedScan(t, e, device("aa:aa:aa:aa:aa:02", "192.168.1.2", "gw", model.KindRouter))
	s := e.mustLogin("admin")

	list := listDevices(t, e, s, "")
	if len(list) != 1 {
		t.Fatalf("seeded %d devices, want 1", len(list))
	}
	rec := e.as(s, http.MethodGet, "/api/devices/"+strconv.FormatInt(list[0].ID, 10), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var body struct {
		Device model.Device `json:"device"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Device.ID != list[0].ID || body.Device.Hostname != "gw" {
		t.Errorf("device = %+v", body.Device)
	}

	for name, id := range map[string]string{
		"unknown":      "9999",
		"not a number": "abc",
		"zero":         "0",
		"negative":     "-1",
		"overflow":     "99999999999999999999",
	} {
		t.Run(name, func(t *testing.T) {
			if rec := e.as(s, http.MethodGet, "/api/devices/"+id, nil); rec.Code != http.StatusNotFound {
				t.Errorf("GET /api/devices/%s = %d, want 404", id, rec.Code)
			}
		})
	}
}

func TestSearchFindsByIPMACAndCyrillicNameIgnoringCase(t *testing.T) {
	e := newEnv(t)
	seedScan(t, e,
		device("aa:bb:cc:00:11:22", "192.168.1.40", "Принтер-бухгалтерия", model.KindPrinter),
		device("aa:bb:cc:00:11:33", "192.168.1.41", "nas", model.KindNAS),
	)
	s := e.mustLogin("admin")
	for name, tc := range map[string]struct {
		query string
		want  string
	}{
		"by IP":                  {"192.168.1.40", "192.168.1.40"},
		"by part of an IP":       {"1.41", "192.168.1.41"},
		"by MAC":                 {"aa:bb:cc:00:11:22", "192.168.1.40"},
		"by MAC in upper case":   {"AA:BB:CC:00:11:33", "192.168.1.41"},
		"by Cyrillic name":       {"принтер", "192.168.1.40"},
		"by Cyrillic upper case": {"БУХГАЛТЕРИЯ", "192.168.1.40"},
	} {
		t.Run(name, func(t *testing.T) {
			got := listDevices(t, e, s, "?q="+url.QueryEscape(tc.query))
			if len(got) != 1 || got[0].IP != tc.want {
				t.Errorf("search %q found %d devices %v, want only %s", tc.query, len(got), got, tc.want)
			}
		})
	}
}

func TestGetDeviceListsInterfaces(t *testing.T) {
	e := newEnv(t)
	in := device("aa:aa:aa:aa:aa:02", "192.168.1.2", "sw1", model.KindSwitch)
	ctx := context.Background()
	scan, err := e.store.StartScan(ctx, "192.168.1.0/24")
	if err != nil {
		t.Fatal(err)
	}
	err = e.store.ApplyResult(ctx, scan.ID, collector.Result{
		Devices: []model.DeviceInput{in, device("aa:aa:aa:aa:aa:03", "192.168.1.3", "quiet", model.KindHost)},
		Interfaces: []model.InterfaceInput{
			{Device: in.Key, Name: "Gi2", Up: false},
			{Device: in.Key, Name: "Gi1", MAC: "02:11:22:33:44:01", IP: "192.168.1.2", Up: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.FinishScan(ctx, scan.ID, "done", ""); err != nil {
		t.Fatal(err)
	}
	s := e.mustLogin("admin")

	byIP := map[string]int64{}
	for _, d := range listDevices(t, e, s, "") {
		byIP[d.IP] = d.ID
	}
	get := func(ip string) []model.Interface {
		rec := e.as(s, http.MethodGet, "/api/devices/"+strconv.FormatInt(byIP[ip], 10), nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
		}
		var body struct {
			Interfaces []model.Interface `json:"interfaces"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(rec.Body.String(), `"interfaces":[`) {
			t.Errorf("interfaces must be a list, never null: %s", rec.Body)
		}
		return body.Interfaces
	}

	got := get("192.168.1.2")
	if len(got) != 2 || got[0].Name != "Gi1" || !got[0].Up || got[0].MAC != "02:11:22:33:44:01" || got[1].Name != "Gi2" || got[1].Up {
		t.Errorf("interfaces = %+v", got)
	}
	if got := get("192.168.1.3"); len(got) != 0 {
		t.Errorf("a device without interfaces got %+v", got)
	}
}
