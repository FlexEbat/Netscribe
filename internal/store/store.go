// Package store owns the SQLite database: opening, migrations and queries.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

//go:embed all:migrations
var migrationsFS embed.FS

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

const memoryPath = ":memory:"

// Open opens the database at path, creates it when missing and applies migrations.
// The file is created with mode 0600 and its directory with mode 0700.
// A path of ":memory:" gives a private in-memory database.
func Open(path string) (*sql.DB, error) {
	dsn := memoryPath
	if path != memoryPath {
		if err := prepareFile(path); err != nil {
			return nil, err
		}
		dsn = fileDSN(path)
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	ctx := context.Background()
	if path == memoryPath {
		// Each connection to ":memory:" is a separate database, so allow exactly one
		// and set the pragmas on it directly.
		db.SetMaxOpenConns(1)
		for _, pragma := range []string{"PRAGMA foreign_keys = ON", "PRAGMA busy_timeout = 5000"} {
			if _, err := db.ExecContext(ctx, pragma); err != nil {
				_ = db.Close() // the pragma error is the one worth reporting
				return nil, fmt.Errorf("set %q: %w", pragma, err)
			}
		}
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close() // the ping error is the one worth reporting
		return nil, fmt.Errorf("open database: %w", err)
	}

	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		_ = db.Close() // the Sub error is the one worth reporting
		return nil, fmt.Errorf("open migrations: %w", err)
	}
	if err := migrate(ctx, db, sub); err != nil {
		_ = db.Close() // the migration error is the one worth reporting
		return nil, err
	}
	return db, nil
}

// prepareFile creates the data directory (0700) and the database file (0600).
// SQLite copies the mode of the main file to its -wal and -shm files.
func prepareFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // the path comes from the config file
	if err != nil {
		return fmt.Errorf("create database file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("create database file: %w", err)
	}
	return nil
}

// fileDSN builds a URI that sets the pragmas on every pooled connection.
func fileDSN(path string) string {
	// Escape the characters that would end the path inside a URI.
	escaped := strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(path)
	return "file:" + escaped +
		"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
}

// migrate applies the goose migrations found in fsys.
// Without any .sql file there is nothing to apply, and goose is not started.
func migrate(ctx context.Context, db *sql.DB, fsys fs.FS) error {
	files, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	if len(files) == 0 {
		return nil
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, fsys)
	if err != nil {
		return fmt.Errorf("prepare migrations: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
