package store

import (
	"context"
	"errors"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/model"
	dbgen "github.com/FlexEbat/Netscribe/internal/store/db"
)

var bg = context.Background()

func mustCreate(t *testing.T, s *Store, in model.ManualDevice) model.Device {
	t.Helper()
	d, err := s.CreateDevice(bg, in, 0)
	if err != nil {
		t.Fatalf("CreateDevice(%+v): %v", in, err)
	}
	return d
}

func TestManualDeviceWithoutAddressIsListedAndMarkedManual(t *testing.T) {
	s := newTestStore(t)
	d := mustCreate(t, s, model.ManualDevice{Label: "Patch panel A", Kind: model.KindSwitch})
	if !d.Manual || d.Label != "Patch panel A" || d.Kind != model.KindSwitch || !d.KindLocked || d.Version != 1 || d.Source != "manual" {
		t.Errorf("device = %+v", d)
	}
	if got := list(t, s, DeviceFilter{}); len(got) != 1 || got[0].ID != d.ID {
		t.Errorf("list = %+v", got)
	}
}

func TestScansNeverTouchManualDevices(t *testing.T) {
	s := newTestStore(t)
	noAddress := mustCreate(t, s, model.ManualDevice{Label: "Patch panel"})
	withIP := mustCreate(t, s, model.ManualDevice{IP: "192.168.1.50", Label: "Remote office switch", Kind: model.KindSwitch})

	// A scan of the whole subnet sees neither of them.
	scanOnce(t, s, "192.168.1.0/24", dev("aa:bb:cc:dd:ee:01", "192.168.1.5"))
	for _, id := range []int64{noAddress.ID, withIP.ID} {
		got, err := s.GetDevice(bg, id)
		if err != nil || !got.Online {
			t.Errorf("manual device %d after a scan: %+v, %v, want it still online", id, got, err)
		}
	}
	if n := len(list(t, s, DeviceFilter{})); n != 3 {
		t.Errorf("devices = %d, want 3", n)
	}
}

func TestManualAndDiscoveredDeviceWithTheSameIPBecomeOneRow(t *testing.T) {
	s := newTestStore(t)
	manual := mustCreate(t, s, model.ManualDevice{IP: "192.168.1.7", Label: "Print server", Notes: "second floor"})

	found := dev("aa:bb:cc:dd:ee:07", "192.168.1.7")
	found.Hostname, found.Vendor, found.Source = "printsrv", "Acme", "arp"
	scanOnce(t, s, "192.168.1.0/24", found)

	got := list(t, s, DeviceFilter{})
	if len(got) != 1 {
		t.Fatalf("devices = %d, want 1", len(got))
	}
	d := got[0]
	if d.ID != manual.ID || d.MAC != "aa:bb:cc:dd:ee:07" || d.Hostname != "printsrv" || d.Vendor != "Acme" {
		t.Errorf("scan data missing: %+v", d)
	}
	if !d.Manual || d.Label != "Print server" || d.Notes != "second floor" {
		t.Errorf("manual data lost: %+v", d)
	}
}

