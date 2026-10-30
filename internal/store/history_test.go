package store

import (
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
)

func historyOf(t *testing.T, s *Store, mac string) model.DeviceHistory {
	t.Helper()
	h, err := s.DeviceHistory(bg, byMAC(t, s, mac).ID)
	if err != nil {
		t.Fatalf("DeviceHistory(%s): %v", mac, err)
	}
	return h
}

func TestPresenceIsWrittenForScannedDevicesOnly(t *testing.T) {
	s := newTestStore(t)
	manual := mustCreate(t, s, model.ManualDevice{Label: "Patch panel"})
	a, b := dev("aa:bb:cc:dd:ee:01", "192.168.1.5"), dev("aa:bb:cc:dd:ee:02", "192.168.1.6")
	scanOnce(t, s, lan, a)
	scanOnce(t, s, lan, a)
	scanOnce(t, s, lan, a, b)
	scanOnce(t, s, lan, a, b)

	if h := historyOf(t, s, "aa:bb:cc:dd:ee:01"); h.Scans != 4 || h.Online != 4 || h.Uptime == nil || *h.Uptime != 100 {
		t.Errorf("always-there device: %+v", h)
	}
	if h := historyOf(t, s, "aa:bb:cc:dd:ee:02"); h.Scans != 2 {
		t.Errorf("a device added in the 3rd of 4 scans has scans = %d, want 2", h.Scans)
	}
	h, err := s.DeviceHistory(bg, manual.ID)
	if err != nil || h.Scans != 0 || h.Uptime != nil {
		t.Errorf("manual device: %+v, %v, want no scans and no uptime", h, err)
	}
}

func TestAFailedScanWritesNoPresence(t *testing.T) {
	s := newTestStore(t)
	scanOnce(t, s, lan, dev("aa:bb:cc:dd:ee:01", "192.168.1.5"))
	failed, err := s.StartScan(bg, lan)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishScan(bg, failed.ID, "failed", "arp: no interface"); err != nil {
		t.Fatal(err)
	}
	if h := historyOf(t, s, "aa:bb:cc:dd:ee:01"); h.Scans != 1 {
		t.Errorf("scans = %d after a failed scan, want 1", h.Scans)
	}
}

func TestAMissedScanIsAnOutageThatClosesWhenTheDeviceReturns(t *testing.T) {
	s := newTestStore(t)
	a, b := dev("aa:bb:cc:dd:ee:01", "192.168.1.5"), dev("aa:bb:cc:dd:ee:02", "192.168.1.6")
	scanOnce(t, s, lan, a, b)
	scanOnce(t, s, lan, a)

	h := historyOf(t, s, "aa:bb:cc:dd:ee:02")
	if h.Scans != 2 || h.Online != 1 || *h.Uptime != 50 || len(h.Outages) != 1 || h.Outages[0].To != nil {
		t.Fatalf("while away: %+v", h)
	}
	scanOnce(t, s, lan, a, b)
	h = historyOf(t, s, "aa:bb:cc:dd:ee:02")
	if len(h.Outages) != 1 || h.Outages[0].To == nil {
		t.Errorf("after the return: %+v, want one closed outage", h.Outages)
	}
}

func TestAChangedAddressShowsAsTwoSpans(t *testing.T) {
	s := newTestStore(t)
	scanOnce(t, s, lan, dev("aa:bb:cc:dd:ee:01", "192.168.1.5"))
	scanOnce(t, s, lan, dev("aa:bb:cc:dd:ee:01", "192.168.1.77"))
	h := historyOf(t, s, "aa:bb:cc:dd:ee:01")
	if len(h.IPs) != 2 || h.IPs[0].IP != "192.168.1.77" || h.IPs[1].IP != "192.168.1.5" {
		t.Errorf("ips = %+v, want the new address first", h.IPs)
	}
}

func TestPruningAScanTakesItsPresenceAlong(t *testing.T) {
	s := newTestStore(t)
	for i := 0; i < 4; i++ {
		scanOnce(t, s, lan, dev("aa:bb:cc:dd:ee:01", "192.168.1.5"))
	}
	if _, err := s.PruneScans(bg, 2); err != nil {
		t.Fatal(err)
	}
	if h := historyOf(t, s, "aa:bb:cc:dd:ee:01"); h.Scans != 2 {
		t.Errorf("scans = %d after keeping 2, want 2", h.Scans)
	}
}

func TestHistoryOfAMissingDeviceIsNotFound(t *testing.T) {
	if _, err := newTestStore(t).DeviceHistory(bg, 999); err != model.ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestOverviewOfAnEmptyDatabaseIsAllZeros(t *testing.T) {
	o, err := newTestStore(t).Overview(bg)
	if err != nil {
		t.Fatal(err)
	}
	if (o.Devices != model.OverviewDevices{}) || o.Risky != 0 || o.LastScan != nil || (o.RecentScans != model.ScanHealth{}) || o.Changes == nil || len(o.Changes) != 0 {
		t.Errorf("overview = %+v", o)
	}
}

func TestOverviewCounts(t *testing.T) {
	s := newTestStore(t)
	mustCreate(t, s, model.ManualDevice{Label: "Patch panel"})
	a, b := dev("aa:bb:cc:dd:ee:01", "192.168.1.5"), dev("aa:bb:cc:dd:ee:02", "192.168.1.6")
	scanOnce(t, s, lan, a, b)
	scanOnce(t, s, lan, a) // b goes offline and a change is written
	failed, err := s.StartScan(bg, lan)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishScan(bg, failed.ID, "failed", "boom"); err != nil {
		t.Fatal(err)
	}
	yes := true
	one := byMAC(t, s, "aa:bb:cc:dd:ee:01")
	if _, err := s.UpdateDevice(bg, one.ID, model.DeviceEdit{Trusted: &yes, Version: one.Version}, 0); err != nil {
		t.Fatal(err)
	}

	o, err := s.Overview(bg)
	if err != nil {
		t.Fatal(err)
	}
	want := model.OverviewDevices{Total: 3, Online: 2, Offline: 1, Unknown: 1, NewThisWeek: 2}
	if o.Devices != want {
		t.Errorf("devices = %+v, want %+v", o.Devices, want)
	}
	if o.LastScan == nil || o.LastScan.Status != "failed" || o.RecentScans != (model.ScanHealth{Total: 3, Failed: 1}) {
		t.Errorf("scans: last %+v, health %+v", o.LastScan, o.RecentScans)
	}
	if len(o.Changes) == 0 {
		t.Error("the change of the second scan is missing")
	}
}

func TestNewThisWeekHasAnEdgeOfSevenDays(t *testing.T) {
	s := newTestStore(t)
	scanOnce(t, s, lan, dev("aa:bb:cc:dd:ee:01", "192.168.1.5"))
	first := byMAC(t, s, "aa:bb:cc:dd:ee:01").FirstSeenAt

	s.SetClock(func() time.Time { return first.Add(6*24*time.Hour + time.Hour) })
	if o, _ := s.Overview(bg); o.Devices.NewThisWeek != 1 {
		t.Errorf("6 days later: %d, want 1", o.Devices.NewThisWeek)
	}
	s.SetClock(func() time.Time { return first.Add(8 * 24 * time.Hour) })
	if o, _ := s.Overview(bg); o.Devices.NewThisWeek != 0 {
		t.Errorf("8 days later: %d, want 0", o.Devices.NewThisWeek)
	}
}
