package setup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/FlexEbat/Netscribe/internal/update"
)

// defaultRollbackWait is how long the helper waits for the new version to answer.
const defaultRollbackWait = 60 * time.Second

// The helper is the one part of Netscribe that runs as root on behalf of the panel. The
// panel (a process without rights) leaves a request file; systemd starts the helper, which
// reads one word from it and does that one thing. Nothing else in the file is used, and the
// helper never takes a version, a path or a command from it: "update" installs the newest
// release of this repository after checking its checksum, "restart" restarts netscribe, and
// "restore" puts the backup the panel staged in place (the stage is always StageDir).

// HelperStatusPath is where the helper tells how the last request went. The directory
// belongs to root, so only the helper can write there, and everybody can read it.
const HelperStatusPath = "/run/netscribe-helper/status.json"

const (
	// HelperUpdate asks for the newest release to be installed.
	HelperUpdate = "update"
	// HelperRestart asks for the service to be restarted.
	HelperRestart = "restart"
	// HelperRolledBack is the state the helper reports when the new program did not answer
	// after an update and the old one was put back.
	HelperRolledBack = "rolled back"

	// previousProgram is where the helper keeps the program an update replaces, next to it.
	previousProgram = "netscribe.previous"

	maxRequestSize = 64
	maxStatusText  = 300
)

// HelperStatus is what the helper wrote about a request.
type HelperStatus struct {
	Action  string    `json:"action"`
	State   string    `json:"state"` // running, done, failed, rolled back
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}

// ReadRequest reads the request file without following a link: the service account may
// create the file, and the helper runs as root. It accepts a regular file of a few bytes.
func ReadRequest(path string) (string, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0) //nolint:gosec // the path is the one in the unit
	if err != nil {
		return "", fmt.Errorf("open the request: %w", err)
	}
	defer func() { _ = f.Close() }() // read only
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > maxRequestSize {
		return "", errors.New("the request is not a small regular file")
	}
	b, err := io.ReadAll(io.LimitReader(f, maxRequestSize))
	if err != nil {
		return "", err
	}
	verb := strings.TrimSpace(string(b))
	switch verb {
	case HelperUpdate, HelperRestart, HelperRestore:
		return verb, nil
	}
	return "", errors.New("the request is not one the helper knows")
}

// Helper handles the request at requestPath: it removes the file first, so the path unit
// does not start the helper again, then does what the file asked for and records the result.
func Helper(ctx context.Context, env Env, o Options, requestPath string) error {
	verb, readErr := ReadRequest(requestPath)
	// Remove does not follow a link: a planted link is removed, not its target.
	if err := os.Remove(requestPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove the request: %w", err)
	}
	if readErr != nil {
		writeHelperStatus(env, o, HelperStatus{Action: "unknown", State: "failed", Message: readErr.Error(), At: env.Now()})
		return readErr
	}
	writeHelperStatus(env, o, HelperStatus{Action: verb, State: "running", At: env.Now()})
	err := runHelperAction(ctx, env, o, verb)
	st := HelperStatus{Action: verb, State: "done", At: env.Now()}
	if err != nil {
		st.State, st.Message = "failed", clip(err.Error(), maxStatusText)
		var rb *rolledBackError
		if errors.As(err, &rb) {
			st.State = HelperRolledBack
		}
	}
	writeHelperStatus(env, o, st)
	return err
}

func runHelperAction(ctx context.Context, env Env, o Options, verb string) error {
	s := newSession(env, o)
	s.p.yes = true
	switch verb {
	case HelperRestore:
		return s.restore(ctx)
	case HelperRestart:
		return s.runChecked(ctx, Command{Name: "systemctl", Args: []string{"restart", "netscribe"}})
	case HelperUpdate:
		rel, err := env.LatestRelease(ctx)
		if err != nil {
			return fmt.Errorf("look for the newest release: %w", err)
		}
		if !update.Newer(o.Version, rel.Tag) {
			return nil // the panel asked, but nothing newer exists any more
		}
		return s.updateWithRollback(ctx, rel)
	}
	return errors.New("unknown request")
}

