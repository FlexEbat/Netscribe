package setup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/FlexEbat/Netscribe/internal/update"
)

// The helper is the one part of Netscribe that runs as root on behalf of the panel. The
// panel (a process without rights) leaves a request file; systemd starts the helper, which
// reads one word from it and does that one thing. Nothing else in the file is used, and the
// helper never takes a version, a path or a command from it: "update" installs the newest
// release of this repository after checking its checksum, "restart" restarts netscribe.

// HelperStatusPath is where the helper tells how the last request went. The directory
// belongs to root, so only the helper can write there, and everybody can read it.
const HelperStatusPath = "/run/netscribe-helper/status.json"

const (
	// HelperUpdate asks for the newest release to be installed.
	HelperUpdate = "update"
	// HelperRestart asks for the service to be restarted.
	HelperRestart = "restart"

	maxRequestSize = 64
	maxStatusText  = 300
)

// HelperStatus is what the helper wrote about a request.
type HelperStatus struct {
	Action  string    `json:"action"`
	State   string    `json:"state"` // running, done, failed
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
	case HelperUpdate, HelperRestart:
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
	}
	writeHelperStatus(env, o, st)
	return err
}

func runHelperAction(ctx context.Context, env Env, o Options, verb string) error {
	s := newSession(env, o)
	s.p.yes = true
	switch verb {
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
		return s.installRelease(ctx, rel, false)
	}
	return errors.New("unknown request")
}

func writeHelperStatus(env Env, o Options, st HelperStatus) {
	data, err := json.Marshal(st)
	if err != nil {
		return
	}
	// A status that cannot be written must not stop the request itself.
	_ = env.WriteFile(o.HelperStatus, data, 0o644, nil) //nolint:gosec // the status is for everybody to read
}
