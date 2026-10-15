package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"unicode"
	"unicode/utf8"

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

	if err := applyInterfaces(ctx, q, r.Interfaces); err != nil {
		return fmt.Errorf("apply result: %w", err)
	}
	if err := applyLinks(ctx, q, r, ts); err != nil {
		return fmt.Errorf("apply result: %w", err)
	}
	if err := applyServices(ctx, q, r, ts); err != nil {
		return fmt.Errorf("apply result: %w", err)
	}
	if err := applyContainers(ctx, q, r, ts); err != nil {
		return fmt.Errorf("apply result: %w", err)
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

	links, err := q.ListLinks(ctx)
	if err != nil {
		return fmt.Errorf("apply result: %w", err)
	}
	containers, err := q.CountContainers(ctx)
	if err != nil {
		return fmt.Errorf("apply result: %w", err)
	}
	if err := q.SetScanCounts(ctx, dbgen.SetScanCountsParams{
		DeviceCount: int64(len(seen)), LinkCount: int64(len(links)), ContainerCount: containers, ID: scanID,
	}); err != nil {
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
	// Names and descriptions come from the network, so a third party controls them.
	in.Hostname, in.Vendor, in.Description = cleanText(in.Hostname), cleanText(in.Vendor), cleanText(in.Description)
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

	if kind == string(model.KindUnknown) || cur.KindLocked != 0 {
		kind = cur.Kind // an unknown kind never replaces a known one, a person's choice stays
	}
	err = q.UpdateDeviceFromScan(ctx, dbgen.UpdateDeviceFromScanParams{
		Mac:         firstNonEmpty(mac, cur.Mac),
		Ip:          firstNonEmpty(ip, cur.Ip),
		Hostname:    firstNonEmpty(in.Hostname, cur.Hostname),
		Vendor:      firstNonEmpty(in.Vendor, cur.Vendor),
		Kind:        kind,
		Source:      betterSource(cur.Source, in.Source),
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

// maxTextRunes is the longest text taken from the network (section 9.5 of the spec).
const maxTextRunes = 255

// cleanText removes control characters, trims the ends and cuts s to maxTextRunes characters.
func cleanText(s string) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s))
	if utf8.RuneCountInString(s) <= maxTextRunes {
		return s
	}
	return string([]rune(s)[:maxTextRunes])
}

func firstNonEmpty(preferred, fallback string) string {
	if preferred != "" {
		return preferred
	}
	return fallback
}

// sourceRank orders the sources of a device: a person beats ssh, ssh beats snmp, snmp
// beats arp and probe (section 5.2 of the spec). Unknown sources rank lowest.
func sourceRank(source string) int {
	switch source {
	case "manual":
		return 4
	case "ssh":
		return 3
	case "snmp":
		return 2
	case "arp", "probe":
		return 1
	}
	return 0
}

// betterSource keeps the source with the higher rank, the current one on a tie.
func betterSource(cur, in string) string {
	if sourceRank(in) > sourceRank(cur) {
		return in
	}
	return cur
}

// applyInterfaces replaces the interfaces of every device that reported some. A device
// that reported none keeps what it had, so a failed SNMP read does not empty the panel.
func applyInterfaces(ctx context.Context, q *dbgen.Queries, in []model.InterfaceInput) error {
	type ifKey struct{ name, ip string }
	var order []int64
	groups := map[int64][]model.InterfaceInput{}
	for _, i := range in {
		dev, found, err := findDevice(ctx, q, strings.ToLower(strings.TrimSpace(i.Device.MAC)), strings.TrimSpace(i.Device.IP))
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		if _, ok := groups[dev.ID]; !ok {
			order = append(order, dev.ID)
		}
		groups[dev.ID] = append(groups[dev.ID], i)
	}
	for _, id := range order {
		if err := q.DeleteDeviceInterfaces(ctx, id); err != nil {
			return fmt.Errorf("replace interfaces of device %d: %w", id, err)
		}
		done := map[ifKey]struct{}{}
		for _, i := range groups[id] {
			name := cleanText(i.Name)
			if name == "" {
				continue
			}
			ip := strings.TrimSpace(i.IP)
			if _, dup := done[ifKey{name, ip}]; dup {
				continue
			}
			done[ifKey{name, ip}] = struct{}{}
			up := int64(0)
			if i.Up {
				up = 1
			}
			err := q.InsertInterface(ctx, dbgen.InsertInterfaceParams{
				DeviceID: id, Name: name, Mac: strings.ToLower(strings.TrimSpace(i.MAC)), Ip: ip, Up: up,
			})
			if err != nil {
				return fmt.Errorf("insert interface of device %d: %w", id, err)
			}
		}
	}
	return nil
}

// applyLinks stores the links a scan found and removes the lldp links that disappeared.
//
// A link is one row: the pair is kept in id order, so the same cable reported from both
// ends is one link. A link drawn by a person keeps its source. Only links that touch a
// device whose LLDP table was read in this scan can disappear, a device that did not
// answer never loses its links.
func applyLinks(ctx context.Context, q *dbgen.Queries, r collector.Result, ts string) error {
	seen := map[int64]struct{}{}
	for _, l := range r.Links {
		a, okA, err := findDevice(ctx, q, strings.ToLower(strings.TrimSpace(l.A.MAC)), strings.TrimSpace(l.A.IP))
		if err != nil {
			return err
		}
		b, okB, err := findDevice(ctx, q, strings.ToLower(strings.TrimSpace(l.B.MAC)), strings.TrimSpace(l.B.IP))
		if err != nil {
			return err
		}
		if !okA || !okB || a.ID == b.ID {
			continue
		}
		aID, bID, aPort, bPort := a.ID, b.ID, cleanText(l.APort), cleanText(l.BPort)
		if aID > bID {
			aID, bID, aPort, bPort = bID, aID, bPort, aPort
		}
		cur, err := q.GetLinkByKey(ctx, dbgen.GetLinkByKeyParams{ADeviceID: aID, APort: aPort, BDeviceID: bID, BPort: bPort})
		switch {
		case err == nil:
			if err := q.TouchLink(ctx, dbgen.TouchLinkParams{LastSeenAt: ts, ID: cur.ID}); err != nil {
				return fmt.Errorf("touch link %d: %w", cur.ID, err)
			}
			seen[cur.ID] = struct{}{}
		case errors.Is(err, sql.ErrNoRows):
			row, err := q.InsertLink(ctx, dbgen.InsertLinkParams{
				ADeviceID: aID, APort: aPort, BDeviceID: bID, BPort: bPort, Source: l.Source, LastSeenAt: ts,
			})
			if err != nil {
				return fmt.Errorf("insert link: %w", err)
			}
			seen[row.ID] = struct{}{}
		default:
			return fmt.Errorf("find link: %w", err)
		}
	}

	// A link can only vanish when the device that would have reported it was asked: the
	// ends of an lldp link are asked over LLDP, the ends of an fdb link over their FDB.
	polled := map[string]map[int64]struct{}{"lldp": {}, "fdb": {}}
	for source, keys := range map[string][]model.DeviceKey{"lldp": r.LinksPolled, "fdb": r.FDBPolled} {
		for _, k := range keys {
			d, found, err := findDevice(ctx, q, strings.ToLower(strings.TrimSpace(k.MAC)), strings.TrimSpace(k.IP))
			if err != nil {
				return err
			}
			if found {
				polled[source][d.ID] = struct{}{}
			}
		}
	}
	if len(polled["lldp"]) == 0 && len(polled["fdb"]) == 0 {
		return nil
	}
	all, err := q.ListLinks(ctx)
	if err != nil {
		return fmt.Errorf("list links: %w", err)
	}
	for _, l := range all {
		set, ok := polled[l.Source]
		if !ok {
			continue // manual links stay
		}
		if _, ok := seen[l.ID]; ok {
			continue
		}
		_, a := set[l.ADeviceID]
		_, b := set[l.BDeviceID]
		if !a && !b {
			continue
		}
		if _, err := q.DeleteLink(ctx, l.ID); err != nil {
			return fmt.Errorf("delete link %d: %w", l.ID, err)
		}
	}
	return nil
}

// applyServices stores the open ports a scan found. A device whose ports were scanned
// loses the services that did not answer this time, a device that was not scanned keeps
// what it had.
func applyServices(ctx context.Context, q *dbgen.Queries, r collector.Result, ts string) error {
	resolve := func(k model.DeviceKey) (int64, bool, error) {
		d, found, err := findDevice(ctx, q, strings.ToLower(strings.TrimSpace(k.MAC)), strings.TrimSpace(k.IP))
		return d.ID, found, err
	}
	for _, in := range r.Services {
		proto := strings.ToLower(strings.TrimSpace(in.Proto))
		if in.Port < 1 || in.Port > 65535 || (proto != "tcp" && proto != "udp") {
			continue
		}
		id, found, err := resolve(in.Device)
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		name := cleanText(in.Name)
		cur, err := q.GetServiceByKey(ctx, dbgen.GetServiceByKeyParams{DeviceID: id, Port: int64(in.Port), Proto: proto})
		switch {
		case err == nil:
			if err := q.TouchService(ctx, dbgen.TouchServiceParams{Name: name, LastSeenAt: ts, ID: cur.ID}); err != nil {
				return fmt.Errorf("touch service %d: %w", cur.ID, err)
			}
		case errors.Is(err, sql.ErrNoRows):
			err := q.InsertService(ctx, dbgen.InsertServiceParams{
				DeviceID: id, Port: int64(in.Port), Proto: proto, Name: name, FirstSeenAt: ts, LastSeenAt: ts,
			})
			if err != nil {
				return fmt.Errorf("insert service: %w", err)
			}
		default:
			return fmt.Errorf("find service: %w", err)
		}
	}
	for _, k := range r.ServicesPolled {
		id, found, err := resolve(k)
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		if err := q.DeleteDeviceServicesBefore(ctx, dbgen.DeleteDeviceServicesBeforeParams{DeviceID: id, LastSeenAt: ts}); err != nil {
			return fmt.Errorf("delete closed services of device %d: %w", id, err)
		}
	}
	return nil
}
