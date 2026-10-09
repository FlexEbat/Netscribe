// Package store owns the SQLite database: opening, migrations and queries.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // registers the "sqlite" driver

	"github.com/FlexEbat/Netscribe/internal/auth"
	"github.com/FlexEbat/Netscribe/internal/collector"
	"github.com/FlexEbat/Netscribe/internal/model"
	dbgen "github.com/FlexEbat/Netscribe/internal/store/db"
)

//go:embed all:migrations
var migrationsFS embed.FS

// Errors of the store. They are shared with auth and api through the model package.
var (
	ErrNotFound  = model.ErrNotFound
	ErrConflict  = model.ErrConflict  // a uniqueness rule was violated
	ErrLastAdmin = model.ErrLastAdmin // the change would leave no enabled administrator
)

// Repo is the data access contract. It grows by the methods each slice implements.
type Repo interface {
	ListDevices(ctx context.Context, f DeviceFilter) ([]model.Device, error)

	StartScan(ctx context.Context, target string) (model.Scan, error)
	FinishScan(ctx context.Context, id int64, status, errMsg string) error
	GetScan(ctx context.Context, id int64) (model.Scan, error) // ErrNotFound

	ApplyResult(ctx context.Context, scanID int64, r collector.Result) error
}

// AuthRepo holds users, sessions and the audit log. Access tokens join it in slice 6.
type AuthRepo interface {
	CountUsers(ctx context.Context) (int, error)
	CreateUser(ctx context.Context, u model.UserRecord) (model.User, error)       // ErrConflict
	GetUserByName(ctx context.Context, username string) (model.UserRecord, error) // ErrNotFound
	GetUser(ctx context.Context, id int64) (model.User, error)                    // ErrNotFound
	ListUsers(ctx context.Context) ([]model.User, error)
	UpdateUser(ctx context.Context, id int64, role *model.Role, disabled *bool) (model.User, error) // ErrNotFound, ErrLastAdmin
	SetPassword(ctx context.Context, id int64, hash string, mustChange bool) error                  // revokes the user's sessions
	RecordLogin(ctx context.Context, id int64, ok bool, lockAfter int, lockFor time.Duration) error
	DeleteUser(ctx context.Context, id int64) error // ErrNotFound, ErrLastAdmin

	CreateSession(ctx context.Context, s SessionRecord) error
	GetSession(ctx context.Context, idHash string) (SessionRecord, model.User, error) // ErrNotFound
	TouchSession(ctx context.Context, idHash string, now time.Time) error
	DeleteSession(ctx context.Context, idHash string) error
	DeleteUserSessions(ctx context.Context, userID int64) error
	PruneSessions(ctx context.Context, now time.Time) error

	AddAudit(ctx context.Context, e model.AuditEntry) error
	ListAudit(ctx context.Context, f AuditFilter) ([]model.AuditEntry, error)
	PruneAudit(ctx context.Context, keepDays int) error
}

// SessionRecord is declared in auth, which cannot import store. This alias keeps the
// name the data layer uses.
type SessionRecord = auth.SessionRecord

// AuditFilter narrows ListAudit. Zero values mean no restriction.
type AuditFilter struct {
	Limit    int   // 1 to 500, default 50
	BeforeID int64 // only entries with a smaller id, for paging
	Action   string
	Username string
}

var (
	_ Repo      = (*Store)(nil)
	_ AuthRepo  = (*Store)(nil)
	_ auth.Repo = (*Store)(nil)
)

// DeviceFilter narrows ListDevices. The zero value matches every device.
type DeviceFilter struct {
	Online *bool
	Kind   model.DeviceKind
	Query  string
}

// Store is the SQLite implementation of Repo.
type Store struct {
	db  *sql.DB
	q   *dbgen.Queries
	now func() time.Time
}

// SetClock replaces the time source. Tests use it to move time without waiting.
func (s *Store) SetClock(now func() time.Time) { s.now = now }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

const memoryPath = ":memory:"

// Open opens the database at path, creates it when missing and applies migrations.
// The file is created with mode 0600 and its directory with mode 0700.
// A path of ":memory:" gives a private in-memory database.
func Open(path string) (*Store, error) {
	db, err := openDB(path)
	if err != nil {
		return nil, err
	}
	return &Store{db: db, q: dbgen.New(db), now: time.Now}, nil
}

func openDB(path string) (*sql.DB, error) {
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
		"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate"
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
