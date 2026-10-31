package setup

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/FlexEbat/Netscribe/internal/backup"
	"github.com/FlexEbat/Netscribe/internal/config"
)

// HelperRestore asks for the backup staged by the panel to be put in place.
const HelperRestore = "restore"

// beforeRestore is added to the name of a file the restore replaces, so the old one is kept.
const beforeRestore = ".before-restore"

// defaultRestoreWait is how long the restored service gets to answer when no timeout is set.
const defaultRestoreWait = time.Minute

// StageDir is where the panel leaves a backup it has checked. The service account writes
// there, so the helper takes the files over before it reads them.
func StageDir(o Options) string { return filepath.Join(o.StateDir, "restore") }

// restore replaces the database and the config with the staged ones. The request carries
// no path: the stage is always StageDir. The old files stay as *.before-restore, and a
// restored service that does not answer is stopped and the old files go back.
func (s *session) restore(ctx context.Context) error {
	stage := StageDir(s.o)
	// The service account wrote the stage. Taking it over first means nothing in it can
	// change while the helper looks at it and copies from it.
	if err := s.runChecked(ctx, Command{Name: "chown", Args: []string{"-R", "-h", "root:root", stage}}); err != nil {
		return fmt.Errorf("take over the staged backup: %w", err)
	}
	for _, name := range []string{backup.NameDB, backup.NameConfig} {
		info, err := s.env.Lstat(filepath.Join(stage, name))
		if err != nil {
			return fmt.Errorf("the staged backup has no %s", name)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("the staged %s is not a regular file", name)
		}
	}
	staged, err := config.Load(filepath.Join(stage, backup.NameConfig), true)
	if err != nil {
		return fmt.Errorf("the config in the backup is not valid: %w", err)
	}
	cur, err := s.loadConfig()
	if err != nil {
		return fmt.Errorf("read the current config: %w", err)
	}
	if staged.Database != cur.Database {
		return fmt.Errorf("the config in the backup keeps the database at %s, this install at %s: change database in the backup config or in this one first", staged.Database, cur.Database)
	}
	db := cur.Database
	paths := []string{db, s.o.ConfigPath}

	if err := s.runChecked(ctx, Command{Name: "systemctl", Args: []string{"stop", "netscribe"}}); err != nil {
		return err
	}
	for _, p := range paths {
		if _, err := s.env.Stat(p); err == nil {
			if err := s.runChecked(ctx, Command{Name: "cp", Args: []string{"-p", "--", p, p + beforeRestore}}); err != nil {
				return fmt.Errorf("keep the old %s: %w", filepath.Base(p), err)
			}
		}
	}
	if err := s.putRestored(ctx, stage, db); err != nil {
		return s.rollBack(ctx, paths, db, err)
	}
	if err := s.runChecked(ctx, Command{Name: "systemctl", Args: []string{"start", "netscribe"}}); err != nil {
		return s.rollBack(ctx, paths, db, err)
	}
	s.cfg = &staged
	if s.o.HealthTimeout <= 0 {
		s.o.HealthTimeout = defaultRestoreWait
	}
	if err := s.waitHealthy(ctx); err != nil {
		return s.rollBack(ctx, paths, db, err)
	}
	// The stage held every secret of the backup: it does not stay.
	if err := s.runChecked(ctx, Command{Name: "rm", Args: []string{"-rf", "--", stage}}); err != nil {
		s.ui.note("the staged backup was not removed: %v", err)
	}
	return nil
}

// putRestored copies the staged database and config over the real ones. A file that exists
// keeps its owner and mode, because cp writes into it; a database that is new gets the
// service account as its owner and mode 0600.
func (s *session) putRestored(ctx context.Context, stage, db string) error {
	// The write ahead log and the shared memory file belong to the old database.
	if err := s.runChecked(ctx, Command{Name: "rm", Args: []string{"-f", "--", db + "-wal", db + "-shm"}}); err != nil {
		return err
	}
	copyFile := func(name, dst string) error {
		src := filepath.Join(stage, name)
		return s.runChecked(ctx, Command{Name: "cp", Args: []string{"--no-preserve=mode,ownership", "--", src, dst}})
	}
	if err := copyFile(backup.NameDB, db); err != nil {
		return err
	}
	if s.o.ServiceUser != "" {
		if err := s.runChecked(ctx, Command{Name: "chown", Args: []string{s.o.ServiceUser + ":", db}}); err != nil {
			return err
		}
	}
	if err := s.runChecked(ctx, Command{Name: "chmod", Args: []string{"0600", db}}); err != nil {
		return err
	}
	return copyFile(backup.NameConfig, s.o.ConfigPath)
}

// rollBack puts the old files back and starts the service on them. It returns the error
// that made the restore fail, with a sentence about what it did.
func (s *session) rollBack(ctx context.Context, paths []string, db string, cause error) error {
	s.ui.note("the restore failed, so the old data goes back: %v", cause)
	// A stop of a service that never started is not an error worth reporting.
	_ = s.runChecked(ctx, Command{Name: "systemctl", Args: []string{"stop", "netscribe"}})
	_ = s.runChecked(ctx, Command{Name: "rm", Args: []string{"-f", "--", db + "-wal", db + "-shm"}})
	var failed []error
	for _, p := range paths {
		if _, err := s.env.Stat(p + beforeRestore); err != nil {
			continue
		}
		if err := s.runChecked(ctx, Command{Name: "cp", Args: []string{"-p", "--", p + beforeRestore, p}}); err != nil {
			failed = append(failed, err)
		}
	}
	if err := s.runChecked(ctx, Command{Name: "systemctl", Args: []string{"start", "netscribe"}}); err != nil {
		failed = append(failed, err)
	}
	if len(failed) > 0 {
		return fmt.Errorf("restore failed (%v) and putting the old files back failed too: %w", cause, errors.Join(failed...))
	}
	return fmt.Errorf("restore failed and the old data was put back: %w", cause)
}

// Restore is "netscribe restore" after the backup has been staged: it does what the helper
// does for the panel, in the terminal of a person who is root.
func Restore(ctx context.Context, env Env, o Options) error {
	s := newSession(env, o)
	s.p.yes = true
	return s.restore(ctx)
}
