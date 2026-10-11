package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/collector"
	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/store"
)

// fakeScanner stands in for the collector scanner (section 10 of the spec).
type fakeScanner struct {
	err   error
	calls int
}

func (f *fakeScanner) Start() (model.Scan, error) {
	f.calls++
	if f.err != nil {
		return model.Scan{}, f.err
	}
	return model.Scan{ID: 42, Target: "192.168.1.0/24", Status: "running"}, nil
}

func TestStartScanAnswers202AndWritesAnAuditRow(t *testing.T) {
	fs := &fakeScanner{}
	e := newEnv(t, func(o *envOptions) { o.scanner = fs })
	e.addUser("olga", model.RoleOperator)
	s := e.mustLogin("olga")

	rec := e.as(s, http.MethodPost, "/api/scans", nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var got model.Scan
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != 42 || got.Status != "running" {
		t.Errorf("scan = %+v", got)
	}
	entries, err := e.store.ListAudit(context.Background(), store.AuditFilter{Action: "scan_start", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Username != "olga" || !strings.Contains(entries[0].Detail, "42") {
		t.Errorf("audit = %+v", entries)
	}
}

func TestStartScanErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		code int
	}{
		"a scan is running": {collector.ErrBusy, http.StatusConflict},
		"no targets":        {collector.ErrNoTargets, http.StatusBadRequest},
		"database failure":  {errors.New("disk I/O error at /var/lib/netscribe/x.db"), http.StatusInternalServerError},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, func(o *envOptions) { o.scanner = &fakeScanner{err: tc.err} })
			rec := e.as(e.mustLogin("admin"), http.MethodPost, "/api/scans", nil)
			if rec.Code != tc.code {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.code, rec.Body)
			}
			if strings.Contains(rec.Body.String(), "/var/lib") {
				t.Errorf("internal detail leaked: %s", rec.Body)
			}
		})
	}
}

func TestViewerCannotStartAScan(t *testing.T) {
	fs := &fakeScanner{}
	e := newEnv(t, func(o *envOptions) { o.scanner = fs })
	e.addUser("vera", model.RoleViewer)
	if rec := e.as(e.mustLogin("vera"), http.MethodPost, "/api/scans", nil); rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	if fs.calls != 0 {
		t.Error("the scanner was started for a viewer")
	}
}

func TestListScansNewestFirst(t *testing.T) {
	e := newEnv(t)
	first := seedScan(t, e)
	second := seedScan(t, e)
	rec := e.as(e.mustLogin("admin"), http.MethodGet, "/api/scans", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var body struct {
		Scans []model.Scan `json:"scans"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Scans) != 2 || body.Scans[0].ID != second.ID || body.Scans[1].ID != first.ID {
		t.Errorf("scans = %+v", body.Scans)
	}
}

func TestListScansIsEmptyArrayWithoutData(t *testing.T) {
	e := newEnv(t)
	rec := e.as(e.mustLogin("admin"), http.MethodGet, "/api/scans", nil)
	if rec.Body.String() != "{\"scans\":[]}\n" {
		t.Errorf("body = %q", rec.Body)
	}
}

func TestStartScanRouteIsAbsentWithoutAScanner(t *testing.T) {
	e := newEnv(t)
	if rec := e.as(e.mustLogin("admin"), http.MethodPost, "/api/scans", nil); rec.Code == http.StatusAccepted {
		t.Errorf("POST /api/scans worked without a scanner: %d", rec.Code)
	}
}
