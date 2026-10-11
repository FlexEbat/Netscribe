package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/events"
	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/store"
)

type deviceBodyOut struct {
	Device model.Device `json:"device"`
}

func decodeDevice(t *testing.T, rec *httptest.ResponseRecorder) model.Device {
	t.Helper()
	var out deviceBodyOut
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body %q: %v", rec.Body, err)
	}
	return out.Device
}

func errorText(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var out struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body %q: %v", rec.Body, err)
	}
	return out.Error
}

func auditActions(t *testing.T, e *env, action string) []model.AuditEntry {
	t.Helper()
	entries, err := e.store.ListAudit(context.Background(), store.AuditFilter{Action: action, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func createManual(t *testing.T, e *env, s session, body any) model.Device {
	t.Helper()
	rec := e.as(s, http.MethodPost, "/api/devices", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/devices: %d %s", rec.Code, rec.Body)
	}
	return decodeDevice(t, rec)
}

func TestOperatorCreatesAManualDeviceWithoutIPAndMAC(t *testing.T) {
	e := newEnv(t)
	e.addUser("olga", model.RoleOperator)
	s := e.mustLogin("olga")

	d := createManual(t, e, s, map[string]string{"label": "Patch panel A", "kind": "switch", "notes": "rack 2"})
	if !d.Manual || d.Label != "Patch panel A" || d.Kind != model.KindSwitch || !d.KindLocked || d.IP != "" || d.MAC != "" {
		t.Errorf("device = %+v", d)
	}
	if got := listDevices(t, e, s, ""); len(got) != 1 || got[0].ID != d.ID {
		t.Errorf("list = %+v", got)
	}
	topo := getTopology(t, e, s)
	if len(topo.Devices) != 1 || topo.Devices[0].X == nil {
		t.Errorf("topology = %+v, want the device with a position", topo.Devices)
	}
	entries := auditActions(t, e, "device_create")
	if len(entries) != 1 || entries[0].Username != "olga" || entries[0].Result != "ok" || !strings.Contains(entries[0].Detail, "Patch panel A") {
		t.Errorf("audit = %+v", entries)
	}
}

func TestManualDeviceInputIsNormalised(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")
	d := createManual(t, e, s, map[string]string{"mac": "AA-BB-CC-DD-EE-FF", "ip": " 192.168.1.9 "})
	if d.MAC != "aa:bb:cc:dd:ee:ff" || d.IP != "192.168.1.9" || d.Kind != model.KindUnknown || d.KindLocked {
		t.Errorf("device = %+v", d)
	}
}

func TestManualDeviceValidation(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")
	for name, tc := range map[string]struct {
		body string
		want string
	}{
		"bad ip":         {`{"ip":"999.1.1.1","label":"x"}`, "ip is not valid"},
		"text as ip":     {`{"ip":"router","label":"x"}`, "ip is not valid"},
		"bad mac":        {`{"mac":"zz:zz","label":"x"}`, "mac is not valid"},
		"short mac":      {`{"mac":"aa:bb:cc","label":"x"}`, "mac is not valid"},
		"nothing at all": {`{}`, "device has no ip, mac or label"},
		"spaces only":    {`{"label":"   "}`, "device has no ip, mac or label"},
		"long label":     {`{"label":"` + strings.Repeat("a", 101) + `"}`, "label is longer than 100 characters"},
		"long notes":     {`{"label":"x","notes":"` + strings.Repeat("a", 2001) + `"}`, "notes are longer than 2000 characters"},
		"unknown kind":   {`{"label":"x","kind":"toaster"}`, "kind is unknown"},
		"unknown field":  {`{"label":"x","admin":true}`, "invalid request body"},
		"not json":       {`label`, "invalid request body"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := e.as(s, http.MethodPost, "/api/devices", tc.body)
			if rec.Code != http.StatusBadRequest || errorText(t, rec) != tc.want {
				t.Errorf("status %d, error %q, want 400 %q", rec.Code, errorText(t, rec), tc.want)
			}
		})
	}
	if got := listDevices(t, e, s, ""); len(got) != 0 {
		t.Errorf("rejected input created %d devices", len(got))
	}
}

func TestLabelOfExactlyTheLimitAndCyrillicCountedAsCharacters(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")
	createManual(t, e, s, map[string]string{"label": strings.Repeat("я", 100)}) // 200 bytes, 100 characters
	if rec := e.as(s, http.MethodPost, "/api/devices", map[string]string{"label": strings.Repeat("я", 101)}); rec.Code != http.StatusBadRequest {
		t.Errorf("101 characters: %d, want 400", rec.Code)
	}
}

func TestCreatingADuplicateIsAConflict(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")
	createManual(t, e, s, map[string]string{"mac": "aa:bb:cc:dd:ee:ff"})
	if rec := e.as(s, http.MethodPost, "/api/devices", map[string]string{"mac": "AA:BB:CC:DD:EE:FF"}); rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
}

func TestEditChangesLabelNotesAndKindAndBumpsTheVersion(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")
	d := createManual(t, e, s, map[string]string{"label": "old"})

	rec := e.as(s, http.MethodPatch, "/api/devices/"+strconv.FormatInt(d.ID, 10),
		map[string]any{"label": "New name", "notes": "line1\nline2", "kind": "router", "version": d.Version})
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH: %d %s", rec.Code, rec.Body)
	}
	got := decodeDevice(t, rec)
	if got.Label != "New name" || got.Notes != "line1\nline2" || got.Kind != model.KindRouter || !got.KindLocked || got.Version != d.Version+1 {
		t.Errorf("device = %+v", got)
	}
	if entries := auditActions(t, e, "device_edit"); len(entries) != 1 || entries[0].Result != "ok" {
		t.Errorf("audit = %+v", entries)
	}

	// Fields that are not sent stay as they are.
	rec = e.as(s, http.MethodPatch, "/api/devices/"+strconv.FormatInt(d.ID, 10), map[string]any{"notes": "only notes", "version": got.Version})
	after := decodeDevice(t, rec)
	if after.Label != "New name" || after.Kind != model.KindRouter || after.Notes != "only notes" {
		t.Errorf("device = %+v", after)
	}
}

func TestEditWithAStaleVersionIsRefusedAndKeepsTheOtherEdit(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")
	d := createManual(t, e, s, map[string]string{"label": "first"})
	path := "/api/devices/" + strconv.FormatInt(d.ID, 10)

	if rec := e.as(s, http.MethodPatch, path, map[string]any{"label": "theirs", "version": d.Version}); rec.Code != http.StatusOK {
		t.Fatalf("first edit: %d", rec.Code)
	}
	rec := e.as(s, http.MethodPatch, path, map[string]any{"label": "mine", "version": d.Version})
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale edit: %d %s, want 409", rec.Code, rec.Body)
	}
	if got := listDevices(t, e, s, ""); got[0].Label != "theirs" {
		t.Errorf("label = %q, the other edit must stay", got[0].Label)
	}
}

