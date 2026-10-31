package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/FlexEbat/Netscribe/internal/api"
	"github.com/FlexEbat/Netscribe/internal/backup"
	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/setup"
	"github.com/FlexEbat/Netscribe/internal/store"
)

// backupService makes backups of this install and checks the ones people hand back. It is
// what the panel and the two commands share, so the checks are the same everywhere.
type backupService struct {
	db         *store.Store
	configPath string
	dbPath     string
	version    string
	now        func() time.Time
}

func newBackupService(db *store.Store, configPath, databasePath string) *backupService {
	return &backupService{db: db, configPath: configPath, dbPath: databasePath, version: releaseVersion(), now: time.Now}
}

func (b *backupService) stateDir() string { return filepath.Dir(b.dbPath) }

// tempArchive is an archive in a file that is removed when it is closed.
type tempArchive struct {
	*os.File
	dir string
}

func (t *tempArchive) Close() error {
	err := t.File.Close()
	if rerr := os.RemoveAll(t.dir); err == nil {
		err = rerr
	}
	return err
}

// Create writes the archive to a temporary file in the state directory and returns it, so
// a failure shows before the first byte goes out.
func (b *backupService) Create(ctx context.Context) (io.ReadCloser, int64, error) {
	dir, err := os.MkdirTemp(b.stateDir(), "backup-")
	if err != nil {
		return nil, 0, fmt.Errorf("make a place for the backup: %w", err)
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(dir) // a half made backup must not stay on disk
		}
	}()
	dbCopy := filepath.Join(dir, backup.NameDB)
	if err := b.db.BackupTo(ctx, dbCopy); err != nil {
		return nil, 0, err
	}
	schema, err := b.db.Schema(ctx)
	if err != nil {
		return nil, 0, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "archive.tar.gz"), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // inside the directory just made
	if err != nil {
		return nil, 0, err
	}
	m := backup.Manifest{Version: b.version, Schema: schema, CreatedAt: b.now().UTC().Truncate(time.Second)}
	files := []backup.File{{Name: backup.NameDB, Path: dbCopy}, {Name: backup.NameConfig, Path: b.configPath}}
	if err := backup.Write(f, m, files); err != nil {
		_ = f.Close() // the write error is the one worth reporting
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close() // the stat error is the one worth reporting
		return nil, 0, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		_ = f.Close() // the seek error is the one worth reporting
		return nil, 0, err
	}
	ok = true
	return &tempArchive{File: f, dir: dir}, info.Size(), nil
}

// Stage reads an archive into the stage directory and checks everything that can be checked
// without root: the archive itself, the database inside it, and the config. What it accepts
// is left for the helper; what it refuses is removed.
func (b *backupService) Stage(ctx context.Context, r io.Reader) (api.BackupSummary, error) {
	stage := setup.StageDir(setup.Options{StateDir: b.stateDir()})
	_ = os.RemoveAll(stage) // an earlier stage that nobody applied
	sum, err := b.checkedStage(ctx, r, stage)
	if err != nil {
		_ = os.RemoveAll(stage) // only a stage that passed stays
		return api.BackupSummary{}, err
	}
	return sum, nil
}

func (b *backupService) checkedStage(ctx context.Context, r io.Reader, stage string) (api.BackupSummary, error) {
	m, err := backup.Stage(r, stage)
	var ae *backup.ArchiveError
	if errors.As(err, &ae) {
		return api.BackupSummary{}, api.BackupRefusal(ae.Msg)
	}
	if err != nil {
		return api.BackupSummary{}, err
	}
	if m.Schema > store.LatestSchema() {
		return api.BackupSummary{}, api.BackupRefusal(store.ErrBackupNewer.Error())
	}
	info, err := store.PrepareRestore(ctx, filepath.Join(stage, backup.NameDB))
	if err != nil {
		return api.BackupSummary{}, api.BackupRefusal(err.Error())
	}
	staged, err := config.Load(filepath.Join(stage, backup.NameConfig), true)
	if err != nil {
		return api.BackupSummary{}, api.BackupRefusal("the config in the backup is not valid: " + err.Error())
	}
	if staged.Database != b.dbPath {
		return api.BackupSummary{}, api.BackupRefusal(fmt.Sprintf(
			"the config in the backup keeps the database at %s and this install at %s: change database in one of them first", staged.Database, b.dbPath))
	}
	return api.BackupSummary{Version: m.Version, CreatedAt: m.CreatedAt, Schema: info.Schema, Devices: info.Devices, Users: info.Users}, nil
}

