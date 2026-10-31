package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
)

// LatestSchema is the number of the newest migration this program carries. A database whose
// schema is newer than that was written by a newer program, and this one cannot read it.
func LatestSchema() int {
	files, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return 0
	}
	latest := 0
	for _, f := range files {
		name := f[strings.LastIndex(f, "/")+1:]
		n, _, ok := strings.Cut(name, "_")
		if !ok {
			continue
		}
		if v, err := strconv.Atoi(n); err == nil && v > latest {
			latest = v
		}
	}
	return latest
}

// schemaQuery reads the newest applied migration. An empty or foreign database has none.
const schemaQuery = `SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version WHERE is_applied = 1`

// Schema returns the number of the newest migration applied to this database.
func (s *Store) Schema(ctx context.Context) (int, error) {
	var v int
	if err := s.db.QueryRowContext(ctx, schemaQuery).Scan(&v); err != nil {
		return 0, fmt.Errorf("read the schema number: %w", err)
	}
	return v, nil
}

// BackupTo writes a consistent copy of the database to path, which must not exist. The copy
// is made inside one read transaction, so a scan that writes meanwhile cannot tear it.
func (s *Store) BackupTo(ctx context.Context, path string) error {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("backup: %s exists", path)
	}
	if _, err := s.db.ExecContext(ctx, "VACUUM INTO ?", path); err != nil {
		return fmt.Errorf("copy the database: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("protect the copy: %w", err)
	}
	return nil
}

// BackupInfo says what a database in a backup holds.
type BackupInfo struct {
	Schema  int `json:"schema"`
	Devices int `json:"devices"`
	Users   int `json:"users"`
}

// ErrBackupNewer means the database was written by a newer program.
var ErrBackupNewer = errors.New("the backup was made by a newer Netscribe than this one")

// PrepareRestore checks the database file at path and makes it ready to be restored: it
// must pass SQLite's own integrity and foreign key checks, it must be a Netscribe database
// of this program's schema or an older one, and its sessions are emptied so everybody signs
// in again. The file is changed in place, so path must be a copy. Nothing is migrated: the
// service does that when it starts on the file.
func PrepareRestore(ctx context.Context, path string) (BackupInfo, error) {
	var info BackupInfo
	db, err := sql.Open("sqlite", "file:"+escapeURIPath(path)+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return info, fmt.Errorf("open the database in the backup: %w", err)
	}
	defer func() { _ = db.Close() }() // the checks already reported what matters
	db.SetMaxOpenConns(1)

	var verdict string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&verdict); err != nil || verdict != "ok" {
		return info, errors.New("the database in the backup is damaged")
	}
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return info, errors.New("the database in the backup is damaged")
	}
	broken := rows.Next()
	if err := rows.Close(); err != nil {
		return info, errors.New("the database in the backup is damaged")
	}
	if broken {
		return info, errors.New("the database in the backup breaks its own references")
	}
	if err := db.QueryRowContext(ctx, schemaQuery).Scan(&info.Schema); err != nil || info.Schema < 1 {
		return info, errors.New("the file is not a Netscribe database")
	}
	if info.Schema > LatestSchema() {
		return info, ErrBackupNewer
	}
	for _, c := range []struct {
		table string
		into  *int
	}{{"devices", &info.Devices}, {"users", &info.Users}} {
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+c.table).Scan(c.into); err != nil {
			return info, errors.New("the file is not a Netscribe database")
		}
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM sessions"); err != nil {
		return info, fmt.Errorf("end the sessions in the backup: %w", err)
	}
	return info, nil
}

// escapeURIPath escapes what would end the path inside a file: URI.
func escapeURIPath(path string) string {
	return strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(path)
}
