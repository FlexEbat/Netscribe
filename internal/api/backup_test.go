package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/store"
)

type fakeBackup struct {
	archive  []byte
	createEr error
	stageErr error
	staged   []byte
	creates  int
}

func (f *fakeBackup) Create(context.Context) (io.ReadCloser, int64, error) {
	f.creates++
	if f.createEr != nil {
		return nil, 0, f.createEr
	}
	return io.NopCloser(bytes.NewReader(f.archive)), int64(len(f.archive)), nil
}

func (f *fakeBackup) Stage(_ context.Context, r io.Reader) (BackupSummary, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return BackupSummary{}, err
	}
	f.staged = b
	if f.stageErr != nil {
		return BackupSummary{}, f.stageErr
	}
	return BackupSummary{Version: "v0.2.0", CreatedAt: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC), Schema: 14, Devices: 7, Users: 2}, nil
}

func withPassword(pw string) func(*http.Request) {
	return func(r *http.Request) {
		r.Header.Set(confirmHeader, base64.StdEncoding.EncodeToString([]byte(pw)))
	}
}

// backupEnv is a system environment with a fake backup service.
func backupEnv(t *testing.T, fb *fakeBackup, h *fakeHelper) (*env, session, session) {
	t.Helper()
	return systemEnv(t, func(o *envOptions) {
		o.backup = fb
		o.helper = h
	})
}

func asWith(e *env, s session, method, path string, body any, extra ...func(*http.Request)) *httptest.ResponseRecorder {
	e.t.Helper()
	mods := append([]func(*http.Request){func(r *http.Request) {
		r.AddCookie(s.cookie)
		if method != http.MethodGet {
			r.Header.Set(csrfHeader, s.csrf)
		}
	}}, extra...)
	return e.do(method, path, body, mods...)
}

func TestBackupDownloadNeedsTheAdministratorsPassword(t *testing.T) {
	fb := &fakeBackup{archive: []byte("ARCHIVE")}
	e, admin, viewer := backupEnv(t, fb, &fakeHelper{state: HelperState{Available: true}})
	e.addUser("olga", model.RoleOperator)
	operator := e.mustLogin("olga")

	rec := asWith(e, admin, http.MethodGet, "/api/backup", nil, withPassword(goodPassword))
	if rec.Code != http.StatusOK || rec.Body.String() != "ARCHIVE" {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body)
	}
	h := rec.Header()
	if h.Get("Cache-Control") != "no-store" || h.Get("Content-Type") != "application/gzip" ||
		!strings.HasPrefix(h.Get("Content-Disposition"), `attachment; filename="netscribe-`) || h.Get("Content-Length") != "7" {
		t.Errorf("headers = %v", h)
	}
	entries, err := e.store.ListAudit(context.Background(), store.AuditFilter{Action: "backup_create", Limit: 5})
	if err != nil || len(entries) != 1 || entries[0].Result != "ok" {
		t.Fatalf("audit = %+v, %v", entries, err)
	}

	for name, s := range map[string]session{"viewer": viewer, "operator": operator} {
		if code := asWith(e, s, http.MethodGet, "/api/backup", nil, withPassword(goodPassword)).Code; code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", name, code)
		}
	}
	if code := e.do(http.MethodGet, "/api/backup", nil).Code; code != http.StatusUnauthorized {
		t.Errorf("no session: status = %d, want 401", code)
	}
	if fb.creates != 1 {
		t.Errorf("an archive was made %d times for one allowed request", fb.creates)
	}
}

func TestBackupDownloadRefusesAWrongOrMissingPassword(t *testing.T) {
	fb := &fakeBackup{archive: []byte("ARCHIVE")}
	e, admin, _ := backupEnv(t, fb, &fakeHelper{state: HelperState{Available: true}})

	if code := asWith(e, admin, http.MethodGet, "/api/backup", nil, withPassword("not the password")).Code; code != http.StatusForbidden {
		t.Errorf("wrong password: status = %d, want 403", code)
	}
	if code := asWith(e, admin, http.MethodGet, "/api/backup", nil).Code; code != http.StatusBadRequest {
		t.Errorf("no password: status = %d, want 400", code)
	}
	bad := func(r *http.Request) { r.Header.Set(confirmHeader, "%%% not base64") }
	if code := asWith(e, admin, http.MethodGet, "/api/backup", nil, bad).Code; code != http.StatusBadRequest {
		t.Errorf("bad encoding: status = %d, want 400", code)
	}
	if fb.creates != 0 {
		t.Error("an archive was made without the password")
	}
	entries, err := e.store.ListAudit(context.Background(), store.AuditFilter{Action: "access_denied", Limit: 5})
	if err != nil || len(entries) != 1 {
		t.Fatalf("audit = %+v, %v", entries, err)
	}
}

