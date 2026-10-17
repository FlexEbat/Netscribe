package store

import (
	"context"
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/collector"
	"github.com/FlexEbat/Netscribe/internal/model"
)

// scanWith runs a complete scan of 192.168.1.0/24 with the given result.
func scanWith(t *testing.T, s *Store, res collector.Result) model.Scan {
	t.Helper()
	ctx := context.Background()
	sc, err := s.StartScan(ctx, "192.168.1.0/24")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyResult(ctx, sc.ID, res); err != nil {
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

func baselineScan() collector.Result {
	return collector.Result{Devices: []model.DeviceInput{dev(keyA.MAC, keyA.IP), dev(keyB.MAC, keyB.IP)}}
}

func feed(t *testing.T, s *Store) []model.Change {
	t.Helper()
	out, err := s.ListChanges(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func kinds(cs []model.Change) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Kind)
	}
	return out
}

func wantKinds(t *testing.T, got []model.Change, want ...string) {
	t.Helper()
	if g := strings.Join(kinds(got), " "); g != strings.Join(want, " ") {
		t.Errorf("changes = [%s], want [%s]", g, strings.Join(want, " "))
	}
}

func TestTheFirstCompletedScanWritesNoChanges(t *testing.T) {
	s := newTestStore(t)
	scanWith(t, s, baselineScan())
	if got := feed(t, s); len(got) != 0 {
		t.Errorf("changes = %+v, want none from the baseline", got)
	}
}

func TestAScanWithoutDifferencesWritesNothing(t *testing.T) {
	s := newTestStore(t)
	scanWith(t, s, baselineScan())
	scanWith(t, s, baselineScan())
	scanWith(t, s, baselineScan())
	if got := feed(t, s); len(got) != 0 {
		t.Errorf("changes = %+v, want none", got)
	}
}

func TestADeviceAddedIsWritten(t *testing.T) {
	s := newTestStore(t)
	scanWith(t, s, baselineScan())
	res := baselineScan()
	res.Devices = append(res.Devices, dev("aa:00:00:00:00:03", "192.168.1.3"))
	sc := scanWith(t, s, res)

	got := feed(t, s)
	wantKinds(t, got, ChangeDeviceAdded)
	c := got[0]
	if c.ScanID != sc.ID || c.EntityID == "" || !strings.Contains(c.Summary, "192.168.1.3") || c.At.IsZero() {
		t.Errorf("change = %+v", c)
	}
}

func TestADeviceGoingOfflineAndComingBackIsWritten(t *testing.T) {
	s := newTestStore(t)
	scanWith(t, s, baselineScan())
	scanWith(t, s, collector.Result{Devices: []model.DeviceInput{dev(keyA.MAC, keyA.IP)}})
	wantKinds(t, feed(t, s), ChangeDeviceOffline)
	if got := feed(t, s)[0].Summary; !strings.Contains(got, "192.168.1.2") || !strings.Contains(got, "offline") {
		t.Errorf("summary = %q", got)
	}

	scanWith(t, s, baselineScan())
	wantKinds(t, feed(t, s), ChangeDeviceOnline, ChangeDeviceOffline) // newest first
}

func TestADeviceThatChangedItsAddressOrNameIsWritten(t *testing.T) {
	s := newTestStore(t)
	scanWith(t, s, baselineScan())

	moved := dev(keyB.MAC, "192.168.1.20")
	moved.Hostname = "printer-2"
	scanWith(t, s, collector.Result{Devices: []model.DeviceInput{dev(keyA.MAC, keyA.IP), moved}})

	got := feed(t, s)
	wantKinds(t, got, ChangeDeviceChanged)
	for _, want := range []string{"ip 192.168.1.2 -> 192.168.1.20", "hostname - -> printer-2"} {
		if !strings.Contains(got[0].Summary, want) {
			t.Errorf("summary %q lacks %q", got[0].Summary, want)
		}
	}
}

func TestALinkAddedAndRemovedIsWritten(t *testing.T) {
	s := newTestStore(t)
	scanWith(t, s, baselineScan())

	res := baselineScan()
	res.Links = []model.LinkInput{{A: keyA, B: keyB, APort: "Gi0/1", BPort: "eth0", Source: "lldp"}}
	res.LinksPolled = []model.DeviceKey{keyA}
	scanWith(t, s, res)
	got := feed(t, s)
	wantKinds(t, got, ChangeLinkAdded)
	if !strings.Contains(got[0].Summary, "Gi0/1") || !strings.Contains(got[0].Summary, "192.168.1.1") {
		t.Errorf("summary = %q", got[0].Summary)
	}

	res = baselineScan()
	res.LinksPolled = []model.DeviceKey{keyA}
	scanWith(t, s, res)
	got = feed(t, s)
	wantKinds(t, got, ChangeLinkRemoved, ChangeLinkAdded)
	if got[0].EntityID != got[1].EntityID {
		t.Errorf("the link has ids %q and %q", got[1].EntityID, got[0].EntityID)
	}
}

func TestAContainerAddedAndRemovedIsWritten(t *testing.T) {
	s := newTestStore(t)
	scanWith(t, s, baselineScan())

	web := model.ContainerInput{Device: keyA, ID: "abcd", Name: "web", Image: "nginx:1.27", State: "running"}
	res := baselineScan()
	res.Containers = []model.ContainerInput{web}
	res.ContainersPolled = []model.DeviceKey{keyA}
	scanWith(t, s, res)
	got := feed(t, s)
	wantKinds(t, got, ChangeContainerAdded)
	if got[0].EntityID != "abcd" || !strings.Contains(got[0].Summary, "web") || !strings.Contains(got[0].Summary, "192.168.1.1") {
		t.Errorf("change = %+v", got[0])
	}

	res = baselineScan()
	res.ContainersPolled = []model.DeviceKey{keyA}
	scanWith(t, s, res)
	wantKinds(t, feed(t, s), ChangeContainerRemoved, ChangeContainerAdded)
}

func TestAFailedScanWritesNoChangesAndLeavesDevicesOnline(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	scanWith(t, s, baselineScan())

	sc, err := s.StartScan(ctx, "192.168.1.0/24")
	if err != nil {
		t.Fatal(err)
	}
	// A scan that fails is finished without a result: nothing is applied.
	if err := s.FinishScan(ctx, sc.ID, "failed", "network is down"); err != nil {
		t.Fatal(err)
	}
	if got := feed(t, s); len(got) != 0 {
		t.Errorf("changes = %+v, want none", got)
	}
	for _, d := range list(t, s, DeviceFilter{}) {
		if !d.Online {
			t.Errorf("device %s went offline after a failed scan", d.IP)
		}
	}
}

func TestAFailedFirstScanDoesNotCountAsTheBaseline(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sc, err := s.StartScan(ctx, "192.168.1.0/24")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishScan(ctx, sc.ID, "failed", "boom"); err != nil {
		t.Fatal(err)
	}
	scanWith(t, s, baselineScan()) // the first completed scan
	if got := feed(t, s); len(got) != 0 {
		t.Errorf("changes = %+v, the first completed scan is the baseline", got)
	}
}

func TestAnApplyThatFailsWritesNoChanges(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	scanWith(t, s, baselineScan())

	sc, err := s.StartScan(ctx, "192.168.1.0/24")
	if err != nil {
		t.Fatal(err)
	}
	bad := baselineScan()
	bad.Devices = append(bad.Devices, model.DeviceInput{Source: "arp"}) // no mac and no ip
	if err := s.ApplyResult(ctx, sc.ID, bad); err == nil {
		t.Fatal("a device without an address was applied")
	}
	if got := feed(t, s); len(got) != 0 {
		t.Errorf("changes = %+v: the transaction must roll back", got)
	}
}

func TestChangesAreNewestFirstAndLimited(t *testing.T) {
	s := newTestStore(t)
	scanWith(t, s, baselineScan())
	scanWith(t, s, collector.Result{Devices: []model.DeviceInput{dev(keyA.MAC, keyA.IP)}})
	scanWith(t, s, baselineScan())

	all, err := s.ListChanges(context.Background(), 100)
	if err != nil || len(all) != 2 || all[0].ID < all[1].ID {
		t.Fatalf("changes = %+v, %v", all, err)
	}
	one, err := s.ListChanges(context.Background(), 1)
	if err != nil || len(one) != 1 || one[0].ID != all[0].ID {
		t.Errorf("limit 1 = %+v, %v", one, err)
	}
	empty := newTestStore(t)
	if got, err := empty.ListChanges(context.Background(), 10); err != nil || got == nil || len(got) != 0 {
		t.Errorf("empty = %#v, %v, want an empty list", got, err)
	}
}

func TestLatestChangesBelongToTheLastCompletedScan(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, _, ok, err := s.LatestChanges(ctx); ok || err != nil {
		t.Fatalf("no scans: ok = %v, err = %v", ok, err)
	}
	scanWith(t, s, baselineScan())
	scanWith(t, s, collector.Result{Devices: []model.DeviceInput{dev(keyA.MAC, keyA.IP)}})
	last := scanWith(t, s, collector.Result{Devices: []model.DeviceInput{dev(keyA.MAC, keyA.IP)}}) // no differences

	sc, changes, ok, err := s.LatestChanges(ctx)
	if err != nil || !ok || sc.ID != last.ID {
		t.Fatalf("scan = %+v, ok = %v, err = %v", sc, ok, err)
	}
	if len(changes) != 0 || changes == nil {
		t.Errorf("changes = %#v, the last scan changed nothing", changes)
	}

	// A running or a failed scan is not the last completed one.
	failed, err := s.StartScan(ctx, "192.168.1.0/24")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishScan(ctx, failed.ID, "failed", "x"); err != nil {
		t.Fatal(err)
	}
	if sc, _, _, _ := s.LatestChanges(ctx); sc.ID != last.ID {
		t.Errorf("latest scan = %d, want %d", sc.ID, last.ID)
	}
}

func TestPruneScansKeepsTheNewestAndTakesTheirChangesAlong(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	scanWith(t, s, baselineScan())
	scanWith(t, s, collector.Result{Devices: []model.DeviceInput{dev(keyA.MAC, keyA.IP)}}) // offline B
	scanWith(t, s, baselineScan())                                                           // online B
	last := scanWith(t, s, collector.Result{Devices: []model.DeviceInput{dev(keyA.MAC, keyA.IP)}})
	running, err := s.StartScan(ctx, "192.168.1.0/24")
	if err != nil {
		t.Fatal(err)
	}

	n, err := s.PruneScans(ctx, 2)
	if err != nil || n != 3 {
		t.Fatalf("pruned %d, %v, want 3", n, err)
	}
	scans, err := s.ListScans(ctx, 100)
	if err != nil || len(scans) != 2 || scans[0].ID != running.ID || scans[1].ID != last.ID {
		t.Fatalf("scans = %+v, %v, want the running one and the last", scans, err)
	}
	for _, c := range feed(t, s) {
		if c.ScanID < last.ID {
			t.Errorf("change %+v of a pruned scan is still there", c)
		}
	}
	wantKinds(t, feed(t, s), ChangeDeviceOffline)

	if n, err := s.PruneScans(ctx, 2); err != nil || n != 0 {
		t.Errorf("a second prune removed %d, %v", n, err)
	}
	if n, err := s.PruneScans(ctx, 0); err != nil || n != 0 {
		t.Errorf("keep 0 removed %d, %v: it must delete nothing", n, err)
	}
}

func TestPruneScansNeverDeletesARunningScan(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	running, err := s.StartScan(ctx, "192.168.1.0/24")
	if err != nil {
		t.Fatal(err)
	}
	scanWith(t, s, baselineScan())
	scanWith(t, s, baselineScan())
	if _, err := s.PruneScans(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetScan(ctx, running.ID); err != nil {
		t.Errorf("the running scan was deleted: %v", err)
	}
}
