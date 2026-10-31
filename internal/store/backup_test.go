package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
)

func TestLatestSchemaIsTheNewestMigration(t *testing.T) {
	if got := LatestSchema(); got < 14 {
		t.Fatalf("LatestSchema = %d", got)
	}
	s := newTestStore(t)
	got, err := s.Schema(context.Background())
	if err != nil || got != LatestSchema() {
		t.Fatalf("Schema = %d, %v; want %d", got, err, LatestSchema())
	}
}

func fileStoreWithSession(t *testing.T) (*Store, string) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "live.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	u, err := s.CreateUser(ctx, model.UserRecord{User: model.User{Username: "admin", Role: model.RoleAdmin}, PasswordHash: "x"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	err = s.CreateSession(ctx, SessionRecord{IDHash: "h", UserID: u.ID, CSRFToken: "c", CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}

func TestBackupCopiesAndPrepareRestoreEmptiesTheSessions(t *testing.T) {
	ctx := context.Background()
	s, _ := fileStoreWithSession(t)
	dst := filepath.Join(t.TempDir(), "copy.db")
	if err := s.BackupTo(ctx, dst); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dst)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("copy: %v %v", info, err)
	}
	if err := s.BackupTo(ctx, dst); err == nil {
		t.Fatal("a copy over an existing file was allowed")
	}
	got, err := PrepareRestore(ctx, dst)
	if err != nil {
		t.Fatal(err)
	}
	if got.Schema != LatestSchema() || got.Users != 1 || got.Devices != 0 {
		t.Fatalf("info = %+v", got)
	}
	// the live database keeps its session, the copy lost it
	var live int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions").Scan(&live); err != nil || live != 1 {
		t.Fatalf("live sessions = %d, %v", live, err)
	}
	copyStore, err := Open(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = copyStore.Close() }()
	var inCopy int
	if err := copyStore.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions").Scan(&inCopy); err != nil || inCopy != 0 {
		t.Fatalf("sessions in the copy = %d, %v", inCopy, err)
	}
}

func TestPrepareRestoreRefusesWhatIsNotARestorableDatabase(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	garbage := filepath.Join(dir, "garbage.db")
	if err := os.WriteFile(garbage, []byte("this is not sqlite at all, only text"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareRestore(ctx, garbage); err == nil {
		t.Error("a text file was accepted")
	}

	// a SQLite file that is not Netscribe's
	other, err := openDB(filepath.Join(dir, "other.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.ExecContext(ctx, "DROP TABLE goose_db_version"); err != nil {
		t.Fatal(err)
	}
	_ = other.Close()
	if _, err := PrepareRestore(ctx, filepath.Join(dir, "other.db")); err == nil {
		t.Error("a database without a schema number was accepted")
	}

	// a database from a newer program
	newer := filepath.Join(dir, "newer.db")
	db, err := openDB(newer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, 1)", LatestSchema()+1); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if _, err := PrepareRestore(ctx, newer); !errors.Is(err, ErrBackupNewer) {
		t.Errorf("a newer schema: err = %v", err)
	}
}