func TestEditsAndLockedKindSurviveScans(t *testing.T) {
	s := newTestStore(t)
	scanOnce(t, s, "192.168.1.0/24", dev("aa:bb:cc:dd:ee:01", "192.168.1.5"))
	d := list(t, s, DeviceFilter{})[0]

	label, notes, kind := "Office NAS", "bought in 2024", model.KindNAS
	if _, err := s.UpdateDevice(bg, d.ID, model.DeviceEdit{Label: &label, Notes: &notes, Kind: &kind, Version: d.Version}, 0); err != nil {
		t.Fatal(err)
	}

	again := dev("aa:bb:cc:dd:ee:01", "192.168.1.5")
	again.Kind, again.Hostname = model.KindRouter, "scanned-name"
	scanOnce(t, s, "192.168.1.0/24", again)

	got, err := s.GetDevice(bg, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Label != label || got.Notes != notes {
		t.Errorf("label %q, notes %q after a scan", got.Label, got.Notes)
	}
	if got.Kind != model.KindNAS || !got.KindLocked {
		t.Errorf("kind = %s locked = %v, want nas locked", got.Kind, got.KindLocked)
	}
	if got.Hostname != "scanned-name" {
		t.Errorf("hostname = %q, scans still update what people did not set", got.Hostname)
	}
}

func TestUnlockedKindFollowsTheScan(t *testing.T) {
	s := newTestStore(t)
	scanOnce(t, s, "192.168.1.0/24", dev("aa:bb:cc:dd:ee:01", "192.168.1.5"))
	in := dev("aa:bb:cc:dd:ee:01", "192.168.1.5")
	in.Kind = model.KindPrinter
	scanOnce(t, s, "192.168.1.0/24", in)
	if got := list(t, s, DeviceFilter{})[0]; got.Kind != model.KindPrinter || got.KindLocked {
		t.Errorf("kind = %s locked = %v, want printer unlocked", got.Kind, got.KindLocked)
	}
}

func TestEmptyKindUnlocksWithoutChangingIt(t *testing.T) {
	s := newTestStore(t)
	d := mustCreate(t, s, model.ManualDevice{Label: "x", Kind: model.KindServer})
	empty := model.DeviceKind("")
	got, err := s.UpdateDevice(bg, d.ID, model.DeviceEdit{Kind: &empty, Version: d.Version}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != model.KindServer || got.KindLocked {
		t.Errorf("kind = %s locked = %v, want server unlocked", got.Kind, got.KindLocked)
	}
}

func TestStaleVersionIsAConflictAndChangesNothing(t *testing.T) {
	s := newTestStore(t)
	d := mustCreate(t, s, model.ManualDevice{Label: "first"})

	mine, theirs := "mine", "theirs"
	if _, err := s.UpdateDevice(bg, d.ID, model.DeviceEdit{Label: &theirs, Version: d.Version}, 0); err != nil {
		t.Fatal(err)
	}
	_, err := s.UpdateDevice(bg, d.ID, model.DeviceEdit{Label: &mine, Version: d.Version}, 0)
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("error = %v, want ErrVersionConflict", err)
	}
	got, _ := s.GetDevice(bg, d.ID)
	if got.Label != "theirs" || got.Version != d.Version+1 {
		t.Errorf("device = %+v, the other person's edit must stay", got)
	}
}

func TestUpdateAndDeleteOfAMissingDevice(t *testing.T) {
	s := newTestStore(t)
	label := "x"
	if _, err := s.UpdateDevice(bg, 404, model.DeviceEdit{Label: &label, Version: 1}, 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateDevice error = %v, want ErrNotFound", err)
	}
	if err := s.DeleteDevice(bg, 404); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteDevice error = %v, want ErrNotFound", err)
	}
}

func TestCreateDeviceConflicts(t *testing.T) {
	s := newTestStore(t)
	scanOnce(t, s, "192.168.1.0/24", dev("aa:bb:cc:dd:ee:01", "192.168.1.5"))
	mustCreate(t, s, model.ManualDevice{MAC: "AA:BB:CC:DD:EE:99", Label: "x"})
	mustCreate(t, s, model.ManualDevice{IP: "192.168.1.60", Label: "y"})

	for name, in := range map[string]model.ManualDevice{
		"same MAC in another letter case": {MAC: "aa:bb:cc:dd:ee:99", Label: "z"},
		"same MAC as a discovered device": {MAC: "aa:bb:cc:dd:ee:01", Label: "z"},
		"same IP as a manual device":      {IP: "192.168.1.60", Label: "z"},
		"same IP as a discovered device":  {IP: "192.168.1.5", Label: "z"},
	} {
		if _, err := s.CreateDevice(bg, in, 0); !errors.Is(err, ErrConflict) {
			t.Errorf("%s: error = %v, want ErrConflict", name, err)
		}
	}
}

