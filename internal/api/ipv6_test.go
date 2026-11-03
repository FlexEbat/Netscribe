package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/model"
)

func TestManualDeviceTakesAnIPv6AddressInCanonicalForm(t *testing.T) {
	e := newEnv(t)
	e.addUser("olga", model.RoleOperator)
	s := e.mustLogin("olga")

	d := createManual(t, e, s, map[string]string{"label": "nas", "ip": "192.168.1.9", "ip6": "FD00:0:0:0:0:0:0:9"})
	if d.IP != "192.168.1.9" || d.IP6 != "fd00::9" {
		t.Errorf("device = %+v, want both addresses with the IPv6 one in canonical form", d)
	}
	if got := listDevices(t, e, s, "fd00::9"); len(got) != 1 || got[0].ID != d.ID {
		t.Errorf("a search for the IPv6 address found %+v, want the device", got)
	}
}

func TestAnIPv6AddressTypedIntoTheIPFieldGoesToTheIPv6Column(t *testing.T) {
	e := newEnv(t)
	e.addUser("olga", model.RoleOperator)
	s := e.mustLogin("olga")

	d := createManual(t, e, s, map[string]string{"label": "printer", "ip": "fd00::20"})
	if d.IP != "" || d.IP6 != "fd00::20" {
		t.Errorf("device = %+v, want the address in ip6 and ip empty", d)
	}
}

func TestManualDeviceRefusesABadIPv6Address(t *testing.T) {
	e := newEnv(t)
	e.addUser("olga", model.RoleOperator)
	s := e.mustLogin("olga")

	for _, bad := range []string{"192.168.1.9", "not-an-address", "fe80::1%eth0", "::ffff:192.168.1.9"} {
		rec := e.as(s, http.MethodPost, "/api/devices", map[string]string{"label": "x", "ip6": bad})
		if rec.Code != http.StatusBadRequest || errorText(t, rec) != "ip6 is not a valid IPv6 address" {
			t.Errorf("ip6 %q: %d %s, want 400 with the ip6 message", bad, rec.Code, rec.Body)
		}
	}
	if got := listDevices(t, e, s, ""); len(got) != 0 {
		t.Errorf("%d devices were created from bad input, want none", len(got))
	}
}

func TestTwoDevicesCannotShareAnIPv6Address(t *testing.T) {
	e := newEnv(t)
	e.addUser("olga", model.RoleOperator)
	s := e.mustLogin("olga")

	createManual(t, e, s, map[string]string{"label": "a", "ip6": "fd00::5"})
	rec := e.as(s, http.MethodPost, "/api/devices", map[string]string{"label": "b", "ip6": "fd00:0::5"})
	if rec.Code != http.StatusConflict {
		t.Errorf("second device with the same address: %d %s, want 409", rec.Code, rec.Body)
	}
}

func TestImportReadsAnIPv6Column(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")

	code, out := postCSV(e, s, "/api/devices/import", "label,ip,ip6\ncam1,192.168.9.5,FD00::1\ncam2,,fd00::2\n")
	if code != http.StatusCreated || string(out["created"]) != "2" {
		t.Fatalf("%d %v", code, out)
	}
	byLabel := map[string]model.Device{}
	for _, d := range listDevices(t, e, s, "") {
		byLabel[d.Label] = d
	}
	if d := byLabel["cam1"]; d.IP != "192.168.9.5" || d.IP6 != "fd00::1" {
		t.Errorf("cam1 = %+v", d)
	}
	if d := byLabel["cam2"]; d.IP != "" || d.IP6 != "fd00::2" {
		t.Errorf("cam2 = %+v", d)
	}
}

func TestImportCatchesTheSameIPv6AddressTwice(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")

	code, out := postCSV(e, s, "/api/devices/import", "label,ip6\na,fd00::1\nb,fd00:0:0::1\n")
	if code != http.StatusBadRequest {
		t.Fatalf("%d %v, want 400", code, out)
	}
	var rows []importProblem
	_ = json.Unmarshal(out["rows"], &rows)
	if len(rows) != 1 || rows[0].Line != 3 || !strings.Contains(rows[0].Error, "the same ip6 as line 2") {
		t.Errorf("rows = %+v, want line 3 named as the same ip6 as line 2", rows)
	}
	if got := listDevices(t, e, s, ""); len(got) != 0 {
		t.Errorf("%d devices were created from a file with a duplicate, want none", len(got))
	}
}
