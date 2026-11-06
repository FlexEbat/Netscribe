package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/config"
)

func TestAllowedOverVersionPairs(t *testing.T) {
	for _, tc := range []struct {
		mode, cur, latest string
		want              bool
	}{
		{"off", "v1.2.3", "v1.2.4", false},
		{"off", "v1.2.3", "v1.3.0", false},
		{"", "v1.2.3", "v1.2.4", false},
		{"patch", "v1.2.3", "v1.2.4", true},
		{"patch", "v1.2.3", "v1.2.10", true},
		{"patch", "v1.2.3", "v1.3.0", false}, // a minor bump
		{"patch", "v1.2.3", "v2.0.0", false}, // a major bump
		{"patch", "v1.2.3", "v1.2.3", false}, // the same version
		{"patch", "v1.2.4", "v1.2.3", false}, // an older release
		{"minor", "v1.2.3", "v1.2.4", true},
		{"minor", "v1.2.3", "v1.9.0", true},
		{"minor", "v1.2.3", "v2.0.0", false},
		{"minor", "v0.8.0", "v0.9.0", true},
		{"minor", "v0.8.0", "v1.0.0", false},
		{"minor", "v1.2.3", "v1.3.0-rc.1", false}, // a pre-release is never offered
		{"minor", "v1.2.3-rc.1", "v1.3.0", false}, // a pre-release does not update itself
		{"minor", "dev", "v1.3.0", false},         // a development build never updates itself
		{"minor", "", "v1.3.0", false},
		{"minor", "v1.2.3", "latest", false},
		{"minor", "v1.2.3", "", false},
		{"major", "v1.2.3", "v2.0.0", false}, // there is no such mode
	} {
		if got := Allowed(tc.mode, tc.cur, tc.latest); got != tc.want {
			t.Errorf("Allowed(%q, %q, %q) = %v, want %v", tc.mode, tc.cur, tc.latest, got, tc.want)
		}
	}
}

func TestInWindow(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 11, 5, h, m, 0, 0, time.Local) }
	day := func(w string, h, m int) bool {
		from, to, err := config.ParseWindow(w)
		if err != nil {
			t.Fatal(err)
		}
		return InWindow(from, to, at(h, m))
	}
	for _, tc := range []struct {
		window string
		h, m   int
		want   bool
	}{
		{"03:00-05:00", 3, 0, true}, // the start is inside
		{"03:00-05:00", 4, 59, true},
		{"03:00-05:00", 5, 0, false}, // the end is not
		{"03:00-05:00", 2, 59, false},
		{"03:00-05:00", 12, 0, false},
		{"23:00-02:00", 23, 0, true}, // across midnight
		{"23:00-02:00", 0, 30, true},
		{"23:00-02:00", 1, 59, true},
		{"23:00-02:00", 2, 0, false},
		{"23:00-02:00", 22, 59, false},
		{"23:00-02:00", 12, 0, false},
	} {
		if got := day(tc.window, tc.h, tc.m); got != tc.want {
			t.Errorf("window %s at %02d:%02d = %v, want %v", tc.window, tc.h, tc.m, got, tc.want)
		}
	}
}

type fakeHelper struct {
	requests []string
	err      error
}

func (f *fakeHelper) Request(verb string) error {
	f.requests = append(f.requests, verb)
	return f.err
}

type autoHarness struct {
	*Auto
	helper  *fakeHelper
	mode    string
	window  string
	busy    bool
	latest  *Release
	outcome HelperOutcome
	audit   []string
	now     time.Time
}

func newAuto(t *testing.T) *autoHarness {
	t.Helper()
	h := &autoHarness{
		helper: &fakeHelper{}, mode: "minor", window: "03:00-05:00",
		latest: &Release{Tag: "v0.9.0", Version: "0.9.0"},
		now:    time.Date(2026, 11, 5, 3, 30, 0, 0, time.Local),
	}
	h.Auto = &Auto{
		Dir: t.TempDir(), Version: "v0.8.0",
		Mode: func() string { return h.mode }, Window: func() string { return h.window },
		Status: func() Status { return Status{Current: "v0.8.0", Latest: h.latest} },
		Busy:   func() bool { return h.busy },
		Helper: h.helper,
		Result: func() HelperOutcome { return h.outcome },
		Audit:  func(action, result, detail string) { h.audit = append(h.audit, action+"/"+result+"/"+detail) },
		Now:    func() time.Time { return h.now },
	}
	return h
}