func TestLinksAreStoredInIDOrderAndDrawnOnce(t *testing.T) {
	s := newTestStore(t)
	a := mustCreate(t, s, model.ManualDevice{Label: "a"})
	b := mustCreate(t, s, model.ManualDevice{Label: "b"})

	l, err := s.CreateLink(bg, model.ManualLink{ADeviceID: b.ID, APort: "eth0", BDeviceID: a.ID, BPort: "Gi0/1"})
	if err != nil {
		t.Fatal(err)
	}
	if l.ADeviceID != a.ID || l.APort != "Gi0/1" || l.BDeviceID != b.ID || l.BPort != "eth0" || l.Source != "manual" {
		t.Errorf("link = %+v, want the smaller id first with its own port", l)
	}
	if _, err := s.CreateLink(bg, model.ManualLink{ADeviceID: a.ID, APort: "Gi0/1", BDeviceID: b.ID, BPort: "eth0"}); !errors.Is(err, ErrConflict) {
		t.Errorf("the same link from the other end: error = %v, want ErrConflict", err)
	}
	if _, err := s.CreateLink(bg, model.ManualLink{ADeviceID: a.ID, BDeviceID: 999}); !errors.Is(err, ErrNotFound) {
		t.Errorf("link to a missing device: error = %v, want ErrNotFound", err)
	}
	topo, err := s.Topology(bg)
	if err != nil || len(topo.Links) != 1 {
		t.Errorf("topology links = %+v, %v", topo.Links, err)
	}
}

func TestOnlyManualLinksCanBeDeletedAndScansLeaveThemAlone(t *testing.T) {
	s := newTestStore(t)
	a := mustCreate(t, s, model.ManualDevice{Label: "a"})
	b := mustCreate(t, s, model.ManualDevice{Label: "b"})
	c := mustCreate(t, s, model.ManualDevice{Label: "c"})
	manual, err := s.CreateLink(bg, model.ManualLink{ADeviceID: a.ID, BDeviceID: b.ID})
	if err != nil {
		t.Fatal(err)
	}
	found, err := s.q.InsertLink(bg, dbgen.InsertLinkParams{ADeviceID: b.ID, BDeviceID: c.ID, Source: "lldp", LastSeenAt: formatTime(s.now())})
	if err != nil {
		t.Fatal(err)
	}

	scanOnce(t, s, "192.168.1.0/24", dev("aa:bb:cc:dd:ee:01", "192.168.1.5"))
	if links, _ := s.ListLinks(bg); len(links) != 2 {
		t.Fatalf("links after a scan = %d, want 2", len(links))
	}

	if err := s.DeleteLink(bg, found.ID); !errors.Is(err, ErrNotManual) {
		t.Errorf("deleting a found link: error = %v, want ErrNotManual", err)
	}
	if err := s.DeleteLink(bg, manual.ID); err != nil {
		t.Errorf("deleting a manual link: %v", err)
	}
	if err := s.DeleteLink(bg, manual.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting it again: error = %v, want ErrNotFound", err)
	}
}

func TestDeletingADeviceRemovesItsLinks(t *testing.T) {
	s := newTestStore(t)
	a := mustCreate(t, s, model.ManualDevice{Label: "a"})
	b := mustCreate(t, s, model.ManualDevice{Label: "b"})
	if _, err := s.CreateLink(bg, model.ManualLink{ADeviceID: a.ID, BDeviceID: b.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteDevice(bg, a.ID); err != nil {
		t.Fatal(err)
	}
	if links, _ := s.ListLinks(bg); len(links) != 0 {
		t.Errorf("links = %+v, want none", links)
	}
}

func TestSearchFindsADeviceByItsLabel(t *testing.T) {
	s := newTestStore(t)
	mustCreate(t, s, model.ManualDevice{Label: "Серверная стойка"})
	if got := list(t, s, DeviceFilter{Query: "СТОЙКА"}); len(got) != 1 {
		t.Errorf("found %d devices by label, want 1", len(got))
	}
}
