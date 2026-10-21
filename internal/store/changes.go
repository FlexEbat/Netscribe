package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/FlexEbat/Netscribe/internal/model"
	dbgen "github.com/FlexEbat/Netscribe/internal/store/db"
)

// The kinds of change a scan can write.
const (
	ChangeDeviceAdded      = "device_added"
	ChangeDeviceOnline     = "device_online"
	ChangeDeviceOffline    = "device_offline"
	ChangeDeviceChanged    = "device_changed"
	ChangeLinkAdded        = "link_added"
	ChangeLinkRemoved      = "link_removed"
	ChangeContainerAdded   = "container_added"
	ChangeContainerRemoved = "container_removed"
)

const maxSummaryRunes = 300

// snapshot is the part of the database a scan can change and the feed talks about.
type snapshot struct {
	devices    map[int64]dbgen.Device
	links      map[int64]dbgen.Link
	containers map[string]dbgen.Container
}

func takeSnapshot(ctx context.Context, q *dbgen.Queries) (snapshot, error) {
	s := snapshot{devices: map[int64]dbgen.Device{}, links: map[int64]dbgen.Link{}, containers: map[string]dbgen.Container{}}
	devices, err := q.ListDevices(ctx)
	if err != nil {
		return s, fmt.Errorf("snapshot devices: %w", err)
	}
	for _, d := range devices {
		s.devices[d.ID] = d
	}
	links, err := q.ListLinks(ctx)
	if err != nil {
		return s, fmt.Errorf("snapshot links: %w", err)
	}
	for _, l := range links {
		s.links[l.ID] = l
	}
	containers, err := q.ListContainers(ctx)
	if err != nil {
		return s, fmt.Errorf("snapshot containers: %w", err)
	}
	for _, c := range containers {
		s.containers[c.ID] = c
	}
	return s, nil
}

// change is a change before it has an id, a scan and a time.
type change struct{ kind, entity, summary string }

func deviceName(d dbgen.Device) string {
	for _, s := range []string{d.Label, d.Hostname, d.Ip, d.Mac} {
		if s != "" {
			return s
		}
	}
	return "device"
}

func shortSummary(s string) string {
	s = cleanText(s)
	if r := []rune(s); len(r) > maxSummaryRunes {
		return string(r[:maxSummaryRunes])
	}
	return s
}

// diffSnapshots lists what changed from before to after: devices by id, then links by
// id, then containers by id, so the same scan always writes the same feed.
func diffSnapshots(before, after snapshot) []change {
	var out []change

	ids := make([]int64, 0, len(after.devices))
	for id := range after.devices {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		now := after.devices[id]
		was, existed := before.devices[id]
		key := strconv.FormatInt(id, 10)
		if !existed {
			out = append(out, change{ChangeDeviceAdded, key, fmt.Sprintf("New device %s (%s, %s)", deviceName(now), dashIfEmpty(now.Ip), now.Kind)})
			continue
		}
		if was.Online == 0 && now.Online != 0 {
			out = append(out, change{ChangeDeviceOnline, key, deviceName(now) + " is back online"})
		}
		if was.Online != 0 && now.Online == 0 {
			out = append(out, change{ChangeDeviceOffline, key, deviceName(now) + " went offline"})
		}
		if parts := deviceParts(was, now); len(parts) > 0 {
			out = append(out, change{ChangeDeviceChanged, key, deviceName(now) + " changed: " + strings.Join(parts, "; ")})
		}
	}

	name := func(id int64) string {
		if d, ok := after.devices[id]; ok {
			return deviceName(d)
		}
		if d, ok := before.devices[id]; ok {
			return deviceName(d)
		}
		return "device " + strconv.FormatInt(id, 10)
	}
	linkText := func(l dbgen.Link) string {
		return fmt.Sprintf("%s %s - %s %s", name(l.ADeviceID), dashIfEmpty(l.APort), name(l.BDeviceID), dashIfEmpty(l.BPort))
	}
	var added, removed []int64
	for id := range after.links {
		if _, ok := before.links[id]; !ok {
			added = append(added, id)
		}
	}
	for id := range before.links {
		if _, ok := after.links[id]; !ok {
			removed = append(removed, id)
		}
	}
	sort.Slice(added, func(i, j int) bool { return added[i] < added[j] })
	sort.Slice(removed, func(i, j int) bool { return removed[i] < removed[j] })
	for _, id := range added {
		out = append(out, change{ChangeLinkAdded, strconv.FormatInt(id, 10), "Link appeared: " + linkText(after.links[id])})
	}
	for _, id := range removed {
		out = append(out, change{ChangeLinkRemoved, strconv.FormatInt(id, 10), "Link disappeared: " + linkText(before.links[id])})
	}

	var cadded, cremoved []string
	for id := range after.containers {
		if _, ok := before.containers[id]; !ok {
			cadded = append(cadded, id)
		}
	}
	for id := range before.containers {
		if _, ok := after.containers[id]; !ok {
			cremoved = append(cremoved, id)
		}
	}
	sort.Strings(cadded)
	sort.Strings(cremoved)
	for _, id := range cadded {
		c := after.containers[id]
		out = append(out, change{ChangeContainerAdded, id, fmt.Sprintf("Container %s (%s) appeared on %s", c.Name, c.Image, name(c.DeviceID))})
	}
	for _, id := range cremoved {
		c := before.containers[id]
		out = append(out, change{ChangeContainerRemoved, id, fmt.Sprintf("Container %s (%s) is gone from %s", c.Name, c.Image, name(c.DeviceID))})
	}
	return out
}

