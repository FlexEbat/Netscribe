package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/FlexEbat/Netscribe/internal/model"
	dbgen "github.com/FlexEbat/Netscribe/internal/store/db"
)

// BulkChange is what a bulk edit sets. A nil field is left as it is.
type BulkChange struct {
	Trusted *bool
	Kind    *model.DeviceKind // a non-empty value locks the kind, as a single edit does
	GroupID *int64            // 0 removes the group
}

// BulkEdit changes every listed device in one transaction. ids must hold distinct ids.
// A device that does not exist is ErrNotFound and nothing is changed; a group that does not
// exist is ErrNoSuchGroup. Each device's version rises by one. It returns how many devices
// it changed.
func (s *Store) BulkEdit(ctx context.Context, ids []int64, c BulkChange, userID int64) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("bulk edit: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := s.q.WithTx(tx)

	var group sql.NullInt64
	if c.GroupID != nil && *c.GroupID != 0 {
		if _, err := q.GetGroup(ctx, *c.GroupID); errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNoSuchGroup
		} else if err != nil {
			return 0, fmt.Errorf("bulk edit: %w", err)
		}
		group = sql.NullInt64{Int64: *c.GroupID, Valid: true}
	}

	for _, id := range ids {
		cur, err := q.GetDeviceByID(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		if err != nil {
			return 0, fmt.Errorf("bulk edit: %w", err)
		}
		trusted, kind, locked, groupID := cur.Trusted, cur.Kind, cur.KindLocked, cur.GroupID
		if c.Trusted != nil {
			trusted = boolToInt(*c.Trusted)
		}
		if c.Kind != nil {
			if *c.Kind == "" {
				locked = 0
			} else {
				kind, locked = string(*c.Kind), 1
			}
		}
		if c.GroupID != nil {
			groupID = group
		}
		n, err := q.UpdateDeviceEdit(ctx, dbgen.UpdateDeviceEditParams{
			Label: cur.Label, Notes: cur.Notes, Kind: kind, KindLocked: locked, Trusted: trusted, GroupID: groupID,
			UpdatedBy: sql.NullInt64{Int64: userID, Valid: userID > 0}, ID: id, Version: cur.Version,
		})
		if err != nil {
			return 0, fmt.Errorf("bulk edit: %w", err)
		}
		if n == 0 {
			// Someone else changed the device between the read and the write.
			return 0, ErrVersionConflict
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("bulk edit: %w", err)
	}
	return len(ids), nil
}

// ImportItem is one device of an import file, already checked.
type ImportItem struct {
	Line    int
	Device  model.ManualDevice
	GroupID *int64
}

// ImportConflict says which line of the file collides with a record that exists, or with an
// earlier line. It wraps ErrConflict.
type ImportConflict struct{ Line int }

func (e *ImportConflict) Error() string {
	return fmt.Sprintf("line %d: the MAC or address is already used", e.Line)
}
func (e *ImportConflict) Unwrap() error { return ErrConflict }

// ImportDevices creates the devices in one transaction, all or none. With dryRun the
// transaction is rolled back at the end, so the answer says what would happen and nothing
// stays. A line that collides is an *ImportConflict. It returns how many devices it created
// (or would have created).
func (s *Store) ImportDevices(ctx context.Context, items []ImportItem, dryRun bool, userID int64) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("import devices: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := s.q.WithTx(tx)
	for _, it := range items {
		if _, err := s.createManual(ctx, q, it.Device, it.GroupID, userID); errors.Is(err, ErrConflict) {
			return 0, &ImportConflict{Line: it.Line}
		} else if err != nil {
			return 0, err
		}
	}
	if dryRun {
		return len(items), nil
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("import devices: %w", err)
	}
	return len(items), nil
}
