package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/collector"
	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/store"
)

type fakeIdentifier struct {
	err error
	ids []int64
}

func (f *fakeIdentifier) Identify(id int64) error {
	f.ids = append(f.ids, id)
	return f.err
}

func identifyEnv(t *testing.T, id *fakeIdentifier) (*env, session) {
	t.Helper()
	e := newEnv(t, func(o *envOptions) { o.ident = id })
	e.addUser("olga", model.RoleOperator)
	return e, e.mustLogin("olga")
}

func TestIdentifyDeviceAnswersAcceptedAndWritesAnAuditEntry(t *testing.T) {
	id := &fakeIdentifier{}
	e, s := identifyEnv(t, id)

	rec := e.as(s, http.MethodPost, "/api/devices/5/identify", nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if len(id.ids) != 1 || id.ids[0] != 5 {
		t.Errorf("identified = %v, want device 5", id.ids)
	}
	entries, err := e.store.ListAudit(context.Background(), store.AuditFilter{Action: "device_identify", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !strings.Contains(entries[0].Detail, "5") {
		t.Errorf("audit = %+v, want one device_identify entry", entries)
	}
}

func TestIdentifyDeviceMapsEachFailureToAStatus(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want int
	}{
		{model.ErrNotFound, http.StatusNotFound},
		{collector.ErrBusy, http.StatusConflict},
		{collector.ErrNmapUnavailable, http.StatusBadRequest},
		{collector.ErrNoAddress, http.StatusBadRequest},
		{errors.New("disk on fire"), http.StatusInternalServerError},
	} {
		e, s := identifyEnv(t, &fakeIdentifier{err: tc.err})
		rec := e.as(s, http.MethodPost, "/api/devices/5/identify", nil)
		if rec.Code != tc.want {
			t.Errorf("%v: status = %d, want %d", tc.err, rec.Code, tc.want)
		}
		if tc.want == http.StatusInternalServerError && strings.Contains(rec.Body.String(), "disk on fire") {
			t.Errorf("the internal error text reached the client: %s", rec.Body)
		}
		entries, _ := e.store.ListAudit(context.Background(), store.AuditFilter{Action: "device_identify", Limit: 10})
		if len(entries) != 0 {
			t.Errorf("%v: a refused request was written to the audit log", tc.err)
		}
	}
}

func TestIdentifyDeviceRefusesABadID(t *testing.T) {
	id := &fakeIdentifier{}
	e, s := identifyEnv(t, id)
	for _, path := range []string{"/api/devices/abc/identify", "/api/devices/0/identify", "/api/devices/-3/identify"} {
		if rec := e.as(s, http.MethodPost, path, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, rec.Code)
		}
	}
	if len(id.ids) != 0 {
		t.Errorf("identified = %v, want nothing", id.ids)
	}
}

func TestIdentifyDeviceNeedsTheRightToRunScans(t *testing.T) {
	id := &fakeIdentifier{}
	e, _ := identifyEnv(t, id)
	e.addUser("vera", model.RoleViewer)
	if rec := e.as(e.mustLogin("vera"), http.MethodPost, "/api/devices/5/identify", nil); rec.Code != http.StatusForbidden {
		t.Errorf("viewer: status = %d, want 403", rec.Code)
	}
	if rec := e.do(http.MethodPost, "/api/devices/5/identify", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("no session: status = %d, want 401", rec.Code)
	}
	if len(id.ids) != 0 {
		t.Errorf("identified = %v, want nothing", id.ids)
	}
}

func TestIdentifyRouteIsAbsentWithoutAnIdentifier(t *testing.T) {
	e := newEnv(t, func(o *envOptions) {})
	if rec := e.as(e.mustLogin("admin"), http.MethodPost, "/api/devices/5/identify", nil); rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 404 or 405", rec.Code)
	}
}