func TestEditValidation(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")
	d := createManual(t, e, s, map[string]string{"label": "x"})
	path := "/api/devices/" + strconv.FormatInt(d.ID, 10)
	for name, tc := range map[string]struct {
		body string
		want string
	}{
		"no version":    {`{"label":"y"}`, "version is required"},
		"zero version":  {`{"label":"y","version":0}`, "version is required"},
		"long label":    {`{"label":"` + strings.Repeat("a", 101) + `","version":1}`, "label is longer than 100 characters"},
		"long notes":    {`{"notes":"` + strings.Repeat("a", 2001) + `","version":1}`, "notes are longer than 2000 characters"},
		"unknown kind":  {`{"kind":"toaster","version":1}`, "kind is unknown"},
		"empty kind":    {`{"kind":"","version":1}`, "kind is unknown"},
		"locked fields": {`{"manual":false,"version":1}`, "invalid request body"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := e.as(s, http.MethodPatch, path, tc.body)
			if rec.Code != http.StatusBadRequest || errorText(t, rec) != tc.want {
				t.Errorf("status %d, error %q, want 400 %q", rec.Code, errorText(t, rec), tc.want)
			}
		})
	}
	for _, id := range []string{"999", "abc", "0"} {
		if rec := e.as(s, http.MethodPatch, "/api/devices/"+id, map[string]any{"label": "y", "version": 1}); rec.Code != http.StatusNotFound {
			t.Errorf("PATCH /api/devices/%s = %d, want 404", id, rec.Code)
		}
	}
}

