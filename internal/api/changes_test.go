package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/collector"
	"github.com/FlexEbat/Netscribe/internal/model"
)

func apiScan(t *testing.T, e *env, devs ...model.DeviceInput) {
	t.Helper()
	ctx := context.Background()
	sc, err := e.store.StartScan(ctx, "192.168.1.0/24")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.ApplyResult(ctx, sc.ID, collector.Result{Devices: devs}); err != nil {
		t.Fatal(err)
	}
	if err := e.store.FinishScan(ctx, sc.ID, "done", ""); err != nil {
		t.Fatal(err)
	}
}

func arp(mac, ip string) model.DeviceInput {
	return model.DeviceInput{Key: model.DeviceKey{MAC: mac, IP: ip}, Kind: model.KindUnknown, Source: "arp"}
}

type changesBody struct {
	Changes []model.Change `json:"changes"`
}

func TestChangesNeedASession(t *testing.T) {
	e := newEnv(t)
	if rec := e.do(http.MethodGet, "/api/changes", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestChangesAreAnEmptyArrayBeforeAnyScan(t *testing.T) {
	e := newEnv(t)
	rec := e.as(e.mustLogin("admin"), http.MethodGet, "/api/changes", nil)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status = %d, err = %v", rec.Code, err)
	}
	if string(raw["changes"]) != "[]" {
		t.Errorf("changes = %s, want []", raw["changes"])
	}
}

func TestChangesListTheNewestFirst(t *testing.T) {
	e := newEnv(t)
	a, b := arp("aa:00:00:00:00:01", "192.168.1.1"), arp("aa:00:00:00:00:02", "192.168.1.2")
	apiScan(t, e, a)    // the baseline
	apiScan(t, e, a, b) // b appears
	apiScan(t, e, a)    // b goes offline

	rec := e.as(e.mustLogin("admin"), http.MethodGet, "/api/changes", nil)
	var body changesBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status = %d, err = %v", rec.Code, err)
	}
	if len(body.Changes) != 2 || body.Changes[0].Kind != "device_offline" || body.Changes[1].Kind != "device_added" {
		t.Fatalf("changes = %+v", body.Changes)
	}
	if c := body.Changes[0]; c.ID == 0 || c.ScanID == 0 || c.EntityID == "" || c.At.IsZero() || !strings.Contains(c.Summary, "192.168.1.2") {
		t.Errorf("change = %+v", c)
	}

	rec = e.as(e.mustLogin("admin"), http.MethodGet, "/api/changes?limit=1", nil)
	body = changesBody{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Changes) != 1 || body.Changes[0].Kind != "device_offline" {
		t.Errorf("limit 1 = %+v, %v", body.Changes, err)
	}
}

func TestChangesRefuseABadLimit(t *testing.T) {
	e := newEnv(t)
	s := e.mustLogin("admin")
	for _, limit := range []string{"0", "-1", "abc", "501", "1.5", ""} {
		want := http.StatusBadRequest
		if limit == "" {
			want = http.StatusOK // an empty value means the default
		}
		if rec := e.as(s, http.MethodGet, "/api/changes?limit="+limit, nil); rec.Code != want {
			t.Errorf("limit %q: status = %d, want %d", limit, rec.Code, want)
		}
	}
	if rec := e.as(s, http.MethodGet, "/api/changes?limit=500", nil); rec.Code != http.StatusOK {
		t.Errorf("limit 500: status = %d", rec.Code)
	}
}

func TestMarkdownAndHTMLExportsCarryTheLastScansChanges(t *testing.T) {
	e := newEnv(t)
	a, b := arp("aa:00:00:00:00:01", "192.168.1.1"), arp("aa:00:00:00:00:02", "192.168.1.2")
	apiScan(t, e, a)
	apiScan(t, e, a, b)

	s := e.mustLogin("admin")
	for _, format := range []string{"md", "html"} {
		rec := e.as(s, http.MethodGet, "/api/export/"+format, nil)
		body := rec.Body.String()
		for _, want := range []string{"Changes", "device_added", "New device", "192.168.1.2"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s export lacks %q", format, want)
			}
		}
	}
}