// deviceParts lists what changed in the fields of a device that the feed reports.
func deviceParts(was, now dbgen.Device) []string {
	var parts []string
	for _, f := range []struct{ name, old, new string }{
		{"ip", was.Ip, now.Ip}, {"hostname", was.Hostname, now.Hostname}, {"kind", was.Kind, now.Kind},
		{"system", was.OsGuess, now.OsGuess},
	} {
		if f.old != f.new {
			parts = append(parts, fmt.Sprintf("%s %s -> %s", f.name, dashIfEmpty(f.old), dashIfEmpty(f.new)))
		}
	}
	return parts
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// writeChanges stores the changes of a scan.
func writeChanges(ctx context.Context, q *dbgen.Queries, scanID int64, ts string, list []change) error {
	for _, c := range list {
		err := q.InsertChange(ctx, dbgen.InsertChangeParams{
			ScanID: scanID, Kind: c.kind, EntityID: c.entity, Summary: shortSummary(c.summary), At: ts,
		})
		if err != nil {
			return fmt.Errorf("write change: %w", err)
		}
	}
	return nil
}

func toChange(c dbgen.Change) (model.Change, error) {
	at, err := parseTime(c.At)
	if err != nil {
		return model.Change{}, fmt.Errorf("change %d: at: %w", c.ID, err)
	}
	return model.Change{ID: c.ID, ScanID: c.ScanID, Kind: c.Kind, EntityID: c.EntityID, Summary: c.Summary, At: at}, nil
}

// ListChanges returns the newest changes first, at most limit of them. The result is
// never nil.
func (s *Store) ListChanges(ctx context.Context, limit int) ([]model.Change, error) {
	rows, err := s.q.ListChanges(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("list changes: %w", err)
	}
	out := make([]model.Change, 0, len(rows))
	for _, r := range rows {
		c, err := toChange(r)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// LatestChanges returns the last completed scan and what it changed, oldest change
// first. ok is false when no scan has completed yet.
func (s *Store) LatestChanges(ctx context.Context) (scan model.Scan, changes []model.Change, ok bool, err error) {
	row, err := s.q.LatestDoneScan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Scan{}, []model.Change{}, false, nil
	}
	if err != nil {
		return model.Scan{}, nil, false, fmt.Errorf("latest scan: %w", err)
	}
	scan, err = s.GetScan(ctx, row.ID)
	if err != nil {
		return model.Scan{}, nil, false, err
	}
	rows, err := s.q.ListChangesByScan(ctx, row.ID)
	if err != nil {
		return model.Scan{}, nil, false, fmt.Errorf("list scan changes: %w", err)
	}
	changes = make([]model.Change, 0, len(rows))
	for _, r := range rows {
		c, err := toChange(r)
		if err != nil {
			return model.Scan{}, nil, false, err
		}
		changes = append(changes, c)
	}
	return scan, changes, true, nil
}

// PruneScans keeps the newest keep scans and deletes the older finished ones with their
// changes. A running scan is never deleted. keep below 1 deletes nothing.
func (s *Store) PruneScans(ctx context.Context, keep int) (int64, error) {
	if keep < 1 {
		return 0, nil
	}
	n, err := s.q.PruneScans(ctx, int64(keep))
	if err != nil {
		return 0, fmt.Errorf("prune scans: %w", err)
	}
	return n, nil
}