func TestDeleteRemovesTheDeviceAndItsLinksAndIsAudited(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")
	a := createManual(t, e, s, map[string]string{"label": "a"})
	b := createManual(t, e, s, map[string]string{"label": "b"})
	if rec := e.as(s, http.MethodPost, "/api/links", map[string]any{"aDeviceId": a.ID, "bDeviceId": b.ID}); rec.Code != http.StatusCreated {
		t.Fatalf("link: %d %s", rec.Code, rec.Body)
	}

	if rec := e.as(s, http.MethodDelete, "/api/devices/"+strconv.FormatInt(a.ID, 10), nil); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE: %d %s", rec.Code, rec.Body)
	}
	if topo := getTopology(t, e, s); len(topo.Devices) != 1 || len(topo.Links) != 0 {
		t.Errorf("topology = %d devices, %d links, want 1 and 0", len(topo.Devices), len(topo.Links))
	}
	if entries := auditActions(t, e, "device_delete"); len(entries) != 1 || !strings.Contains(entries[0].Detail, "a") {
		t.Errorf("audit = %+v", entries)
	}
	for _, id := range []string{strconv.FormatInt(a.ID, 10), "abc", "0"} {
		if rec := e.as(s, http.MethodDelete, "/api/devices/"+id, nil); rec.Code != http.StatusNotFound {
			t.Errorf("DELETE /api/devices/%s = %d, want 404", id, rec.Code)
		}
	}
}

