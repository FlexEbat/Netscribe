package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/setup"
	"github.com/FlexEbat/Netscribe/internal/store"
	"github.com/FlexEbat/Netscribe/internal/update"
)

type fakeHelper struct {
	state    HelperState
	requests []string
	err      error
}

func (f *fakeHelper) State() HelperState { return f.state }
func (f *fakeHelper) Request(verb string) error {
	if f.err != nil {
		return f.err
	}
	f.requests = append(f.requests, verb)
	return nil
}

type fakeSettings struct {
	groups  []setup.PanelGroup
	applied []map[string]json.RawMessage
	err     error
}

func (f *fakeSettings) Settings() ([]setup.PanelGroup, error) { return f.groups, nil }
func (f *fakeSettings) Apply(c map[string]json.RawMessage) (setup.PanelResult, error) {
	if f.err != nil {
		return setup.PanelResult{}, f.err
	}
	f.applied = append(f.applied, c)
	res := setup.PanelResult{}
	for k := range c {
		res.Saved = append(res.Saved, k)
	}
	return res, nil
}

// releaseChecker is a checker that has already seen the newest release.
func releaseChecker(t *testing.T, tag string) *update.Checker {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"tag_name":%q,"body":"- notes"}`, tag)
	}))
	t.Cleanup(srv.Close)
	c := &update.Checker{URL: srv.URL}
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	return c
}

func systemEnv(t *testing.T, mods ...func(*envOptions)) (*env, session, session) {
	t.Helper()
	e := newEnv(t, append([]func(*envOptions){func(o *envOptions) {
		o.version = "v0.2.0"
		o.updates = releaseChecker(t, "v0.3.0")
		o.helper = &fakeHelper{state: HelperState{Available: true, State: "idle"}}
		o.settings = &fakeSettings{}
	}}, mods...)...)
	e.addUser("vera", model.RoleViewer)
	return e, e.mustLogin("admin"), e.mustLogin("vera")
}

func TestVersionIsOpenToEveryRoleAndReportsTheUpdate(t *testing.T) {
	e, admin, viewer := systemEnv(t)
	for name, s := range map[string]session{"admin": admin, "viewer": viewer} {
		rec := e.as(s, http.MethodGet, "/api/version", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", name, rec.Code)
		}
		var got versionAnswer
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Version != "v0.2.0" || !got.Update.Available || got.Update.Latest == nil || got.Update.Latest.Tag != "v0.3.0" {
			t.Errorf("%s: answer = %+v", name, got)
		}
	}
	if e.do(http.MethodGet, "/api/version", nil).Code != http.StatusUnauthorized {
		t.Error("the version needs a session")
	}
}

func TestVersionWithoutACheckerStillAnswers(t *testing.T) {
	e := newEnv(t)
	rec := e.as(e.mustLogin("admin"), http.MethodGet, "/api/version", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"version":"dev"`) {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
}