func (h *autoHarness) tick(t *testing.T) bool {
	t.Helper()
	ok, err := h.Tick(context.Background())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	return ok
}

func TestAutoRequestsInsideTheWindowAndWritesOneWord(t *testing.T) {
	h := newAuto(t)
	if !h.tick(t) {
		t.Fatal("nothing was requested although every condition holds")
	}
	if len(h.helper.requests) != 1 || h.helper.requests[0] != "update" {
		t.Errorf("requests = %v, want the single word update and nothing else", h.helper.requests)
	}
	if len(h.audit) != 1 || h.audit[0] != "auto_update_request/ok/update to v0.9.0" {
		t.Errorf("audit = %v, want one auto_update_request entry", h.audit)
	}
	if last := h.Last(); last == nil || last.Result != ResultRequested || last.Tag != "v0.9.0" || last.From != "v0.8.0" {
		t.Errorf("record = %+v, want the try noted", last)
	}
}

func TestAutoRequestsNothingWhenAConditionFails(t *testing.T) {
	for name, edit := range map[string]func(*autoHarness){
		"the mode is off":       func(h *autoHarness) { h.mode = "off" },
		"the mode is empty":     func(h *autoHarness) { h.mode = "" },
		"a minor bump in patch": func(h *autoHarness) { h.mode = "patch" },
		"a major bump":          func(h *autoHarness) { h.latest = &Release{Tag: "v1.0.0"} },
		"before the window":     func(h *autoHarness) { h.now = time.Date(2026, 11, 5, 2, 59, 0, 0, time.Local) },
		"at the end of window":  func(h *autoHarness) { h.now = time.Date(2026, 11, 5, 5, 0, 0, 0, time.Local) },
		"a scan is running":     func(h *autoHarness) { h.busy = true },
		"nothing is known":      func(h *autoHarness) { h.latest = nil },
		"no newer release":      func(h *autoHarness) { h.latest = &Release{Tag: "v0.8.0"} },
		"a pre-release":         func(h *autoHarness) { h.latest = &Release{Tag: "v0.9.0-rc.1"} },
		"a development build":   func(h *autoHarness) { h.Auto.Version = "dev" },
		"an empty version":      func(h *autoHarness) { h.Auto.Version = "" },
		"a pre-release build":   func(h *autoHarness) { h.Auto.Version = "v0.8.0-rc.1" },
		"a tried a few hours ago": func(h *autoHarness) {
			if err := h.saveRecord(AutoRecord{At: h.now.Add(-23 * time.Hour), Tag: "v0.8.5", Result: ResultOK}); err != nil {
				t.Fatal(err)
			}
		},
		"paused after a rollback": func(h *autoHarness) {
			if err := h.pause("v0.8.5 did not start"); err != nil {
				t.Fatal(err)
			}
		},
	} {
		h := newAuto(t)
		if name == "a minor bump in patch" {
			h.Auto.Version = "v0.8.0"
		}
		edit(h)
		if h.tick(t) || len(h.helper.requests) != 0 {
			t.Errorf("%s: an update was requested, want none", name)
		}
	}
}

func TestAutoTriesAgainAfterTwentyFourHours(t *testing.T) {
	h := newAuto(t)
	if err := h.saveRecord(AutoRecord{At: h.now.Add(-24 * time.Hour), Tag: "v0.8.5", Result: ResultFailed}); err != nil {
		t.Fatal(err)
	}
	if !h.tick(t) {
		t.Error("no request exactly 24 hours after the last try, want one")
	}
}

