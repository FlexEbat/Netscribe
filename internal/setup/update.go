package setup

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/FlexEbat/Netscribe/internal/update"
)

const maxNotesLines = 24

// Update looks for a newer release and, after a question, installs it: the program, the
// units of the helper, and a restart. checkOnly only says what exists.
func Update(ctx context.Context, env Env, o Options, checkOnly bool) error {
	s := newSession(env, o)
	s.confirmEach = !o.Yes
	s.version = o.Version
	return s.updateFlow(ctx, checkOnly)
}

func (s *session) updateFlow(ctx context.Context, checkOnly bool) error {
	s.ui.doing("ask GitHub for the newest release")
	rel, err := s.env.LatestRelease(ctx)
	if err != nil {
		return fmt.Errorf("could not look for a new release: %w", err)
	}
	cur := s.o.Version
	s.ui.printf("  installed  %s\n  newest     %s\n", orDefault(cur, "unknown"), rel.Tag)
	if !update.Newer(cur, rel.Tag) {
		if _, err := update.ParseVersion(cur); err != nil {
			s.ui.note("this build (%s) is not a release, so it cannot tell whether it is outdated", orDefault(cur, "unknown"))
			s.ui.note("the newest release is %s: %s", rel.Tag, rel.URL)
			return nil
		}
		s.ui.done("netscribe is up to date")
		return nil
	}
	s.ui.printf("\n  What is new in %s:\n", rel.Tag)
	for _, line := range notesPreview(rel.Notes) {
		s.ui.printf("    %s\n", line)
	}
	s.ui.printf("\n  Release and changelog: %s\n\n", rel.URL)
	if checkOnly {
		s.ui.note("update with: sudo netscribe update")
		return nil
	}
	if err := s.needRoot(); err != nil {
		return err
	}
	if err := s.approve(fmt.Sprintf("Update netscribe from %s to %s? The panel is down for a moment.", orDefault(cur, "this build"), rel.Tag), true); err != nil {
		return err
	}
	return s.installRelease(ctx, rel, true)
}

// notesPreview is the first lines of a changelog, for a terminal.
func notesPreview(notes string) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(notes, "\r", ""), "\n") {
		l = strings.TrimRight(l, " \t")
		if l == "" && (len(out) == 0 || out[len(out)-1] == "") {
			continue
		}
		out = append(out, l)
		if len(out) == maxNotesLines {
			out = append(out, "…")
			break
		}
	}
	return out
}

// installRelease puts the release in place, refreshes the units with the new program, and
// restarts the service. wait asks for the panel to answer afterwards, which only a person at
// the console wants: the helper has no config to learn the address from.
func (s *session) installRelease(ctx context.Context, rel update.Release, wait bool) error {
	s.ui.doing("download " + update.Asset(rel.Tag, s.env.GOARCH) + " and check its checksum")
	if err := s.env.InstallRelease(ctx, rel.Tag, s.o.BinPath); err != nil {
		return err
	}
	s.ui.done("installed " + rel.Tag + " at " + s.o.BinPath)
	// The new program carries the new units, so it writes them.
	if hasSystemd(s.env) {
		args := []string{"update", "--units", "--config", s.o.ConfigPath}
		if err := s.runChecked(ctx, Command{Name: s.o.BinPath, Args: args}); err != nil {
			s.ui.note("the units were not refreshed: %v", err)
		}
		if s.serviceActive(ctx) {
			if err := s.runChecked(ctx, Command{Name: "systemctl", Args: []string{"restart", "netscribe"}}); err != nil {
				return err
			}
			s.ui.done("restarted the service")
			if wait {
				return s.waitHealthy(ctx)
			}
		}
	}
	return nil
}

// RefreshUnits writes the units of the helper and the drop-in that lets the panel save its
// settings, and starts the path unit. It is the step "netscribe update --units" runs.
func RefreshUnits(ctx context.Context, env Env, o Options) error {
	s := newSession(env, o)
	if !hasSystemd(env) {
		return errors.New("this host does not run systemd")
	}
	changed, err := s.installPanelUnits(ctx)
	if err != nil {
		return err
	}
	if changed {
		s.ui.done("the panel can now update and restart the service")
	}
	return nil
}

// installPanelUnits writes the helper units and the panel drop-in and makes systemd read them.
// It tells whether a file changed.
func (s *session) installPanelUnits(ctx context.Context) (bool, error) {
	service, err := RenderHelperService(s.o)
	if err != nil {
		return false, err
	}
	path, err := RenderHelperPath(s.o)
	if err != nil {
		return false, err
	}
	files := []struct{ path, text string }{
		{filepath.Join(s.o.UnitDir, "netscribe-helper.service"), service},
		{filepath.Join(s.o.UnitDir, "netscribe-helper.path"), path},
		{filepath.Join(s.o.UnitDir, "netscribe.service.d", "20-panel.conf"), RenderPanelDropIn(s.o)},
	}
	changed := false
	for _, f := range files {
		old, _ := s.env.ReadFile(f.path)
		if string(old) == f.text {
			continue
		}
		changed = true
		if err := s.env.MkdirAll(filepath.Dir(f.path), 0o755, nil); err != nil { //nolint:gosec // systemd directories are readable by everyone
			return false, err
		}
		if err := s.env.WriteFile(f.path, []byte(f.text), 0o644, nil); err != nil { //nolint:gosec // a unit file is readable by everyone
			return false, err
		}
		s.ui.done("wrote " + f.path)
	}
	if !changed {
		return false, nil
	}
	if err := s.runChecked(ctx, Command{Name: "systemctl", Args: []string{"daemon-reload"}}); err != nil {
		return true, err
	}
	if err := s.runChecked(ctx, Command{Name: "systemctl", Args: []string{"enable", "--now", "netscribe-helper.path"}}); err != nil {
		return true, err
	}
	// The drop-in changes what the running service may write, which a restart applies.
	if s.serviceActive(ctx) {
		s.ui.note("the running service picks up the new rights at its next restart")
	}
	return true, nil
}
