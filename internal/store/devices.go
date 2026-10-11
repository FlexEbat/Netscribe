package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
	dbgen "github.com/FlexEbat/Netscribe/internal/store/db"
)

// timeLayout is the fixed-width UTC format of every timestamp column.
// A fixed width keeps text order equal to time order.
const timeLayout = "2006-01-02T15:04:05.000Z"

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTime(s string) (time.Time, error) { return time.Parse(timeLayout, s) }

// ListDevices returns the devices that match f, ordered by the numeric value of the IP.
// Substring matching and ordering happen in Go: SQLite's LIKE folds case for ASCII only.
func (s *Store) ListDevices(ctx context.Context, f DeviceFilter) ([]model.Device, error) {
	rows, err := s.q.ListDevices(ctx)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	query := strings.ToLower(strings.TrimSpace(f.Query))
	out := make([]model.Device, 0, len(rows))
	for _, r := range rows {
		d, err := toDevice(r)
		if err != nil {
			return nil, err
		}
		if f.Online != nil && d.Online != *f.Online {
			continue
		}
		if f.Kind != "" && d.Kind != f.Kind {
			continue
		}
		if query != "" && !matches(d, query) {
			continue
		}
		out = append(out, d)
	}
	slices.SortFunc(out, compareByIP)
	return out, nil
}

// GetDevice returns one device, or ErrNotFound.
func (s *Store) GetDevice(ctx context.Context, id int64) (model.Device, error) {
	row, err := s.q.GetDeviceByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Device{}, ErrNotFound
	}
	if err != nil {
		return model.Device{}, fmt.Errorf("get device: %w", err)
	}
	return toDevice(row)
}

func matches(d model.Device, lowerQuery string) bool {
	for _, field := range []string{d.IP, d.MAC, d.Hostname, d.Vendor} {
		if strings.Contains(strings.ToLower(field), lowerQuery) {
			return true
		}
	}
	return false
}

// compareByIP orders valid addresses numerically, then devices without one, then by id.
func compareByIP(a, b model.Device) int {
	ai, aerr := netip.ParseAddr(a.IP)
	bi, berr := netip.ParseAddr(b.IP)
	switch {
	case aerr == nil && berr == nil:
		if c := ai.Compare(bi); c != 0 {
			return c
		}
	case aerr == nil:
		return -1
	case berr == nil:
		return 1
	}
	switch {
	case a.ID < b.ID:
		return -1
	case a.ID > b.ID:
		return 1
	}
	return 0
}

func toDevice(r dbgen.Device) (model.Device, error) {
	first, err := parseTime(r.FirstSeenAt)
	if err != nil {
		return model.Device{}, fmt.Errorf("device %d: first_seen_at: %w", r.ID, err)
	}
	last, err := parseTime(r.LastSeenAt)
	if err != nil {
		return model.Device{}, fmt.Errorf("device %d: last_seen_at: %w", r.ID, err)
	}
	d := model.Device{
		ID:          r.ID,
		MAC:         r.Mac,
		IP:          r.Ip,
		Hostname:    r.Hostname,
		Vendor:      r.Vendor,
		Kind:        model.DeviceKind(r.Kind),
		Description: r.Description,
		Source:      r.Source,
		Online:      r.Online != 0,
		FirstSeenAt: first,
		LastSeenAt:  last,
	}
	if r.X.Valid {
		d.X = &r.X.Float64
	}
	if r.Y.Valid {
		d.Y = &r.Y.Float64
	}
	return d, nil
}

// StartScan records a new running scan.
func (s *Store) StartScan(ctx context.Context, target string) (model.Scan, error) {
	row, err := s.q.InsertScan(ctx, dbgen.InsertScanParams{Target: target, StartedAt: formatTime(s.now())})
	if err != nil {
		return model.Scan{}, fmt.Errorf("start scan: %w", err)
	}
	return toScan(row)
}

// FinishScan closes a running scan as done or failed.
func (s *Store) FinishScan(ctx context.Context, id int64, status, errMsg string) error {
	if status != "done" && status != "failed" {
		return fmt.Errorf("finish scan: invalid status %q", status)
	}
	n, err := s.q.FinishScan(ctx, dbgen.FinishScanParams{
		Status:     status,
		FinishedAt: sql.NullString{String: formatTime(s.now()), Valid: true},
		Error:      errMsg,
		ID:         id,
	})
	if err != nil {
		return fmt.Errorf("finish scan: %w", err)
	}
	if n == 0 {
		if _, err := s.GetScan(ctx, id); err != nil {
			return err
		}
		return fmt.Errorf("finish scan: scan %d is not running", id)
	}
	return nil
}

// ListScans returns the most recent scans, newest first.
func (s *Store) ListScans(ctx context.Context, limit int) ([]model.Scan, error) {
	rows, err := s.q.ListScans(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("list scans: %w", err)
	}
	out := make([]model.Scan, 0, len(rows))
	for _, r := range rows {
		sc, err := toScan(r)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, nil
}

// GetScan returns one scan, or ErrNotFound.
func (s *Store) GetScan(ctx context.Context, id int64) (model.Scan, error) {
	row, err := s.q.GetScan(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Scan{}, ErrNotFound
	}
	if err != nil {
		return model.Scan{}, fmt.Errorf("get scan: %w", err)
	}
	return toScan(row)
}

func toScan(r dbgen.Scan) (model.Scan, error) {
	started, err := parseTime(r.StartedAt)
	if err != nil {
		return model.Scan{}, fmt.Errorf("scan %d: started_at: %w", r.ID, err)
	}
	sc := model.Scan{
		ID:             r.ID,
		Target:         r.Target,
		Status:         r.Status,
		StartedAt:      started,
		Error:          r.Error,
		DeviceCount:    int(r.DeviceCount),
		LinkCount:      int(r.LinkCount),
		ContainerCount: int(r.ContainerCount),
	}
	if r.FinishedAt.Valid {
		fin, err := parseTime(r.FinishedAt.String)
		if err != nil {
			return model.Scan{}, fmt.Errorf("scan %d: finished_at: %w", r.ID, err)
		}
		sc.FinishedAt = &fin
	}
	return sc, nil
}
