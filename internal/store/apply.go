package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"github.com/FlexEbat/Netscribe/internal/collector"
	"github.com/FlexEbat/Netscribe/internal/model"
	dbgen "github.com/FlexEbat/Netscribe/internal/store/db"
)

// ApplyResult merges the result of a scan into the database in one transaction.
//
// A device is identified by its MAC, or by its IP when it has none. A known MAC with
// a new IP updates the row. A device found by IP that has no MAC yet gets the MAC.
// Devices whose IP lies inside the scan targets and that the scan did not see go
// offline and stay in the database. Devices outside the targets are left alone,
// so scanning one subnet never marks the others offline.
func (s *Store) ApplyResult(ctx context.Context, scanID int64, r collector.Result) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("apply result: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // after Commit this is a no-op
	q := s.q.WithTx(tx)

	scan, err := q.GetScan(ctx, scanID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("apply result: %w", err)
	}
	if scan.Status != "running" {
		return fmt.Errorf("apply result: scan %d is not running", scanID)
	}
	targets, err := parseTargets(scan.Target)
	if err != nil {
		return fmt.Errorf("apply result: %w", err)
	}

	ts := formatTime(s.now())
	seen := make(map[int64]struct{}, len(r.Devices))
	for _, in := range r.Devices {
		id, err := mergeDevice(ctx, q, in, ts)
		if err != nil {
			return fmt.Errorf("apply result: %w", err)
		}
		seen[id] = struct{}{}
	}

	online, err := q.ListOnlineDeviceAddrs(ctx)
	if err != nil {
		return fmt.Errorf("apply result: %w", err)
	}
	for _, d := range online {
		if _, ok := seen[d.ID]; ok || !inTargets(targets, d.Ip) {
			continue
		}
		if err := q.SetDeviceOffline(ctx, d.ID); err != nil {
			return fmt.Errorf("apply result: %w", err)
		}
	}

	if err := q.SetScanCounts(ctx, dbgen.SetScanCountsParams{DeviceCount: int64(len(seen)), ID: scanID}); err != nil {
		return fmt.Errorf("apply result: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("apply result: %w", err)
	}
	return nil
}

func parseTargets(list string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, part := range strings.Split(list, ",") {
		p, err := netip.ParsePrefix(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("scan target %q: %w", part, err)
		}
		out = append(out, p)
	}
	return out, nil
}

func inTargets(targets []netip.Prefix, ip string) bool {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	for _, t := range targets {
		if t.Contains(addr) {
			return true
		}
	}
	return false
}

// mergeDevice inserts or updates one device and returns its row id.
// A non-empty value wins over an empty one, and an unknown kind never replaces a known one.
func mergeDevice(ctx context.Context, q *dbgen.Queries, in model.DeviceInput, ts string) (int64, error) {
	mac := strings.ToLower(strings.TrimSpace(in.Key.MAC))
	ip := strings.TrimSpace(in.Key.IP)
	if mac == "" && ip == "" {
		return 0, errors.New("device has neither mac nor ip")
	}
	if in.Source == "" {
		return 0, errors.New("device has no source")
	}
	kind := string(in.Kind)
	if kind == "" {
		kind = string(model.KindUnknown)
	}

	cur, found, err := findDevice(ctx, q, mac, ip)
	if err != nil {
		return 0, err
	}
	if !found {
		id, err := q.InsertDevice(ctx, dbgen.InsertDeviceParams{
			Mac: mac, Ip: ip, Hostname: in.Hostname, Vendor: in.Vendor, Kind: kind,
			Description: in.Description, Source: in.Source, FirstSeenAt: ts, LastSeenAt: ts,
		})
		if err != nil {
			return 0, fmt.Errorf("insert device: %w", err)
		}
		return id, nil
	}

	if kind == string(model.KindUnknown) {
		kind = cur.Kind
	}
	err = q.UpdateDeviceFromScan(ctx, dbgen.UpdateDeviceFromScanParams{
		Mac:         firstNonEmpty(mac, cur.Mac),
		Ip:          firstNonEmpty(ip, cur.Ip),
		Hostname:    firstNonEmpty(in.Hostname, cur.Hostname),
		Vendor:      firstNonEmpty(in.Vendor, cur.Vendor),
		Kind:        kind,
		Description: firstNonEmpty(in.Description, cur.Description),
		LastSeenAt:  ts,
		ID:          cur.ID,
	})
	if err != nil {
		return 0, fmt.Errorf("update device %d: %w", cur.ID, err)
	}
	return cur.ID, nil
}

// findDevice looks a device up by MAC, then among records without a MAC by IP.
// An input without a MAC falls back to any record with that IP.
func findDevice(ctx context.Context, q *dbgen.Queries, mac, ip string) (dbgen.Device, bool, error) {
	lookups := []func() (dbgen.Device, error){}
	if mac != "" {
		lookups = append(lookups, func() (dbgen.Device, error) { return q.GetDeviceByMAC(ctx, mac) })
	}
	if ip != "" {
		lookups = append(lookups, func() (dbgen.Device, error) { return q.GetDeviceByIPWithoutMAC(ctx, ip) })
		if mac == "" {
			lookups = append(lookups, func() (dbgen.Device, error) { return q.GetDeviceByIP(ctx, ip) })
		}
	}
	for _, lookup := range lookups {
		d, err := lookup()
		if err == nil {
			return d, true, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return dbgen.Device{}, false, fmt.Errorf("find device: %w", err)
		}
	}
	return dbgen.Device{}, false, nil
}

func firstNonEmpty(preferred, fallback string) string {
	if preferred != "" {
		return preferred
	}
	return fallback
}
