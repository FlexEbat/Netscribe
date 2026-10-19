package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/collector"
	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/store"
)

// fakeNmapScanner is a scanner that can also run a scan with nmap on.
type fakeNmapScanner struct {
	fakeScanner
	nmapCalls int
}

func (f *fakeNmapScanner) StartWithNmap() (model.Scan, error) {
	f.nmapCalls++
	return model.Scan{ID: 43, Target: "192.168.1.0/24", Status: "running"}, nil
}

type fakeNmapStatus struct{ state collector.NmapState }

func (f fakeNmapStatus) NmapState() collector.NmapState { return f.state }

func nmapEnv(t *testing.T, state collector.NmapState) (*env, *fakeNmapScanner, session) {
	t.Helper()
	fs := &fakeNmapScanner{}
	e := newEnv(t, func(o *envOptions) {
		o.scanner = fs
		o.nmap = fakeNmapStatus{state: state}
	})
	e.addUser("olga", model.RoleOperator)
	return e, fs, e.mustLogin("olga")
}

func TestStartScanWithNmapUsesTheNmapStart(t *testing.T) {
	e, fs, s := nmapEnv(t, collector.NmapState{Available: true})

	rec := e.as(s, http.MethodPost, "/api/scans", map[string]bool{"nmap": true})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if fs.nmapCalls != 1 || fs.calls != 0 {
		t.Errorf("nmap starts = %d, plain starts = %d, want only the nmap one", fs.nmapCalls, fs.calls)
	}
	entries, err := e.store.ListAudit(context.Background(), store.AuditFilter{Action: "scan_start", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !strings.Contains(entries[0].Detail, "with nmap") {
		t.Errorf("audit = %+v, want the scan marked as run with nmap", entries)
	}
}

func TestStartScanWithoutTheNmapFlagStaysPlain(t *testing.T) {
	e, fs, s := nmapEnv(t, collector.NmapState{Available: true})
	for name, body := range map[string]any{"no body": nil, "false": map[string]bool{"nmap": false}, "empty object": map[string]bool{}} {
		if rec := e.as(s, http.MethodPost, "/api/scans", body); rec.Code != http.StatusAccepted {
			t.Fatalf("%s: status = %d, body %s", name, rec.Code, rec.Body)
		}
	}
	if fs.calls != 3 || fs.nmapCalls != 0 {
		t.Errorf("plain starts = %d, nmap starts = %d, want 3 and 0", fs.calls, fs.nmapCalls)
	}
}

func TestStartScanWithNmapIsRefusedWhenNmapIsNotThere(t *testing.T) {
	e, fs, s := nmapEnv(t, collector.NmapState{Available: false, Reason: "nmap was not found"})
	rec := e.as(s, http.MethodPost, "/api/scans", map[string]bool{"nmap": true})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if fs.calls+fs.nmapCalls != 0 {
		t.Error("a scan was started although nmap is missing")
	}

	// A scanner that cannot run with nmap is refused the same way.
	plain := &fakeScanner{}
	e = newEnv(t, func(o *envOptions) {
		o.scanner = plain
		o.nmap = fakeNmapStatus{state: collector.NmapState{Available: true}}
	})
	e.addUser("olga", model.RoleOperator)
	if rec := e.as(e.mustLogin("olga"), http.MethodPost, "/api/scans", map[string]bool{"nmap": true}); rec.Code != http.StatusBadRequest || plain.calls != 0 {
		t.Errorf("status = %d, starts = %d, want 400 and no scan", rec.Code, plain.calls)
	}
}

func TestStartScanRefusesABrokenBody(t *testing.T) {
	e, fs, s := nmapEnv(t, collector.NmapState{Available: true})
	rec := e.as(s, http.MethodPost, "/api/scans", `{"nmap": tru`)
	if rec.Code != http.StatusBadRequest || fs.calls+fs.nmapCalls != 0 {
		t.Errorf("status = %d, starts = %d, want 400 and no scan", rec.Code, fs.calls+fs.nmapCalls)
	}
}

func TestGetNmapReportsTheState(t *testing.T) {
	want := collector.NmapState{Available: true, Enabled: false}
	e, _, s := nmapEnv(t, want)
	rec := e.as(s, http.MethodGet, "/api/nmap", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var got collector.NmapState
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("state = %+v, want %+v", got, want)
	}
}

func TestGetNmapNeedsTheRightToRunScans(t *testing.T) {
	e, _, _ := nmapEnv(t, collector.NmapState{Available: true})
	e.addUser("vera", model.RoleViewer)
	if rec := e.as(e.mustLogin("vera"), http.MethodGet, "/api/nmap", nil); rec.Code != http.StatusForbidden {
		t.Errorf("viewer: status = %d, want 403", rec.Code)
	}
	if rec := e.do(http.MethodGet, "/api/nmap", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("no session: status = %d, want 401", rec.Code)
	}
}

func TestNmapRouteIsAbsentWithoutAStatusSource(t *testing.T) {
	e := newEnv(t, func(o *envOptions) { o.scanner = &fakeScanner{} })
	if rec := e.as(e.mustLogin("admin"), http.MethodGet, "/api/nmap", nil); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