func TestLinksCreateValidateAndDelete(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")
	a := createManual(t, e, s, map[string]string{"label": "a"})
	b := createManual(t, e, s, map[string]string{"label": "b"})

	rec := e.as(s, http.MethodPost, "/api/links", map[string]any{"aDeviceId": b.ID, "aPort": "eth0", "bDeviceId": a.ID, "bPort": "Gi0/1"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/links: %d %s", rec.Code, rec.Body)
	}
	var out struct {
		Link model.Link `json:"link"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Link.ADeviceID != a.ID || out.Link.Source != "manual" {
		t.Errorf("link = %+v", out.Link)
	}
	if topo := getTopology(t, e, s); len(topo.Links) != 1 {
		t.Errorf("topology links = %d, want 1", len(topo.Links))
	}
	if entries := auditActions(t, e, "link_create"); len(entries) != 1 {
		t.Errorf("link_create audit = %+v", entries)
	}

	for name, tc := range map[string]struct {
		body any
		code int
		msg  string
	}{
		"itself":           {map[string]any{"aDeviceId": a.ID, "bDeviceId": a.ID}, http.StatusBadRequest, "link connects a device to itself"},
		"long port":        {map[string]any{"aDeviceId": a.ID, "bDeviceId": b.ID, "aPort": strings.Repeat("p", 65)}, http.StatusBadRequest, "port is longer than 64 characters"},
		"missing device":   {map[string]any{"aDeviceId": a.ID, "bDeviceId": 9999}, http.StatusNotFound, "device not found"},
		"the same link":    {map[string]any{"aDeviceId": a.ID, "aPort": "Gi0/1", "bDeviceId": b.ID, "bPort": "eth0"}, http.StatusConflict, "this link already exists"},
		"not an object":    {`[]`, http.StatusBadRequest, "invalid request body"},
		"unknown property": {map[string]any{"aDeviceId": a.ID, "bDeviceId": b.ID, "source": "lldp"}, http.StatusBadRequest, "invalid request body"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := e.as(s, http.MethodPost, "/api/links", tc.body)
			if rec.Code != tc.code || errorText(t, rec) != tc.msg {
				t.Errorf("status %d, error %q, want %d %q", rec.Code, errorText(t, rec), tc.code, tc.msg)
			}
		})
	}

	path := "/api/links/" + strconv.FormatInt(out.Link.ID, 10)
	if rec := e.as(s, http.MethodDelete, path, nil); rec.Code != http.StatusNoContent {
		t.Errorf("DELETE link: %d %s", rec.Code, rec.Body)
	}
	if rec := e.as(s, http.MethodDelete, path, nil); rec.Code != http.StatusNotFound {
		t.Errorf("DELETE link again: %d, want 404", rec.Code)
	}
	if rec := e.as(s, http.MethodDelete, "/api/links/abc", nil); rec.Code != http.StatusNotFound {
		t.Errorf("DELETE /api/links/abc: %d, want 404", rec.Code)
	}
	if entries := auditActions(t, e, "link_delete"); len(entries) != 1 {
		t.Errorf("link_delete audit = %+v", entries)
	}
}

// foundLinkRepo behaves as if every link had been found by a scan.
type foundLinkRepo struct{ store.Repo }

func (foundLinkRepo) DeleteLink(context.Context, int64) error { return model.ErrNotManual }

func TestDeletingALinkFoundByAScanIsAConflict(t *testing.T) {
	e := newEnv(t, func(o *envOptions) { o.wrapRepo = func(r store.Repo) store.Repo { return foundLinkRepo{r} } })
	rec := e.as(e.mustLogin("admin"), http.MethodDelete, "/api/links/1", nil)
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
}

func TestViewerCannotEditAnything(t *testing.T) {
	e := newEnv(t)
	d := createManual(t, e, e.mustLogin("admin"), map[string]string{"label": "x"})
	e.addUser("vera", model.RoleViewer)
	s := e.mustLogin("vera")
	id := strconv.FormatInt(d.ID, 10)

	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/api/devices", map[string]string{"label": "y"}},
		{http.MethodPatch, "/api/devices/" + id, map[string]any{"label": "y", "version": d.Version}},
		{http.MethodDelete, "/api/devices/" + id, nil},
		{http.MethodPost, "/api/links", map[string]any{"aDeviceId": 1, "bDeviceId": 2}},
		{http.MethodDelete, "/api/links/1", nil},
	} {
		if rec := e.as(s, tc.method, tc.path, tc.body); rec.Code != http.StatusForbidden {
			t.Errorf("viewer %s %s = %d, want 403", tc.method, tc.path, rec.Code)
		}
	}
	if got := listDevices(t, e, s, ""); len(got) != 1 || got[0].Label != "x" {
		t.Errorf("devices = %+v, the viewer's requests must change nothing", got)
	}
	if rec := e.as(s, http.MethodGet, "/api/devices", nil); rec.Code != http.StatusOK {
		t.Errorf("viewer GET /api/devices = %d, want 200", rec.Code)
	}
}

func TestChangesAreAnnouncedToTheEventStreams(t *testing.T) {
	hub := events.NewHub()
	e := newEnv(t, func(o *envOptions) { o.hub = hub })
	ch, cancel := hub.Subscribe()
	defer cancel()
	createManual(t, e, e.mustLogin("admin"), map[string]string{"label": "x"})
	select {
	case ev := <-ch:
		if ev.Name != "topology.changed" {
			t.Errorf("event = %q, want topology.changed", ev.Name)
		}
	default:
		t.Error("no event after creating a device")
	}
}
