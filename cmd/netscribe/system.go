package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/FlexEbat/Netscribe/internal/api"
	"github.com/FlexEbat/Netscribe/internal/setup"
)

// staleRunning is how long a "running" status of the helper is believed.
const staleRunning = 20 * time.Minute

// releaseVersion is the running version without the build details: v0.2.0, or dev.
func releaseVersion() string {
	v, _ := buildVersion()
	return v
}

// baseOptions are the default paths of the install, with the running version.
func baseOptions() setup.Options {
	o := setup.DefaultOptions()
	o.Version = releaseVersion()
	return o
}

const updateUsage = `usage: netscribe update [--check] [--yes] [--config FILE]

Looks for a newer release, shows what is new and asks before it installs it.
  --check   only say whether a newer release exists
  --yes     do not ask
`

// runUpdate looks for a newer release and installs it. --units is the step the new program
// runs on itself after an update, to write the units that belong to its version.
func runUpdate(args []string, e streams) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(e.err)
	o := baseOptions()
	fs.StringVar(&o.ConfigPath, "config", o.ConfigPath, "path of the config file")
	check := fs.Bool("check", false, "only say whether a newer release exists")
	fs.BoolVar(&o.Yes, "yes", false, "do not ask before installing")
	units := fs.Bool("units", false, "write the systemd units of this version (run by the update itself)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		_, _ = fmt.Fprint(e.err, updateUsage)
		return errors.New("update takes no arguments")
	}
	if *units {
		return setup.RefreshUnits(context.Background(), setupEnv(e), o)
	}
	return setup.Update(context.Background(), setupEnv(e), o, *check)
}

// runHelper is what systemd runs, as root, when the panel leaves a request.
func runHelper(args []string, e streams) error {
	fs := flag.NewFlagSet("helper", flag.ContinueOnError)
	fs.SetOutput(e.err)
	o := baseOptions()
	request := fs.String("request", setup.RequestPath(o), "the request file the panel leaves")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("helper takes no arguments")
	}
	if os.Getuid() != 0 {
		return errors.New("the helper changes the system and needs root; the panel starts it through systemd")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	o.BinPath, o.StateDir = exe, filepath.Dir(*request)
	env := setupEnv(e)
	env.Interactive = false
	return setup.Helper(context.Background(), env, o, *request)
}

// helperClient is the panel's side of the helper: it leaves a request and reads the status.
type helperClient struct {
	stateDir string
	unitDir  string
	status   string
}

func newHelperClient(databasePath string) helperClient {
	o := setup.DefaultOptions()
	return helperClient{stateDir: filepath.Dir(databasePath), unitDir: o.UnitDir, status: o.HelperStatus}
}

func (h helperClient) request() string { return filepath.Join(h.stateDir, "helper.request") }

func (h helperClient) State() api.HelperState {
	st := api.HelperState{State: "idle"}
	if _, err := os.Stat(filepath.Join(h.unitDir, "netscribe-helper.path")); err != nil {
		return st // no helper installed: Docker, or an install from before it existed
	}
	st.Available = true
	if _, err := os.Lstat(h.request()); err == nil {
		st.State = "waiting"
		return st
	}
	data, err := os.ReadFile(h.status) //nolint:gosec // a fixed path the helper writes
	if err != nil {
		return st
	}
	var hs setup.HelperStatus
	if json.Unmarshal(data, &hs) != nil {
		return st
	}
	if hs.State == "running" && time.Since(hs.At) > staleRunning {
		return st
	}
	at := hs.At
	st.State, st.Action, st.Message, st.At = hs.State, hs.Action, hs.Message, &at
	return st
}

func (h helperClient) Request(verb string) error {
	if verb != setup.HelperUpdate && verb != setup.HelperRestart {
		return errors.New("unknown request")
	}
	if st := h.State(); st.State == "waiting" || st.State == "running" {
		return api.ErrHelperBusy
	}
	tmp := h.request() + ".tmp"
	_ = os.Remove(tmp)                                                   // a leftover of a crashed request
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // in the state directory of the service
	if err != nil {
		return fmt.Errorf("leave the request: %w", err)
	}
	if _, err := f.WriteString(verb + "\n"); err != nil {
		_ = f.Close() // the write error is the one to report
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// The rename makes the request appear whole, so systemd never starts the helper on half a file.
	if err := os.Rename(tmp, h.request()); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// settingsEditor serves the settings page from the same code as netscribe config.
type settingsEditor struct {
	env setup.Env
	o   setup.Options
}

func newSettingsEditor(configPath string) settingsEditor {
	o := baseOptions()
	o.ConfigPath = configPath
	return settingsEditor{env: setup.NewEnv(strings.NewReader(""), io.Discard, false, false), o: o}
}

func (s settingsEditor) Settings() ([]setup.PanelGroup, error) {
	return setup.PanelSettings(s.env, s.o)
}

func (s settingsEditor) Apply(changes map[string]json.RawMessage) (setup.PanelResult, error) {
	return setup.ApplyPanelSettings(s.env, s.o, changes)
}
