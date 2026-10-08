package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func scanString(t *testing.T, row *sql.Row) string {
	t.Helper()
	var v string
	if err := row.Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestOpenMemoryGivesWorkingDatabase(t *testing.T) {
	ctx := context.Background()
	db, err := openDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// The statements below only see each other if the pool keeps a single connection.
	for _, q := range []string{
		"CREATE TABLE probe (id INTEGER PRIMARY KEY, name TEXT NOT NULL)",
		"INSERT INTO probe (name) VALUES ('a'), ('b')",
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if got := scanString(t, db.QueryRowContext(ctx, "SELECT count(*) FROM probe")); got != "2" {
		t.Errorf("row count = %s, want 2", got)
	}
}

func TestOpenMemoryDatabasesAreIndependent(t *testing.T) {
	ctx := context.Background()
	a, err := openDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	b, err := openDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })

	if _, err := a.ExecContext(ctx, "CREATE TABLE only_in_a (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ExecContext(ctx, "SELECT * FROM only_in_a"); err == nil {
		t.Error("a second :memory: database sees a table of the first one")
	}
}

func TestOpenMemoryLeavesNoFileOnDisk(t *testing.T) {
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	db, err := openDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("openDB(\":memory:\") left files behind: %v", entries)
	}
}

func TestOpenMemorySetsPragmas(t *testing.T) {
	db, err := openDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if got := scanString(t, db.QueryRowContext(ctx, "PRAGMA foreign_keys")); got != "1" {
		t.Errorf("foreign_keys = %s, want 1", got)
	}
	if got := scanString(t, db.QueryRowContext(ctx, "PRAGMA busy_timeout")); got != "5000" {
		t.Errorf("busy_timeout = %s, want 5000", got)
	}
}

func TestOpenFileCreatesPrivateFilesWithPragmasOnEveryConnection(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	path := filepath.Join(dir, "netscribe.db")

	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("data directory: err=%v mode=%v, want 0700", err, info)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("database file: err=%v mode=%v, want 0600", err, info)
	}

	// Two connections held at once force the pool to open a second one.
	c1, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c1.Close() }()
	c2, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c2.Close() }()
	for i, c := range []*sql.Conn{c1, c2} {
		if got := scanString(t, c.QueryRowContext(ctx, "PRAGMA foreign_keys")); got != "1" {
			t.Errorf("connection %d: foreign_keys = %s, want 1", i+1, got)
		}
		if got := scanString(t, c.QueryRowContext(ctx, "PRAGMA journal_mode")); got != "wal" {
			t.Errorf("connection %d: journal_mode = %s, want wal", i+1, got)
		}
		if got := scanString(t, c.QueryRowContext(ctx, "PRAGMA busy_timeout")); got != "5000" {
			t.Errorf("connection %d: busy_timeout = %s, want 5000", i+1, got)
		}
	}
}

func TestOpenFileKeepsDataAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "netscribe.db")

	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "CREATE TABLE probe (v TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO probe VALUES ('kept')"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if got := scanString(t, db.QueryRowContext(ctx, "SELECT v FROM probe")); got != "kept" {
		t.Errorf("v = %q, want kept", got)
	}
}

func TestOpenFileWithSpecialCharactersInPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a b?c#d%e.db")
	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := os.Stat(path); err != nil {
		t.Errorf("database was not created at the requested path: %v", err)
	}
}

func TestOpenFailsWhenDirectoryCannotBeCreated(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := openDB(filepath.Join(blocker, "sub", "netscribe.db"))
	if err == nil {
		_ = db.Close()
		t.Fatal("openDB() succeeded below a regular file")
	}
}

func TestMigrateAppliesFilesOnce(t *testing.T) {
	ctx := context.Background()
	db, err := openDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	fsys := fstest.MapFS{
		"0001_probe.sql": {Data: []byte("-- +goose Up\nCREATE TABLE probe (id INTEGER PRIMARY KEY);\n-- +goose Down\nDROP TABLE probe;\n")},
	}
	for i := 0; i < 2; i++ { // the second run must be a no-op, not a "table exists" error
		if err := migrate(ctx, db, fsys); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO probe DEFAULT VALUES"); err != nil {
		t.Errorf("migration did not create the table: %v", err)
	}
}

func TestMigrateWithoutFilesIsANoOp(t *testing.T) {
	db, err := openDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrate(context.Background(), db, fstest.MapFS{}); err != nil {
		t.Fatalf("migrate on an empty directory: %v", err)
	}
}

func TestMigrateReportsBrokenSQL(t *testing.T) {
	db, err := openDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	fsys := fstest.MapFS{
		"0001_broken.sql": {Data: []byte("-- +goose Up\nCREATE TABEL nope (id INTEGER);\n-- +goose Down\nSELECT 1;\n")},
	}
	if err := migrate(context.Background(), db, fsys); err == nil {
		t.Fatal("migrate() accepted invalid SQL")
	}
}

func TestErrNotFoundIsComparable(t *testing.T) {
	wrapped := errors.Join(errors.New("device 7"), ErrNotFound)
	if !errors.Is(wrapped, ErrNotFound) {
		t.Error("errors.Is does not see ErrNotFound inside a joined error")
	}
}
