package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/FlexEbat/Netscribe/internal/model"
	dbgen "github.com/FlexEbat/Netscribe/internal/store/db"
)

// CreateDevice stores a device a person added. A MAC or an IP that another record
// already uses is ErrConflict. A kind given by the person is locked against scans.
func (s *Store) CreateDevice(ctx context.Context, in model.ManualDevice, userID int64) (model.Device, error) {
	kind := in.Kind
	locked := kind != "" && kind != model.KindUnknown
	if kind == "" {
		kind = model.KindUnknown
	}
	mac, ip := strings.ToLower(strings.TrimSpace(in.MAC)), strings.TrimSpace(in.IP)
	if mac == "" && ip != "" {
		// An address that a discovered device already uses is that device, not a new one.
		if _, err := s.q.GetDeviceByIP(ctx, ip); err == nil {
			return model.Device{}, ErrConflict
		} else if !errors.Is(err, sql.ErrNoRows) {
			return model.Device{}, fmt.Errorf("create device: %w", err)
		}
	}
	ts := formatTime(s.now())
	row, err := s.q.InsertManualDevice(ctx, dbgen.InsertManualDeviceParams{
		Mac:         mac,
		Ip:          ip,
		Hostname:    in.Hostname,
		Kind:        string(kind),
		Label:       in.Label,
		Notes:       in.Notes,
		KindLocked:  boolToInt(locked),
		UpdatedBy:   sql.NullInt64{Int64: userID, Valid: userID > 0},
		FirstSeenAt: ts,
		LastSeenAt:  ts,
	})
	if isUniqueViolation(err) {
		return model.Device{}, ErrConflict
	}
	if err != nil {
		return model.Device{}, fmt.Errorf("create device: %w", err)
	}
	return toDevice(row)
}

// UpdateDevice applies an edit if the editor saw the current version. A stale version is
// ErrVersionConflict and changes nothing.
func (s *Store) UpdateDevice(ctx context.Context, id int64, e model.DeviceEdit, userID int64) (model.Device, error) {
	cur, err := s.GetDevice(ctx, id)
	if err != nil {
		return model.Device{}, err
	}
	label, notes, kind, locked := cur.Label, cur.Notes, cur.Kind, cur.KindLocked
	if e.Label != nil {
		label = *e.Label
	}
	if e.Notes != nil {
		notes = *e.Notes
	}
	if e.Kind != nil {
		if *e.Kind == "" {
			locked = false
		} else {
			kind, locked = *e.Kind, true
		}
	}
	n, err := s.q.UpdateDeviceEdit(ctx, dbgen.UpdateDeviceEditParams{
		Label:      label,
		Notes:      notes,
		Kind:       string(kind),
		KindLocked: boolToInt(locked),
		UpdatedBy:  sql.NullInt64{Int64: userID, Valid: userID > 0},
		ID:         id,
		Version:    e.Version,
	})
	if err != nil {
		return model.Device{}, fmt.Errorf("update device: %w", err)
	}
	if n == 0 {
		// The row is there (it was read above), so the version is what did not match.
		if _, err := s.GetDevice(ctx, id); err != nil {
			return model.Device{}, err
		}
		return model.Device{}, ErrVersionConflict
	}
	return s.GetDevice(ctx, id)
}

// DeleteDevice removes a device. Its links go with it.
func (s *Store) DeleteDevice(ctx context.Context, id int64) error {
	n, err := s.q.DeleteDevice(ctx, id)
	if err != nil {
		return fmt.Errorf("delete device: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateLink stores a link drawn by a person. Both devices must exist. The pair is kept
// in id order, so the same link drawn from either end is one link (ErrConflict).
func (s *Store) CreateLink(ctx context.Context, in model.ManualLink) (model.Link, error) {
	a, b := in.ADeviceID, in.BDeviceID
	aPort, bPort := in.APort, in.BPort
	if a > b {
		a, b = b, a
		aPort, bPort = bPort, aPort
	}
	for _, id := range []int64{a, b} {
		if _, err := s.q.GetDeviceByID(ctx, id); errors.Is(err, sql.ErrNoRows) {
			return model.Link{}, ErrNotFound
		} else if err != nil {
			return model.Link{}, fmt.Errorf("create link: %w", err)
		}
	}
	row, err := s.q.InsertLink(ctx, dbgen.InsertLinkParams{
		ADeviceID: a, APort: aPort, BDeviceID: b, BPort: bPort,
		Source: "manual", LastSeenAt: formatTime(s.now()),
	})
	if isUniqueViolation(err) {
		return model.Link{}, ErrConflict
	}
	if err != nil {
		return model.Link{}, fmt.Errorf("create link: %w", err)
	}
	return toLink(row)
}

// DeleteLink removes a link drawn by a person. A link found by a scan is ErrNotManual.
func (s *Store) DeleteLink(ctx context.Context, id int64) error {
	row, err := s.q.GetLink(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("delete link: %w", err)
	}
	if row.Source != "manual" {
		return ErrNotManual
	}
	if _, err := s.q.DeleteLink(ctx, id); err != nil {
		return fmt.Errorf("delete link: %w", err)
	}
	return nil
}

// ListLinks returns all links, never nil.
func (s *Store) ListLinks(ctx context.Context) ([]model.Link, error) {
	rows, err := s.q.ListLinks(ctx)
	if err != nil {
		return nil, fmt.Errorf("list links: %w", err)
	}
	out := make([]model.Link, 0, len(rows))
	for _, r := range rows {
		l, err := toLink(r)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}

func toLink(r dbgen.Link) (model.Link, error) {
	seen, err := parseTime(r.LastSeenAt)
	if err != nil {
		return model.Link{}, fmt.Errorf("link %d: last_seen_at: %w", r.ID, err)
	}
	return model.Link{
		ID: r.ID, ADeviceID: r.ADeviceID, APort: r.APort, BDeviceID: r.BDeviceID, BPort: r.BPort,
		Source: r.Source, LastSeenAt: seen,
	}, nil
}