func TestStartUpdateAsksTheHelperAndIsAudited(t *testing.T) {
	h := &fakeHelper{state: HelperState{Available: true, State: "idle"}}
	e, admin, _ := systemEnv(t, func(o *envOptions) { o.helper = h })
	rec := e.as(admin, http.MethodPost, "/api/update", nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if len(h.requests) != 1 || h.requests[0] != "update" {
		t.Fatalf("requests = %v", h.requests)
	}
	entries, err := e.store.ListAudit(context.Background(), store.AuditFilter{Action: "update_request", Limit: 10})
	if err != nil || len(entries) != 1 || !strings.Contains(entries[0].Detail, "v0.3.0") {
		t.Fatalf("audit = %+v, %v", entries, err)
	}
}

func TestStartUpdateIsForAdministratorsOnly(t *testing.T) {
	h := &fakeHelper{state: HelperState{Available: true, State: "idle"}}
	e, _, viewer := systemEnv(t, func(o *envOptions) { o.helper = h })
	e.addUser("olga", model.RoleOperator)
	for name, s := range map[string]session{"viewer": viewer, "operator": e.mustLogin("olga")} {
		for _, path := range []string{"/api/update", "/api/system/restart", "/api/update/check"} {
			if code := e.as(s, http.MethodPost, path, nil).Code; code != http.StatusForbidden {
				t.Errorf("%s %s: status = %d, want 403", name, path, code)
			}
		}
	}
	if len(h.requests) != 0 {
		t.Errorf("the helper was asked: %v", h.requests)
	}
}

func TestStartUpdateRefusalsAreExplained(t *testing.T) {
	e, admin, _ := systemEnv(t, func(o *envOptions) {
		o.helper = &fakeHelper{state: HelperState{Available: false, State: "idle"}}
	})
	if code := e.as(admin, http.MethodPost, "/api/update", nil).Code; code != http.StatusNotImplemented {
		t.Errorf("no helper: status = %d, want 501", code)
	}

	e, admin, _ = systemEnv(t, func(o *envOptions) { o.updates = releaseChecker(t, "v0.2.0") })
	if code := e.as(admin, http.MethodPost, "/api/update", nil).Code; code != http.StatusConflict {
		t.Errorf("nothing newer: status = %d, want 409", code)
	}

	e, admin, _ = systemEnv(t, func(o *envOptions) {
		o.helper = &fakeHelper{state: HelperState{Available: true, State: "running"}, err: ErrHelperBusy}
	})
	if code := e.as(admin, http.MethodPost, "/api/update", nil).Code; code != http.StatusConflict {
		t.Errorf("busy helper: status = %d, want 409", code)
	}
}

func TestRestartAsksTheHelper(t *testing.T) {
	h := &fakeHelper{state: HelperState{Available: true, State: "idle"}}
	e, admin, _ := systemEnv(t, func(o *envOptions) { o.helper = h })
	if code := e.as(admin, http.MethodPost, "/api/system/restart", nil).Code; code != http.StatusAccepted {
		t.Fatalf("status = %d", code)
	}
	if len(h.requests) != 1 || h.requests[0] != "restart" {
		t.Errorf("requests = %v", h.requests)
	}
}

func TestSettingsAreReadAndChangedByAnAdministrator(t *testing.T) {
	fs := &fakeSettings{groups: []setup.PanelGroup{{Name: "Scanning", Settings: []setup.PanelSetting{{Key: "scan.interval", Type: "duration", Value: "15m"}}}}}
	e, admin, viewer := systemEnv(t, func(o *envOptions) { o.settings = fs })
	rec := e.as(admin, http.MethodGet, "/api/settings", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"scan.interval"`) {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if code := e.as(viewer, http.MethodGet, "/api/settings", nil).Code; code != http.StatusForbidden {
		t.Errorf("viewer read the settings: %d", code)
	}

	rec = e.as(admin, http.MethodPut, "/api/settings", map[string]any{"changes": map[string]any{"scan.interval": "30m", "snmp.communities": []string{"topsecret"}}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if len(fs.applied) != 1 || string(fs.applied[0]["scan.interval"]) != `"30m"` {
		t.Fatalf("applied = %v", fs.applied)
	}
	entries, err := e.store.ListAudit(context.Background(), store.AuditFilter{Action: "settings_change", Limit: 10})
	if err != nil || len(entries) != 1 {
		t.Fatalf("audit = %+v, %v", entries, err)
	}
	if !strings.Contains(entries[0].Detail, "scan.interval") || strings.Contains(entries[0].Detail, "topsecret") {
		t.Errorf("audit detail %q: keys yes, values never", entries[0].Detail)
	}
}

func TestSettingsRefusalIsABadRequestAndAFailureIsNot(t *testing.T) {
	fs := &fakeSettings{err: setup.PanelInputError{Msg: "Scan every: must be a duration"}}
	e, admin, _ := systemEnv(t, func(o *envOptions) { o.settings = fs })
	rec := e.as(admin, http.MethodPut, "/api/settings", map[string]any{"changes": map[string]any{"scan.interval": "soon"}})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "must be a duration") {
		t.Errorf("status = %d, body %s", rec.Code, rec.Body)
	}
	fs.err = fmt.Errorf("disk full")
	rec = e.as(admin, http.MethodPut, "/api/settings", map[string]any{"changes": map[string]any{"scan.interval": "30m"}})
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "disk full") {
		t.Errorf("status = %d, body %s: a system error must not reach the page", rec.Code, rec.Body)
	}
	for name, body := range map[string]any{"empty": map[string]any{}, "no changes": map[string]any{"changes": map[string]any{}}, "broken": `{"changes": `} {
		if code := e.as(admin, http.MethodPut, "/api/settings", body).Code; code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, code)
		}
	}
}