func TestAutoRecordsAFailedRequestAndDoesNotRetryAtOnce(t *testing.T) {
	h := newAuto(t)
	h.helper.err = errors.New("the helper is busy")
	if _, err := h.Tick(context.Background()); err == nil {
		t.Fatal("a failed request was not reported")
	}
	h.helper.err = nil
	if h.tick(t) {
		t.Error("the helper was asked again within the day after a failure")
	}
	if last := h.Last(); last == nil || last.Result != ResultFailed {
		t.Errorf("record = %+v, want the failure noted", last)
	}
	if len(h.audit) != 1 || h.audit[0] != "auto_update_request/error/update to v0.9.0" {
		t.Errorf("audit = %v, want one entry with result error", h.audit)
	}
}

func TestAutoPausesAfterARollbackUntilAPersonResumes(t *testing.T) {
	h := newAuto(t)
	h.tick(t)
	h.outcome = HelperOutcome{Action: "update", State: "rolled back", Message: "the new program did not answer in 60 seconds", At: h.now.Add(2 * time.Minute)}
	h.now = h.now.Add(5 * time.Minute)
	h.tick(t)

	if paused, why := h.Paused(); !paused || why == "" {
		t.Fatalf("paused = %v %q, want automatic updates paused with a reason", paused, why)
	}
	if last := h.Last(); last == nil || last.Result != ResultRolledBack {
		t.Errorf("record = %+v, want the rollback noted", last)
	}
	foundAudit := false
	for _, e := range h.audit {
		if e == "auto_update_rollback/error/v0.9.0 rolled back to v0.8.0" {
			foundAudit = true
		}
	}
	if !foundAudit {
		t.Errorf("audit = %v, want an auto_update_rollback entry", h.audit)
	}

	// A day later, still paused: nothing is requested.
	h.now = h.now.Add(48 * time.Hour)
	if h.tick(t) {
		t.Error("an update was requested while paused")
	}
	if err := h.Resume(); err != nil {
		t.Fatal(err)
	}
	if !h.tick(t) {
		t.Error("nothing was requested after a person resumed")
	}
}

func TestAutoNotesASuccessfulUpdate(t *testing.T) {
	h := newAuto(t)
	h.tick(t)
	h.outcome = HelperOutcome{Action: "update", State: "done", At: h.now.Add(time.Minute)}
	h.Auto.Version = "v0.9.0" // the service restarted as the new version
	h.now = h.now.Add(2 * time.Minute)
	h.tick(t)
	if last := h.Last(); last == nil || last.Result != ResultOK {
		t.Errorf("record = %+v, want ok", last)
	}
	if paused, _ := h.Paused(); paused {
		t.Error("a good update paused automatic updates")
	}
}

func TestAutoIgnoresAnOutcomeThatIsOlderThanTheTry(t *testing.T) {
	h := newAuto(t)
	h.outcome = HelperOutcome{Action: "update", State: "rolled back", At: h.now.Add(-time.Hour)}
	h.tick(t)
	if paused, _ := h.Paused(); paused {
		t.Error("an old rollback paused a new try")
	}
}

func TestAutoDoneButStillOldIsAFailure(t *testing.T) {
	h := newAuto(t)
	h.tick(t)
	h.outcome = HelperOutcome{Action: "update", State: "done", At: h.now.Add(time.Minute)}
	h.now = h.now.Add(2 * time.Minute)
	h.tick(t)
	if last := h.Last(); last == nil || last.Result != ResultFailed {
		t.Errorf("record = %+v, want a failure when the service still runs the old version", last)
	}
}

func TestAutoStateFilesAreStrictAndSnapshotShowsThem(t *testing.T) {
	h := newAuto(t)
	if err := h.pause("because"); err != nil {
		t.Fatal(err)
	}
	snap := h.Snapshot()
	if !snap.Paused || snap.PausedReason != "because" || snap.Mode != "minor" || snap.Window != "03:00-05:00" {
		t.Errorf("snapshot = %+v", snap)
	}
	info, err := os.Stat(filepath.Join(h.Dir, "auto-paused"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the pause file mode = %v, %v, want 0600", info, err)
	}
	if err := os.WriteFile(filepath.Join(h.Dir, "auto-update.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if h.Last() != nil {
		t.Error("a broken record file was read as a record")
	}
}