// rolledBackError says an update was undone: the new program was installed, did not answer,
// and the old one is running again.
type rolledBackError struct{ reason string }

func (e *rolledBackError) Error() string { return e.reason }

// updateWithRollback installs the release, restarts the service and waits for the new
// version to answer on /healthz. If it does not answer in time, the old program, which was
// kept as netscribe.previous before anything was replaced, is put back and started again.
// A release that fails its checksum never gets that far: nothing is replaced then.
func (s *session) updateWithRollback(ctx context.Context, rel update.Release) error {
	prev := filepath.Join(filepath.Dir(s.o.BinPath), previousProgram)
	if err := s.env.CopyExecutable(s.o.BinPath, prev); err != nil {
		return fmt.Errorf("keep the old program as %s, so nothing was changed: %w", prev, err)
	}
	if err := s.installBinary(ctx, rel); err != nil {
		return err
	}
	restarted, err := s.activate(ctx, false)
	if err == nil && !restarted {
		return nil // the service was not running, so there is nothing to wait for
	}
	if err == nil {
		err = s.waitVersion(ctx, rel.Tag)
	}
	if err == nil {
		return nil
	}
	return s.putBackProgram(ctx, prev, rel.Tag, err)
}

// waitVersion waits until /healthz answers with version tag. Without a config to learn the
// address from, or without a way to read the version, the answer of the old kind is enough.
func (s *session) waitVersion(ctx context.Context, tag string) error {
	cfg, err := s.loadConfig()
	if err != nil {
		s.ui.note("the address of the panel is not known (%v): the new version cannot be checked", err)
		return nil
	}
	url := healthURL(*cfg)
	wait := s.o.RollbackWait
	if wait <= 0 {
		wait = defaultRollbackWait
	}
	s.ui.doing("wait for " + tag + " to answer at " + strings.TrimSuffix(url, "/healthz"))
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	tick := time.NewTicker(healthPoll)
	defer tick.Stop()
	var last error
	for {
		last = s.probe(ctx, url, tag)
		if last == nil {
			s.ui.done(tag + " answers")
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s did not answer in %d seconds: %w", tag, int(wait.Seconds()), last)
		case <-tick.C:
		}
	}
}

// probe asks /healthz once. It is an error while the answer is not from version tag.
func (s *session) probe(ctx context.Context, url, tag string) error {
	if s.env.HealthVersion == nil {
		return s.env.Health(ctx, url)
	}
	got, err := s.env.HealthVersion(ctx, url)
	if err != nil {
		return err
	}
	if got != tag {
		return fmt.Errorf("the panel answers as %q", got)
	}
	return nil
}

// putBackProgram puts the kept program back, starts it and reports the rollback.
func (s *session) putBackProgram(ctx context.Context, prev, tag string, cause error) error {
	s.ui.failed("the new version does not work, putting the old program back", cause)
	if err := s.env.CopyExecutable(prev, s.o.BinPath); err != nil {
		return fmt.Errorf("%s failed (%v) and the old program could not be put back: %w", tag, cause, err)
	}
	if err := s.runChecked(ctx, Command{Name: "systemctl", Args: []string{"restart", "netscribe"}}); err != nil {
		return fmt.Errorf("%s failed (%v), the old program is back but did not start: %w", tag, cause, err)
	}
	return &rolledBackError{reason: clip(fmt.Sprintf("%s did not start: %v; the old program is back", tag, cause), maxStatusText)}
}

func writeHelperStatus(env Env, o Options, st HelperStatus) {
	data, err := json.Marshal(st)
	if err != nil {
		return
	}
	// A status that cannot be written must not stop the request itself.
	_ = env.WriteFile(o.HelperStatus, data, 0o644, nil) //nolint:gosec // the status is for everybody to read
}
