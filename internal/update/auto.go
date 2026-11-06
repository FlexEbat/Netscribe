package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/FlexEbat/Netscribe/internal/config"
)

// Automatic updates (section 5.3 of the spec). The service decides and the helper acts: the
// service leaves the one word "update" in the request file, exactly as the update button
// does, and the helper installs the newest release of this repository after checking its
// checksum. Nothing about the version travels in the request.

const (
	// tryEvery is the shortest time between two automatic tries, whatever came of the first.
	tryEvery = 24 * time.Hour
	// tickEvery is how often the service looks at the clock and the check.
	tickEvery = time.Minute

	autoRecordFile = "auto-update.json"
	autoPausedFile = "auto-paused"
	maxPauseText   = 300
)

// The results of an automatic update, as the panel shows them.
const (
	ResultRequested  = "requested"
	ResultOK         = "ok"
	ResultFailed     = "failed"
	ResultRolledBack = "rolled back"
)

// Allowed says whether the mode permits installing latest over current. off permits nothing,
// patch a newer version with the same major and minor number, minor one with the same major
// number. A new major version is never installed by itself, a development build and a
// pre-release are never updated or offered, and an older or equal version is no update.
func Allowed(mode, current, latest string) bool {
	if mode != config.AutoPatch && mode != config.AutoMinor {
		return false
	}
	cur, err := ParseVersion(current)
	if err != nil || cur.Pre != "" {
		return false
	}
	next, err := ParseVersion(latest)
	if err != nil || next.Pre != "" || next.Compare(cur) <= 0 {
		return false
	}
	if next.Major != cur.Major {
		return false
	}
	if mode == config.AutoPatch && next.Minor != cur.Minor {
		return false
	}
	return true
}

// InWindow reports whether the clock time of t lies in the window from to (minutes after
// midnight, as config.ParseWindow returns them). The start is inside, the end is not, and a
// window whose end is before its start crosses midnight.
func InWindow(from, to int, t time.Time) bool {
	m := t.Hour()*60 + t.Minute()
	if from < to {
		return m >= from && m < to
	}
	return m >= from || m < to
}

// AutoRecord is the last automatic try and how it ended.
type AutoRecord struct {
	At      time.Time `json:"at"`
	From    string    `json:"from"`
	Tag     string    `json:"tag"`
	Result  string    `json:"result"`
	Message string    `json:"message,omitempty"`
}

// AutoStatus is what the panel shows about automatic updates.
type AutoStatus struct {
	Mode         string      `json:"mode"`
	Window       string      `json:"window"`
	Paused       bool        `json:"paused"`
	PausedReason string      `json:"pausedReason,omitempty"`
	Last         *AutoRecord `json:"last"`
}

// HelperOutcome is how the helper says the last request went.
type HelperOutcome struct {
	Action  string
	State   string // running, done, failed, rolled back
	Message string
	At      time.Time
}

// Requester leaves a request for the helper. Only the word is passed.
type Requester interface {
	Request(verb string) error
}

// Auto decides when an automatic update is requested. Every field must be set except Audit.
type Auto struct {
	// Dir is the state directory of the service: the record of the last try and the file
	// that pauses automatic updates live there.
	Dir     string
	Version string // the running version

	Mode   func() string // off, patch or minor
	Window func() string // "03:00-05:00"
	Status func() Status // what the checker knows
	Busy   func() bool   // a scan is running
	Helper Requester
	Result func() HelperOutcome // how the helper's last request went
	// Audit writes one entry of the audit log; nil writes none.
	Audit func(action, result, detail string)
	Now   func() time.Time

	mu sync.Mutex
}

func (a *Auto) recordPath() string { return filepath.Join(a.Dir, autoRecordFile) }
func (a *Auto) pausedPath() string { return filepath.Join(a.Dir, autoPausedFile) }

func (a *Auto) audit(action, result, detail string) {
	if a.Audit != nil {
		a.Audit(action, result, detail)
	}
}

// Last reads the record of the last try.
func (a *Auto) Last() *AutoRecord {
	data, err := os.ReadFile(a.recordPath())
	if err != nil {
		return nil
	}
	var rec AutoRecord
	if json.Unmarshal(data, &rec) != nil || rec.At.IsZero() {
		return nil
	}
	return &rec
}

