package api

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/model"
)

func TestOperatorTrustsADeviceAndTheListFiltersByIt(t *testing.T) {
	e := newEnv(t)
	ids := seedServices(t, e)
	s := e.mustLogin("admin")
	gw := ids["192.168.1.1"]

	untrusted := listDevices(t, e, s, "?trusted=false")
	if len(untrusted) != 2 {
		t.Fatalf("untrusted after a scan = %d, want 2", len(untrusted))
	}
	var version int64
	for _, d := range untrusted {
		if d.ID == gw {
			version = d.Version
		}
	}
	rec := e.as(s, http.MethodPatch, "/api/devices/"+strconv.FormatInt(gw, 10), map[string]any{"trusted": true, "version": version})
	if rec.Code != http.StatusOK || !decodeDevice(t, rec).Trusted {
		t.Fatalf("PATCH trusted: %d %s", rec.Code, rec.Body)
	}
	if got := listDevices(t, e, s, "?trusted=true"); len(got) != 1 || got[0].ID != gw {
		t.Errorf("trusted list = %+v, want only the gateway", got)
	}
	if got := listDevices(t, e, s, "?trusted=false"); len(got) != 1 || got[0].ID == gw {
		t.Errorf("untrusted list = %+v, want only the other device", got)
	}
}

func TestTrustedFilterRejectsOtherValues(t *testing.T) {
	e := newEnv(t)
	rec := e.as(e.mustLogin("admin"), http.MethodGet, "/api/devices?trusted=maybe", nil)
	if rec.Code != http.StatusBadRequest || errorText(t, rec) != "trusted must be true or false" {
		t.Errorf("GET: %d %s", rec.Code, rec.Body)
	}
}

func TestTrustWithAStaleVersionIsRefusedAndChangesNothing(t *testing.T) {
	e := newEnv(t)
	ids := seedServices(t, e)
	s := e.mustLogin("admin")
	id := strconv.FormatInt(ids["192.168.1.1"], 10)

	rec := e.as(s, http.MethodPatch, "/api/devices/"+id, map[string]any{"trusted": true, "version": 999})
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale PATCH: %d %s, want 409", rec.Code, rec.Body)
	}
	if got := listDevices(t, e, s, "?trusted=true"); len(got) != 0 {
		t.Errorf("trusted after a refused edit = %+v", got)
	}
}

func TestViewerCannotTrustADevice(t *testing.T) {
	e := newEnv(t)
	ids := seedServices(t, e)
	e.addUser("vera", model.RoleViewer)
	s := e.mustLogin("vera")
	id := strconv.FormatInt(ids["192.168.1.1"], 10)

	if rec := e.as(s, http.MethodPatch, "/api/devices/"+id, map[string]any{"trusted": true, "version": 1}); rec.Code != http.StatusForbidden {
		t.Errorf("viewer PATCH = %d, want 403", rec.Code)
	}
	if got := listDevices(t, e, s, "?trusted=true"); len(got) != 0 {
		t.Errorf("trusted after a viewer request = %+v", got)
	}
}

func TestServicesCarryTheirRiskAndCanBeFilteredByIt(t *testing.T) {
	e := newEnv(t)
	seedServices(t, e)

	code, all, raw := getServices(t, e, "")
	if code != http.StatusOK || len(all) != 3 {
		t.Fatalf("all services: %d %d %s", code, len(all), raw)
	}
	for _, sv := range all {
		if (sv.Port == 445) != (sv.Risk != nil) {
			t.Errorf("service %d/%s has risk %+v", sv.Port, sv.Proto, sv.Risk)
		}
	}
	code, risky, _ := getServices(t, e, "?risky=true")
	if code != http.StatusOK || len(risky) != 1 || risky[0].Port != 445 || risky[0].Risk == nil || risky[0].Risk.Severity != model.RiskMedium {
		t.Errorf("risky services = %+v", risky)
	}
	if code, _, _ := getServices(t, e, "?risky=maybe"); code != http.StatusBadRequest {
		t.Errorf("risky=maybe = %d, want 400", code)
	}
}
