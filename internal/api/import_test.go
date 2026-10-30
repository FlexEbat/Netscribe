package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func postCSV(e *env, s session, target, body string) (int, map[string]json.RawMessage) {
	e.t.Helper()
	rec := e.do(http.MethodPost, target, body, func(r *http.Request) {
		r.AddCookie(s.cookie)
		r.Header.Set(csrfHeader, s.csrf)
		r.Header.Set("Content-Type", "text/csv; charset=utf-8")
	})
	var out map[string]json.RawMessage
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestImportCreatesTrustedManualDevicesInTheirGroup(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")
	makeGroup(t, e, s, map[string]any{"name": "Office"})
	code, out := postCSV(e, s, "/api/devices/import", "label,ip,mac,kind,group,notes\ncam1,192.168.9.5,,camera,office,front door\nswitch,,aa:bb:cc:dd:ee:09,switch,,\n=cmd|' /C calc'!A0,192.168.9.7,,,,@SUM(1)\n")
	if code != http.StatusCreated || string(out["created"]) != "3" {
		t.Fatalf("%d %v", code, out)
	}
	devices := listDevices(t, e, s, "")
	if len(devices) != 3 {
		t.Fatalf("devices = %d", len(devices))
	}
	for _, d := range devices {
		if !d.Manual || !d.Trusted {
			t.Errorf("%+v is not a trusted manual device", d)
		}
		switch d.IP {
		case "192.168.9.5":
			if d.Group == nil || d.Group.Name != "Office" || d.Notes != "front door" || d.Kind != "camera" {
				t.Errorf("cam1 = %+v", d)
			}
		case "192.168.9.7":
			if d.Label != "=cmd|' /C calc'!A0" || d.Notes != "@SUM(1)" {
				t.Errorf("a formula cell was changed: %q %q", d.Label, d.Notes)
			}
		}
	}
	if got := auditActions(t, e, "device_import"); len(got) != 1 || got[0].Detail != "3 devices" {
		t.Errorf("audit = %+v", got)
	}
}

func TestImportWithOneBadRowCreatesNothingAndNamesTheLine(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")
	code, out := postCSV(e, s, "/api/devices/import", "label,ip,group\ngood,192.168.9.5,\nbad,not-an-ip,\nworse,192.168.9.6,Nowhere\n")
	if code != http.StatusBadRequest || string(out["error"]) != `"2 rows have problems"` {
		t.Fatalf("%d %v", code, out)
	}
	var rows []importProblem
	if err := json.Unmarshal(out["rows"], &rows); err != nil || len(rows) != 2 || rows[0].Line != 3 || rows[1].Line != 4 {
		t.Errorf("rows = %+v, %v, want lines 3 and 4", rows, err)
	}
	if !strings.Contains(rows[1].Error, `group "Nowhere" does not exist`) {
		t.Errorf("row 4 error = %q", rows[1].Error)
	}
	if got := listDevices(t, e, s, ""); len(got) != 0 {
		t.Errorf("%d devices were created by a bad file", len(got))
	}
}

func TestImportCatchesADuplicateInsideTheFileAndOneThatExists(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")
	code, out := postCSV(e, s, "/api/devices/import", "label,mac\na,aa:bb:cc:dd:ee:01\nb,AA:BB:CC:DD:EE:01\n")
	if code != http.StatusBadRequest {
		t.Fatalf("duplicate in the file: %d %v", code, out)
	}
	var rows []importProblem
	_ = json.Unmarshal(out["rows"], &rows)
	if len(rows) != 1 || rows[0].Line != 3 || !strings.Contains(rows[0].Error, "line 2") {
		t.Errorf("rows = %+v", rows)
	}

	createManual(t, e, s, map[string]any{"label": "old", "ip": "192.168.9.5"})
	code, out = postCSV(e, s, "/api/devices/import", "label,ip\nnew,192.168.9.6\nclash,192.168.9.5\n")
	if code != http.StatusConflict {
		t.Fatalf("collision with an existing device: %d %v", code, out)
	}
	_ = json.Unmarshal(out["rows"], &rows)
	if len(rows) != 1 || rows[0].Line != 3 {
		t.Errorf("rows = %+v, want line 3", rows)
	}
	if got := listDevices(t, e, s, ""); len(got) != 1 {
		t.Errorf("%d devices, want only the old one (all or none)", len(got))
	}
}

func TestImportDryRunChangesNothing(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")
	code, out := postCSV(e, s, "/api/devices/import?dryRun=true", "label,ip\na,192.168.9.5\nb,192.168.9.6\n")
	if code != http.StatusOK || string(out["wouldCreate"]) != "2" {
		t.Fatalf("%d %v", code, out)
	}
	if got := listDevices(t, e, s, ""); len(got) != 0 {
		t.Errorf("a dry run created %d devices", len(got))
	}
	if got := auditActions(t, e, "device_import"); len(got) != 0 {
		t.Errorf("a dry run was audited: %+v", got)
	}
	// The same file for real works afterwards: the dry run left no trace in the database.
	if code, _ := postCSV(e, s, "/api/devices/import", "label,ip\na,192.168.9.5\nb,192.168.9.6\n"); code != http.StatusCreated {
		t.Errorf("real import after a dry run = %d", code)
	}
}

func TestImportRefusesWhatIsNotAGoodCSV(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")
	for name, tc := range map[string]struct {
		body string
		want int
	}{
		"no header":     {"", http.StatusBadRequest},
		"only a header": {"label,ip\n", http.StatusBadRequest},
		"bad column":    {"label,colour\nx,y\n", http.StatusBadRequest},
		"empty row":     {"label,ip\n,\n", http.StatusBadRequest},
		"1001 rows":     {"label\n" + strings.Repeat("x\n", 1001), http.StatusRequestEntityTooLarge},
		"over 1 MiB":    {"label\n" + strings.Repeat("y", 1<<20), http.StatusRequestEntityTooLarge},
	} {
		if code, out := postCSV(e, s, "/api/devices/import", tc.body); code != tc.want {
			t.Errorf("%s: %d %v, want %d", name, code, out, tc.want)
		}
	}
	rec := e.as(s, http.MethodPost, "/api/devices/import", `{"label":"x"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "text/csv") {
		t.Errorf("JSON body: %d %s", rec.Code, rec.Body)
	}
}
