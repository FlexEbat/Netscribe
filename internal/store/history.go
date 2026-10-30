package store

import (
	"context"
	"fmt"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
)

const overviewScans = 10 // the scans that count for the scan health

// DeviceHistory returns what the stored scans say about one device. A device that was never
// scanned, a manual one for example, has an empty history. It returns ErrNotFound for a
// device that does not exist.
func (s *Store) DeviceHistory(ctx context.Context, id int64) (model.DeviceHistory, error) {
	if _, err := s.GetDevice(ctx, id); err != nil {
		return model.DeviceHistory{}, err
	}
	rows, err := s.q.ListPresence(ctx, id)
	if err != nil {
		return model.DeviceHistory{}, fmt.Errorf("device history: %w", err)
	}
	points := make([]model.PresencePoint, 0, len(rows))
	for _, r := range rows {
		at, err := parseTime(r.StartedAt)
		if err != nil {
			return model.DeviceHistory{}, fmt.Errorf("device history: scan %d: %w", r.ScanID, err)
		}
		points = append(points, model.PresencePoint{ScanID: r.ScanID, At: at, Online: r.Online != 0, IP: r.Ip})
	}
	return model.BuildHistory(points), nil
}

// Overview counts what the overview page shows.
func (s *Store) Overview(ctx context.Context) (model.Overview, error) {
	o := model.Overview{Changes: []model.Change{}}
	devices, err := s.ListDevices(ctx, DeviceFilter{})
	if err != nil {
		return o, err
	}
	weekAgo := s.now().Add(-7 * 24 * time.Hour)
	for _, d := range devices {
		o.Devices.Total++
		if d.Online {
			o.Devices.Online++
		} else {
			o.Devices.Offline++
		}
		if !d.Trusted {
			o.Devices.Unknown++
		}
		if !d.Manual && !d.FirstSeenAt.Before(weekAgo) {
			o.Devices.NewThisWeek++
		}
	}
	services, err := s.ListServices(ctx, 0)
	if err != nil {
		return o, err
	}
	for _, sv := range services {
		if sv.Risk != nil {
			o.Risky++
		}
	}
	scans, err := s.ListScans(ctx, overviewScans)
	if err != nil {
		return o, err
	}
	if len(scans) > 0 {
		o.LastScan = &scans[0]
	}
	for _, sc := range scans {
		// A scan that is still running says nothing about health yet.
		if sc.Status == "running" {
			continue
		}
		o.RecentScans.Total++
		if sc.Status == "failed" {
			o.RecentScans.Failed++
		}
	}
	changes, err := s.ListChanges(ctx, overviewScans)
	if err != nil {
		return o, err
	}
	if changes != nil {
		o.Changes = changes
	}
	return o, nil
}
