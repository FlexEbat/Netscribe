package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/collector"
	"github.com/FlexEbat/Netscribe/internal/model"
)

// newTestStore returns an in-memory store whose clock advances one minute per reading.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	clock := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time {
		clock = clock.Add(time.Minute)
		return clock
	}
	return s
}

func dev(mac, ip string) model.DeviceInput {
	return model.DeviceInput{Key: model.DeviceKey{MAC: mac, IP: ip}, Kind: model.KindUnknown, Source: "arp"}
}

// scanOnce runs a complete scan of target with the given devices.
func scanOnce(t *testing.T, s *Store, target string, devs ...model.DeviceInput) model.Scan {
	t.Helper()
	ctx := context.Background()
	sc, err := s.StartScan(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyResult(ctx, sc.ID, collector.Result{Devices: devs}); err != nil {
		t.Fatalf("ApplyResult: %v", err)
	}
	if err := s.FinishScan(ctx, sc.ID, "done", ""); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetScan(ctx, sc.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func list(t *testing.T, s *Store, f DeviceFilter) []model.Device {
	t.Helper()
	out, err := s.ListDevices(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func byMAC(t *testing.T, s *Store, mac string) model.Device {
	t.Helper()
	for _, d := range list(t, s, DeviceFilter{}) {
		if d.MAC == mac {
			return d
		}
	}
	t.Fatalf("no device with mac %s", mac)
	return model.Device{}
}

const lan = "192.168.1.0/24"

func TestApplyInsertsDevices(t *testing.T) {
	s := newTestStore(t)
	sc := scanOnce(t, s, lan, dev("aa:aa:aa:aa:aa:01", "192.168.1.1"), dev("aa:aa:aa:aa:aa:02", "192.168.1.2"))

	if sc.Status != "done" || sc.DeviceCount != 2 || sc.FinishedAt == nil {
		t.Errorf("scan = %+v", sc)
	}
	got := list(t, s, DeviceFilter{})
	if len(got) != 2 {
		t.Fatalf("%d devices, want 2", len(got))
	}
	d := got[0]
	if !d.Online || d.Source != "arp" || d.Kind != model.KindUnknown || d.FirstSeenAt.IsZero() || !d.FirstSeenAt.Equal(d.LastSeenAt) {
		t.Errorf("device = %+v", d)
	}
}

func TestRepeatedScanCreatesNoDuplicates(t *testing.T) {
	s := newTestStore(t)
	a, b := dev("aa:aa:aa:aa:aa:01", "192.168.1.1"), dev("aa:aa:aa:aa:aa:02", "192.168.1.2")
	scanOnce(t, s, lan, a, b)
	first := byMAC(t, s, "aa:aa:aa:aa:aa:01")
	scanOnce(t, s, lan, a, b)
	scanOnce(t, s, lan, a, b)

	got := list(t, s, DeviceFilter{})
	if len(got) != 2 {
		t.Fatalf("%d devices after three scans, want 2", len(got))
	}
	again := byMAC(t, s, "aa:aa:aa:aa:aa:01")
	if again.ID != first.ID || !again.FirstSeenAt.Equal(first.FirstSeenAt) {
		t.Errorf("the row was recreated or lost first_seen_at: %+v vs %+v", first, again)
	}
	if !again.LastSeenAt.After(first.LastSeenAt) {
		t.Errorf("last_seen_at did not advance: %v then %v", first.LastSeenAt, again.LastSeenAt)
	}
}

func TestSameMACWithNewIPUpdatesTheRow(t *testing.T) {
	s := newTestStore(t)
	scanOnce(t, s, lan, dev("aa:aa:aa:aa:aa:01", "192.168.1.10"))
	before := byMAC(t, s, "aa:aa:aa:aa:aa:01")
	scanOnce(t, s, lan, dev("aa:aa:aa:aa:aa:01", "192.168.1.77"))

	got := list(t, s, DeviceFilter{})
	if len(got) != 1 {
		t.Fatalf("%d devices, want 1", len(got))
	}
	if got[0].ID != before.ID || got[0].IP != "192.168.1.77" || !got[0].Online {
		t.Errorf("device = %+v", got[0])
	}
}

func TestMACIsNormalized(t *testing.T) {
	s := newTestStore(t)
	scanOnce(t, s, lan, dev("AA:BB:CC:DD:EE:FF", "192.168.1.1"))
	scanOnce(t, s, lan, dev(" aa:bb:cc:dd:ee:ff ", "192.168.1.1"))
	got := list(t, s, DeviceFilter{})
	if len(got) != 1 || got[0].MAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("devices = %+v", got)
	}
}

func TestMissingDeviceGoesOfflineAndStaysInTheDatabase(t *testing.T) {
	s := newTestStore(t)
	a, b := dev("aa:aa:aa:aa:aa:01", "192.168.1.1"), dev("aa:aa:aa:aa:aa:02", "192.168.1.2")
	scanOnce(t, s, lan, a, b)
	sc := scanOnce(t, s, lan, a)

	if sc.DeviceCount != 1 {
		t.Errorf("DeviceCount = %d, want 1", sc.DeviceCount)
	}
	if d := byMAC(t, s, "aa:aa:aa:aa:aa:02"); d.Online {
		t.Error("the missing device is still online")
	}
	if d := byMAC(t, s, "aa:aa:aa:aa:aa:01"); !d.Online {
		t.Error("the seen device went offline")
	}

	scanOnce(t, s, lan, a, b)
	if d := byMAC(t, s, "aa:aa:aa:aa:aa:02"); !d.Online {
		t.Error("the device did not come back online")
	}
}

func TestOfflineMarkingStaysInsideTheScanTargets(t *testing.T) {
	s := newTestStore(t)
	scanOnce(t, s, lan+",10.0.0.0/24", dev("aa:aa:aa:aa:aa:01", "192.168.1.1"), dev("aa:aa:aa:aa:aa:02", "10.0.0.5"), dev("aa:aa:aa:aa:aa:03", "192.168.1.9"))
	// A scan of one subnet must not touch the others.
	scanOnce(t, s, "10.0.0.0/24", dev("aa:aa:aa:aa:aa:02", "10.0.0.5"))

	for mac, wantOnline := range map[string]bool{
		"aa:aa:aa:aa:aa:01": true, // other subnet, untouched
		"aa:aa:aa:aa:aa:02": true, // seen
		"aa:aa:aa:aa:aa:03": true, // other subnet, untouched
	} {
		if got := byMAC(t, s, mac).Online; got != wantOnline {
			t.Errorf("%s online = %v, want %v", mac, got, wantOnline)
		}
	}
	scanOnce(t, s, lan, dev("aa:aa:aa:aa:aa:01", "192.168.1.1"))
	if byMAC(t, s, "aa:aa:aa:aa:aa:03").Online {
		t.Error("an unseen device inside the targets stayed online")
	}
	if !byMAC(t, s, "aa:aa:aa:aa:aa:02").Online {
		t.Error("a device outside the targets was marked offline")
	}
}

func TestDeviceWithoutMACGetsItFromTheScan(t *testing.T) {
	s := newTestStore(t)
	// A record with no MAC, as manual entry will create later.
	if _, err := s.db.Exec(`INSERT INTO devices (ip, hostname, source, first_seen_at, last_seen_at) VALUES ('192.168.1.5', 'typed-by-hand', 'manual', '2026-01-01T00:00:00.000Z', '2026-01-01T00:00:00.000Z')`); err != nil {
		t.Fatal(err)
	}
	scanOnce(t, s, lan, dev("aa:aa:aa:aa:aa:05", "192.168.1.5"))

	got := list(t, s, DeviceFilter{})
	if len(got) != 1 {
		t.Fatalf("%d devices, want the record to be completed, not duplicated", len(got))
	}
	if got[0].MAC != "aa:aa:aa:aa:aa:05" || got[0].Hostname != "typed-by-hand" || !got[0].Online {
		t.Errorf("device = %+v", got[0])
	}
}

func TestInputWithoutMACMatchesByIP(t *testing.T) {
	s := newTestStore(t)
	scanOnce(t, s, lan, dev("aa:aa:aa:aa:aa:01", "192.168.1.1"))
	in := dev("", "192.168.1.1")
	in.Hostname = "gw"
	scanOnce(t, s, lan, in)

	got := list(t, s, DeviceFilter{})
	if len(got) != 1 || got[0].MAC != "aa:aa:aa:aa:aa:01" || got[0].Hostname != "gw" {
		t.Errorf("devices = %+v, want the MAC kept and the hostname added", got)
	}

	// Two inputs without a MAC and the same IP are one device.
	s2 := newTestStore(t)
	scanOnce(t, s2, lan, dev("", "192.168.1.8"))
	scanOnce(t, s2, lan, dev("", "192.168.1.8"))
	if n := len(list(t, s2, DeviceFilter{})); n != 1 {
		t.Errorf("%d devices, want 1", n)
	}
}

func TestMergeKeepsKnownValues(t *testing.T) {
	s := newTestStore(t)
	rich := dev("aa:aa:aa:aa:aa:01", "192.168.1.1")
	rich.Hostname, rich.Vendor, rich.Description, rich.Kind = "gw.lan", "Acme", "Acme router OS 1.2", model.KindRouter
	scanOnce(t, s, lan, rich)
	scanOnce(t, s, lan, dev("aa:aa:aa:aa:aa:01", "192.168.1.1")) // empty values, unknown kind

	d := byMAC(t, s, "aa:aa:aa:aa:aa:01")
	if d.Hostname != "gw.lan" || d.Vendor != "Acme" || d.Description != "Acme router OS 1.2" {
		t.Errorf("an empty value overwrote a known one: %+v", d)
	}
	if d.Kind != model.KindRouter {
		t.Errorf("Kind = %q: unknown must not replace a known kind", d.Kind)
	}

	upgrade := dev("aa:aa:aa:aa:aa:01", "192.168.1.1")
	upgrade.Hostname, upgrade.Kind = "gw2.lan", model.KindFirewall
	scanOnce(t, s, lan, upgrade)
	d = byMAC(t, s, "aa:aa:aa:aa:aa:01")
	if d.Hostname != "gw2.lan" || d.Kind != model.KindFirewall {
		t.Errorf("a non-empty value did not win: %+v", d)
	}
}

func TestApplyDoesNotTouchStateWhenInputIsInvalid(t *testing.T) {
	s := newTestStore(t)
	scanOnce(t, s, lan, dev("aa:aa:aa:aa:aa:01", "192.168.1.1"), dev("aa:aa:aa:aa:aa:02", "192.168.1.2"))

	ctx := context.Background()
	sc, _ := s.StartScan(ctx, lan)
	noSource := dev("aa:aa:aa:aa:aa:09", "192.168.1.9")
	noSource.Source = ""
	err := s.ApplyResult(ctx, sc.ID, collector.Result{Devices: []model.DeviceInput{dev("aa:aa:aa:aa:aa:03", "192.168.1.3"), noSource}})
	if err == nil {
		t.Fatal("ApplyResult accepted a device without a source")
	}
	got := list(t, s, DeviceFilter{})
	if len(got) != 2 {
		t.Errorf("%d devices: the transaction was not rolled back", len(got))
	}
	for _, d := range got {
		if !d.Online {
			t.Errorf("%s went offline although the apply failed", d.MAC)
		}
	}
	if err := s.ApplyResult(ctx, sc.ID, collector.Result{Devices: []model.DeviceInput{dev("", "")}}); err == nil {
		t.Error("ApplyResult accepted a device without mac and ip")
	}
}

func TestApplyWithCanceledContextChangesNothing(t *testing.T) {
	s := newTestStore(t)
	scanOnce(t, s, lan, dev("aa:aa:aa:aa:aa:01", "192.168.1.1"))
	sc, _ := s.StartScan(context.Background(), lan)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.ApplyResult(ctx, sc.ID, collector.Result{}); err == nil {
		t.Fatal("ApplyResult succeeded with a canceled context")
	}
	if !byMAC(t, s, "aa:aa:aa:aa:aa:01").Online {
		t.Error("a canceled apply marked a device offline")
	}
}

func TestApplyRequiresARunningScan(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.ApplyResult(ctx, 999, collector.Result{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown scan: %v, want ErrNotFound", err)
	}
	sc, _ := s.StartScan(ctx, lan)
	if err := s.FinishScan(ctx, sc.ID, "failed", "canceled"); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyResult(ctx, sc.ID, collector.Result{Devices: []model.DeviceInput{dev("aa:aa:aa:aa:aa:01", "192.168.1.1")}}); err == nil {
		t.Error("ApplyResult accepted a finished scan")
	}
	if n := len(list(t, s, DeviceFilter{})); n != 0 {
		t.Errorf("%d devices stored for a finished scan", n)
	}
}

func TestFailedScanLeavesDevicesAsTheyWere(t *testing.T) {
	s := newTestStore(t)
	scanOnce(t, s, lan, dev("aa:aa:aa:aa:aa:01", "192.168.1.1"))
	ctx := context.Background()
	sc, _ := s.StartScan(ctx, lan)
	if err := s.FinishScan(ctx, sc.ID, "failed", "canceled"); err != nil {
		t.Fatal(err)
	}
	if !byMAC(t, s, "aa:aa:aa:aa:aa:01").Online {
		t.Error("a failed scan marked the device offline")
	}
	got, _ := s.GetScan(ctx, sc.ID)
	if got.Status != "failed" || got.Error != "canceled" || got.FinishedAt == nil {
		t.Errorf("scan = %+v", got)
	}
}

func TestScanLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	sc, err := s.StartScan(ctx, lan)
	if err != nil {
		t.Fatal(err)
	}
	if sc.Status != "running" || sc.FinishedAt != nil || sc.Target != lan || sc.ID == 0 {
		t.Errorf("new scan = %+v", sc)
	}
	if err := s.FinishScan(ctx, sc.ID, "bogus", ""); err == nil {
		t.Error("FinishScan accepted an invalid status")
	}
	if err := s.FinishScan(ctx, 999, "done", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("FinishScan(unknown) = %v, want ErrNotFound", err)
	}
	if err := s.FinishScan(ctx, sc.ID, "done", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishScan(ctx, sc.ID, "failed", "late"); err == nil {
		t.Error("a finished scan was finished twice")
	}
	got, _ := s.GetScan(ctx, sc.ID)
	if got.Status != "done" || got.Error != "" {
		t.Errorf("the second FinishScan changed the scan: %+v", got)
	}
	if _, err := s.GetScan(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetScan(unknown) = %v, want ErrNotFound", err)
	}
}

func TestTimestampsRoundTripInUTC(t *testing.T) {
	s := newTestStore(t)
	scanOnce(t, s, lan, dev("aa:aa:aa:aa:aa:01", "192.168.1.1"))
	d := byMAC(t, s, "aa:aa:aa:aa:aa:01")
	if d.FirstSeenAt.Location() != time.UTC || d.FirstSeenAt.Year() != 2026 {
		t.Errorf("FirstSeenAt = %v", d.FirstSeenAt)
	}
}

func TestListDevicesSortsByNumericIP(t *testing.T) {
	s := newTestStore(t)
	scanOnce(t, s, "10.0.0.0/16",
		dev("aa:aa:aa:aa:aa:01", "10.0.0.100"), dev("aa:aa:aa:aa:aa:02", "10.0.0.9"),
		dev("aa:aa:aa:aa:aa:03", "10.0.0.20"), dev("aa:aa:aa:aa:aa:04", "10.0.1.1"), dev("aa:aa:aa:aa:aa:05", "10.0.0.2"))
	var got []string
	for _, d := range list(t, s, DeviceFilter{}) {
		got = append(got, d.IP)
	}
	want := []string{"10.0.0.2", "10.0.0.9", "10.0.0.20", "10.0.0.100", "10.0.1.1"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestListDevicesPutsDevicesWithoutIPLast(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.db.Exec(`INSERT INTO devices (mac, ip, source, first_seen_at, last_seen_at) VALUES ('aa:aa:aa:aa:aa:ff', '', 'manual', '2026-01-01T00:00:00.000Z', '2026-01-01T00:00:00.000Z')`); err != nil {
		t.Fatal(err)
	}
	scanOnce(t, s, lan, dev("aa:aa:aa:aa:aa:01", "192.168.1.1"))
	got := list(t, s, DeviceFilter{})
	if len(got) != 2 || got[0].IP != "192.168.1.1" || got[1].IP != "" {
		t.Errorf("order = %+v", got)
	}
}

func TestListDevicesFilters(t *testing.T) {
	s := newTestStore(t)
	printer := dev("aa:aa:aa:aa:aa:01", "192.168.1.10")
	printer.Hostname, printer.Vendor, printer.Kind = "Принтер-Офис", "HP Inc.", model.KindPrinter
	nas := dev("aa:aa:aa:aa:aa:02", "192.168.1.20")
	nas.Hostname, nas.Kind = "nas.lan", model.KindNAS
	gone := dev("aa:aa:aa:aa:aa:03", "192.168.1.30")
	scanOnce(t, s, lan, printer, nas, gone)
	scanOnce(t, s, lan, printer, nas) // gone goes offline

	yes, no := true, false
	names := func(f DeviceFilter) []string {
		var out []string
		for _, d := range list(t, s, f) {
			out = append(out, d.IP)
		}
		return out
	}
	tests := []struct {
		name string
		f    DeviceFilter
		want int
	}{
		{"no filter", DeviceFilter{}, 3},
		{"online", DeviceFilter{Online: &yes}, 2},
		{"offline", DeviceFilter{Online: &no}, 1},
		{"kind", DeviceFilter{Kind: model.KindNAS}, 1},
		{"query by hostname ignores case", DeviceFilter{Query: "NAS"}, 1},
		{"query folds Cyrillic case", DeviceFilter{Query: "принтер"}, 1},
		{"query by vendor", DeviceFilter{Query: "hp inc"}, 1},
		{"query by mac", DeviceFilter{Query: "AA:AA:AA:AA:AA:03"}, 1},
		{"query by ip substring", DeviceFilter{Query: "1.20"}, 1},
		{"query is trimmed", DeviceFilter{Query: "  nas  "}, 1},
		{"blank query means no filter", DeviceFilter{Query: "   "}, 3},
		{"filters combine", DeviceFilter{Online: &yes, Query: "168.1.3"}, 0},
		{"no match", DeviceFilter{Query: "zzz"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := names(tt.f); len(got) != tt.want {
				t.Errorf("got %v, want %d devices", got, tt.want)
			}
		})
	}
}

func TestIdentityIndexesRejectDuplicates(t *testing.T) {
	s := newTestStore(t)
	ins := `INSERT INTO devices (mac, ip, source, first_seen_at, last_seen_at) VALUES (?, ?, 'arp', 't', 't')`
	if _, err := s.db.Exec(ins, "aa:aa:aa:aa:aa:01", "1.1.1.1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(ins, "aa:aa:aa:aa:aa:01", "2.2.2.2"); err == nil {
		t.Error("two rows share a MAC")
	}
	if _, err := s.db.Exec(ins, "", "3.3.3.3"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(ins, "", "3.3.3.3"); err == nil {
		t.Error("two rows without a MAC share an IP")
	}
	if _, err := s.db.Exec(ins, "aa:aa:aa:aa:aa:02", "3.3.3.3"); err != nil {
		t.Errorf("a MAC-bearing row may reuse an IP of a MAC-less row: %v", err)
	}
	if _, err := s.db.Exec(ins, "", ""); err != nil {
		t.Errorf("rows with neither MAC nor IP must not collide: %v", err)
	}
	if _, err := s.db.Exec(ins, "", ""); err != nil {
		t.Errorf("rows with neither MAC nor IP must not collide: %v", err)
	}
}