const backupUsage = `usage: netscribe backup --out FILE [--config FILE]

Writes the database and the config, with its secrets, to one archive. Keep the file as you
keep the config. It needs the right to read the database, so run it with sudo.
`

// runBackup is "netscribe backup --out FILE".
func runBackup(args []string, e streams) error {
	flags := newCommonFlags("backup", e.err)
	out := flags.fs.String("out", "", "the archive to write (it must not exist)")
	cfg, _, err := flags.load(args, e.err)
	if err != nil {
		return err
	}
	if *out == "" || len(flags.args) > 0 {
		_, _ = fmt.Fprint(e.err, backupUsage)
		return errors.New("backup needs --out FILE and no other argument")
	}
	st, err := store.Open(cfg.Database)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }() // the archive is already written
	rc, _, err := newBackupService(st, *flags.configPath, cfg.Database).Create(context.Background())
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()                                       // read only
	dst, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // the person named the file
	if err != nil {
		return fmt.Errorf("write the backup: %w", err)
	}
	if _, err := io.Copy(dst, rc); err != nil {
		_ = dst.Close() // the copy error is the one worth reporting
		_ = os.Remove(*out)
		return fmt.Errorf("write the backup: %w", err)
	}
	if err := dst.Close(); err != nil {
		return fmt.Errorf("write the backup: %w", err)
	}
	_, _ = fmt.Fprintf(e.out, "wrote %s (mode 0600). It holds the config with its secrets: keep it safe.\n", *out)
	return nil
}

const restoreUsage = `usage: netscribe restore FILE [--yes] [--config FILE]

Replaces the database and the config with the ones in a backup. The service stops, the old
files stay next to the new ones as *.before-restore, the service starts again, and everybody
signs in again. Run it as root.
  --yes   do not ask first
`

// runRestore is "netscribe restore FILE". It stages the archive the way the panel does and
// then does what the helper does, as root.
func runRestore(args []string, e streams) error {
	flags := newCommonFlags("restore", e.err)
	yes := flags.fs.Bool("yes", false, "do not ask before replacing the data")
	cfg, _, err := flags.load(args, e.err)
	if err != nil {
		return err
	}
	if len(flags.args) != 1 {
		_, _ = fmt.Fprint(e.err, restoreUsage)
		return errors.New("restore needs the backup file")
	}
	if os.Getuid() != 0 {
		return errors.New("restore replaces the data of the service and needs root")
	}
	f, err := os.Open(flags.args[0])
	if err != nil {
		return fmt.Errorf("open the backup: %w", err)
	}
	defer func() { _ = f.Close() }() // read only
	// The database is not opened here: the checks work on the staged copy.
	b := &backupService{configPath: *flags.configPath, dbPath: cfg.Database, version: releaseVersion(), now: time.Now}
	sum, err := b.Stage(context.Background(), f)
	if err != nil {
		var refusal api.BackupRefusal
		if errors.As(err, &refusal) {
			return fmt.Errorf("the backup cannot be restored: %w", err)
		}
		return err
	}
	_, _ = fmt.Fprintf(e.out, "backup of %s (Netscribe %s): %d devices, %d users\n", sum.CreatedAt.Format("2006-01-02 15:04 UTC"), sum.Version, sum.Devices, sum.Users)
	if !*yes {
		_, _ = fmt.Fprint(e.out, "This replaces the data of this install. Type yes to go on: ")
		line, _ := bufio.NewReader(e.in).ReadString('\n')
		if strings.TrimSpace(line) != "yes" {
			_ = os.RemoveAll(setup.StageDir(setup.Options{StateDir: filepath.Dir(cfg.Database)}))
			return errors.New("nothing was changed")
		}
	}
	o := baseOptions()
	o.ConfigPath, o.StateDir = *flags.configPath, filepath.Dir(cfg.Database)
	return setup.Restore(context.Background(), setupEnv(e), o)
}