func TestBackupDownloadIsNotForTokens(t *testing.T) {
	fb := &fakeBackup{archive: []byte("ARCHIVE")}
	e, admin, _ := backupEnv(t, fb, &fakeHelper{state: HelperState{Available: true}})
	tok := issueToken(t, e, admin, map[string]any{"name": "ci", "scope": "write"})
	for _, path := range []string{"/api/backup"} {
		if code := e.do(http.MethodGet, path, nil, bearer(tok.Value), withPassword(goodPassword)).Code; code != http.StatusForbidden {
			t.Errorf("%s with a token: status = %d, want 403", path, code)
		}
	}
	if code := e.do(http.MethodPost, "/api/backup/restore", "x", bearer(tok.Value), withPassword(goodPassword)).Code; code != http.StatusForbidden {
		t.Errorf("restore with a token: status = %d, want 403", code)
	}
	if fb.creates != 0 || fb.staged != nil {
		t.Error("a token reached the backup service")
	}
}

func TestBackupDownloadReportsAFailureBeforeItSendsAnything(t *testing.T) {
	fb := &fakeBackup{createEr: errors.New("disk full")}
	e, admin, _ := backupEnv(t, fb, &fakeHelper{state: HelperState{Available: true}})
	rec := asWith(e, admin, http.MethodGet, "/api/backup", nil, withPassword(goodPassword))
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "disk full") {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body)
	}
}

func TestRestoreStagesTheArchiveAndAsksTheHelper(t *testing.T) {
	fb := &fakeBackup{}
	h := &fakeHelper{state: HelperState{Available: true, State: "idle"}}
	e, admin, viewer := backupEnv(t, fb, h)

	if code := asWith(e, viewer, http.MethodPost, "/api/backup/restore", "x", withPassword(goodPassword)).Code; code != http.StatusForbidden {
		t.Errorf("viewer: status = %d, want 403", code)
	}
	if code := asWith(e, admin, http.MethodPost, "/api/backup/restore", "x", withPassword("wrong wrong")).Code; code != http.StatusForbidden {
		t.Errorf("wrong password: status = %d, want 403", code)
	}
	if fb.staged != nil || len(h.requests) != 0 {
		t.Fatal("a refused request staged something")
	}

	rec := asWith(e, admin, http.MethodPost, "/api/backup/restore", "THE ARCHIVE", withPassword(goodPassword))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if string(fb.staged) != "THE ARCHIVE" || len(h.requests) != 1 || h.requests[0] != "restore" {
		t.Fatalf("staged %q, requests %v", fb.staged, h.requests)
	}
	if !strings.Contains(rec.Body.String(), `"devices":7`) {
		t.Errorf("body = %s", rec.Body)
	}
	entries, err := e.store.ListAudit(context.Background(), store.AuditFilter{Action: "backup_restore", Limit: 5})
	if err != nil || len(entries) != 1 || entries[0].Result != "ok" {
		t.Fatalf("audit = %+v, %v", entries, err)
	}
}

func TestRestoreRefusalsAreShownAndNothingIsRequested(t *testing.T) {
	h := &fakeHelper{state: HelperState{Available: true}}
	fb := &fakeBackup{stageErr: BackupRefusal("the backup was made by a newer Netscribe than this one")}
	e, admin, _ := backupEnv(t, fb, h)
	rec := asWith(e, admin, http.MethodPost, "/api/backup/restore", "x", withPassword(goodPassword))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "newer Netscribe") {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if len(h.requests) != 0 {
		t.Error("the helper was asked to restore a refused backup")
	}

	fb.stageErr = errors.New("disk full")
	rec = asWith(e, admin, http.MethodPost, "/api/backup/restore", "x", withPassword(goodPassword))
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "disk full") || len(h.requests) != 0 {
		t.Errorf("status = %d, body %s, requests %v", rec.Code, rec.Body, h.requests)
	}
}

func TestRestoreWithoutAHelperPointsToTheCommand(t *testing.T) {
	fb := &fakeBackup{}
	e, admin, _ := backupEnv(t, fb, &fakeHelper{state: HelperState{Available: false}})
	rec := asWith(e, admin, http.MethodPost, "/api/backup/restore", "x", withPassword(goodPassword))
	if rec.Code != http.StatusNotImplemented || !strings.Contains(rec.Body.String(), "netscribe restore") || fb.staged != nil {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
}

func TestRestoreAnswers409WhenTheHelperIsBusy(t *testing.T) {
	h := &fakeHelper{state: HelperState{Available: true}, err: ErrHelperBusy}
	e, admin, _ := backupEnv(t, &fakeBackup{}, h)
	rec := asWith(e, admin, http.MethodPost, "/api/backup/restore", "x", withPassword(goodPassword))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
}

func TestBackupRoutesAreMissingWithoutAService(t *testing.T) {
	e, admin, _ := systemEnv(t)
	for _, path := range []string{"/api/backup", "/api/backup/restore"} {
		if code := asWith(e, admin, http.MethodGet, path, nil, withPassword(goodPassword)).Code; code != http.StatusNotFound && code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d", path, code)
		}
	}
}