func (a *Auto) saveRecord(rec AutoRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	tmp := a.recordPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.recordPath())
}

// Paused tells whether a rollback paused automatic updates, and why.
func (a *Auto) Paused() (bool, string) {
	data, err := os.ReadFile(a.pausedPath())
	if err != nil {
		return false, ""
	}
	return true, strings.TrimSpace(string(data))
}

func (a *Auto) pause(reason string) error {
	if len(reason) > maxPauseText {
		reason = strings.ToValidUTF8(reason[:maxPauseText], "")
	}
	return os.WriteFile(a.pausedPath(), []byte(reason+"\n"), 0o600)
}

// Resume lets automatic updates go on after a person looked at the rollback.
func (a *Auto) Resume() error {
	if err := os.Remove(a.pausedPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Snapshot is the status for the panel.
func (a *Auto) Snapshot() AutoStatus {
	paused, reason := a.Paused()
	return AutoStatus{Mode: a.Mode(), Window: a.Window(), Paused: paused, PausedReason: reason, Last: a.Last()}
}

// Run looks at the clock once a minute until ctx ends.
func (a *Auto) Run(ctx context.Context, logged func(error)) {
	tick := time.NewTicker(tickEvery)
	defer tick.Stop()
	for {
		if _, err := a.Tick(ctx); err != nil && logged != nil {
			logged(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// Tick settles what the helper reported about the last try, then requests an update when
// every condition holds. It tells whether it requested one.
func (a *Auto) Tick(_ context.Context) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.settle()

	if a.Mode() == config.AutoOff {
		return false, nil
	}
	if cur, err := ParseVersion(a.Version); err != nil || cur.Pre != "" {
		return false, nil // a development build never updates itself
	}
	if paused, _ := a.Paused(); paused {
		return false, nil
	}
	from, to, err := config.ParseWindow(a.Window())
	if err != nil {
		return false, fmt.Errorf("updates.window: %w", err)
	}
	now := a.Now()
	if !InWindow(from, to, now) || a.Busy() {
		return false, nil
	}
	if last := a.Last(); last != nil && now.Sub(last.At) < tryEvery {
		return false, nil
	}
	st := a.Status()
	if st.Latest == nil || !Allowed(a.Mode(), a.Version, st.Latest.Tag) {
		return false, nil
	}

	// The try is recorded before the request, so a helper that fails is not asked again
	// every minute: the next try is a day away whatever happens.
	rec := AutoRecord{At: now, From: a.Version, Tag: st.Latest.Tag, Result: ResultRequested}
	if err := a.saveRecord(rec); err != nil {
		return false, fmt.Errorf("save the record of the automatic update: %w", err)
	}
	if err := a.Helper.Request("update"); err != nil {
		rec.Result, rec.Message = ResultFailed, err.Error()
		_ = a.saveRecord(rec) // the request error is the one to report
		a.audit("auto_update_request", "error", "update to "+rec.Tag)
		return false, fmt.Errorf("request the automatic update: %w", err)
	}
	a.audit("auto_update_request", "ok", "update to "+rec.Tag)
	return true, nil
}

// settle turns the helper's report about a requested update into the result of the record.
// A rollback also pauses automatic updates until a person resumes them.
func (a *Auto) settle() {
	rec := a.Last()
	if rec == nil || rec.Result != ResultRequested || a.Result == nil {
		return
	}
	out := a.Result()
	if out.Action != "update" || out.At.Before(rec.At) {
		return
	}
	switch out.State {
	case "done":
		rec.Result = ResultOK
		if cur, err := ParseVersion(a.Version); err == nil {
			if want, err := ParseVersion(rec.Tag); err == nil && cur.Compare(want) < 0 {
				rec.Result = ResultFailed // the helper was done, and this program is still the old one
				rec.Message = "the service still runs " + a.Version
			}
		}
	case "failed":
		rec.Result, rec.Message = ResultFailed, out.Message
	case ResultRolledBack:
		rec.Result, rec.Message = ResultRolledBack, out.Message
		_ = a.pause(fmt.Sprintf("%s did not start and %s was put back: %s", rec.Tag, rec.From, out.Message))
		a.audit("auto_update_rollback", "error", rec.Tag+" rolled back to "+rec.From)
	default:
		return // still running
	}
	_ = a.saveRecord(*rec) // the next tick settles it again when this one is lost
}
