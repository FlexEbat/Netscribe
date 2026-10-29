package store

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/FlexEbat/Netscribe/internal/model"
)

func TestMigrationTrustsTheDevicesThatExistedBeforeIt(t *testing.T) {
	db, err := openDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	all, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	before := fstest.MapFS{}
	entries, err := fs.ReadDir(all, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "0011_") {
			continue
		}
		data, err := fs.ReadFile(all, e.Name())
		if err != nil {
			t.Fatal(err)
		}
		before[e.Name()] = &fstest.MapFile{Data: data}
	}
	if err := migrate(bg, db, before); err != nil {
		t.Fatalf("migrate to 0010: %v", err)
	}
	if _, err := db.ExecContext(bg, `INSERT INTO devices (mac, ip, source, first_seen_at, last_seen_at)
		VALUES ('aa:bb:cc:dd:ee:01', '192.168.1.5', 'arp', '2026-10-01T00:00:00Z', '2026-10-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	if err := migrate(bg, db, all); err != nil {
		t.Fatalf("migrate to the latest: %v", err)
	}
	if got := scanString(t, db.QueryRowContext(bg, "SELECT CAST(trusted AS TEXT) FROM devices")); got != "1" {
		t.Errorf("trusted of the old device = %s, want 1", got)
	}
	if _, err := db.ExecContext(bg, `INSERT INTO devices (mac, ip, source, first_seen_at, last_seen_at)
		VALUES ('aa:bb:cc:dd:ee:02', '192.168.1.6', 'arp', '2026-10-02T00:00:00Z', '2026-10-02T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if got := scanString(t, db.QueryRowContext(bg, "SELECT CAST(trusted AS TEXT) FROM devices WHERE ip = '192.168.1.6'")); got != "0" {
		t.Errorf("trusted of a device added after the migration = %s, want 0", got)
	}
}

func TestScanCreatesUntrustedDevicesAndManualDevicesAreTrusted(t *testing.T) {
	s := newTestStore(t)
	manual := mustCreate(t, s, model.ManualDevice{Label: "Patch panel"})
	scanOnce(t, s, "192.168.1.0/24", dev("aa:bb:cc:dd:ee:01", "192.168.1.5"))

	if !manual.Trusted {
		t.Errorf("manual device is not trusted: %+v", manual)
	}
	if found := byMAC(t, s, "aa:bb:cc:dd:ee:01"); found.Trusted {
		t.Errorf("a device found by a scan is trusted: %+v", found)
	}
}

func TestTrustSurvivesLaterScansAndEditsThatDoNotSendIt(t *testing.T) {
	s := newTestStore(t)
	scanOnce(t, s, "192.168.1.0/24", dev("aa:bb:cc:dd:ee:01", "192.168.1.5"))
	d := byMAC(t, s, "aa:bb:cc:dd:ee:01")

	yes := true
	d, err := s.UpdateDevice(bg, d.ID, model.DeviceEdit{Trusted: &yes, Version: d.Version}, 0)
	if err != nil || !d.Trusted {
		t.Fatalf("trust: %+v, %v", d, err)
	}
	scanOnce(t, s, "192.168.1.0/24", dev("aa:bb:cc:dd:ee:01", "192.168.1.5"))
	label := "printer"
	d, err = s.UpdateDevice(bg, d.ID, model.DeviceEdit{Label: &label, Version: d.Version}, 0)
	if err != nil || !d.Trusted || d.Label != "printer" {
		t.Errorf("after a scan and a label edit: %+v, %v, want it still trusted", d, err)
	}

	no := false
	if _, err := s.UpdateDevice(bg, d.ID, model.DeviceEdit{Trusted: &no, Version: d.Version - 1}, 0); err != ErrVersionConflict {
		t.Fatalf("stale edit: %v, want ErrVersionConflict", err)
	}
	if got, _ := s.GetDevice(bg, d.ID); !got.Trusted {
		t.Errorf("a refused edit changed trusted: %+v", got)
	}
}

func TestListDevicesFiltersByTrust(t *testing.T) {
	s := newTestStore(t)
	mustCreate(t, s, model.ManualDevice{Label: "known"})
	scanOnce(t, s, "192.168.1.0/24", dev("aa:bb:cc:dd:ee:01", "192.168.1.5"))

	yes, no := true, false
	if got := list(t, s, DeviceFilter{Trusted: &yes}); len(got) != 1 || got[0].Label != "known" {
		t.Errorf("trusted = %+v", got)
	}
	if got := list(t, s, DeviceFilter{Trusted: &no}); len(got) != 1 || got[0].MAC != "aa:bb:cc:dd:ee:01" {
		t.Errorf("untrusted = %+v", got)
	}
	if got := list(t, s, DeviceFilter{}); len(got) != 2 {
		t.Errorf("unfiltered = %d devices, want 2", len(got))
	}
}
