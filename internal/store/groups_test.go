package store

import (
	"errors"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/model"
)

func mkGroup(t *testing.T, s *Store, name string, vlan *int, ranges ...string) model.Group {
	t.Helper()
	g, err := s.CreateGroup(bg, model.ManualGroup{Name: name, VLAN: vlan, Ranges: ranges})
	if err != nil {
		t.Fatalf("CreateGroup(%s): %v", name, err)
	}
	return g
}

func TestAGroupThatListsTheRangeIsTheEffectiveGroupUntilAPersonChoosesOne(t *testing.T) {
	s := newTestStore(t)
	office := mkGroup(t, s, "Office", ptr(10), "192.168.1.0/24")
	lab := mkGroup(t, s, "Lab", nil)
	scanOnce(t, s, lan, dev("aa:bb:cc:dd:ee:01", "192.168.1.5"))
	d := byMAC(t, s, "aa:bb:cc:dd:ee:01")

	if d.Group == nil || d.Group.ID != office.ID || !d.GroupAuto || d.GroupID != nil || d.Group.VLAN == nil || *d.Group.VLAN != 10 {
		t.Fatalf("by range: group %+v auto %v id %v", d.Group, d.GroupAuto, d.GroupID)
	}

	d, err := s.UpdateDevice(bg, d.ID, model.DeviceEdit{GroupID: &lab.ID, Version: d.Version}, 0)
	if err != nil || d.Group == nil || d.Group.ID != lab.ID || d.GroupAuto || d.GroupID == nil || *d.GroupID != lab.ID {
		t.Fatalf("chosen: %+v, %v", d, err)
	}

	zero := int64(0)
	d, err = s.UpdateDevice(bg, d.ID, model.DeviceEdit{GroupID: &zero, Version: d.Version}, 0)
	if err != nil || d.Group == nil || d.Group.ID != office.ID || !d.GroupAuto || d.GroupID != nil {
		t.Fatalf("cleared: %+v, %v, want the range again", d, err)
	}
}

func TestWithOverlappingRangesTheLowerIdWinsAndDeletingItHandsTheDeviceOver(t *testing.T) {
	s := newTestStore(t)
	wide := mkGroup(t, s, "Wide", nil, "192.168.0.0/16")
	narrow := mkGroup(t, s, "Narrow", nil, "192.168.1.0/24")
	scanOnce(t, s, lan, dev("aa:bb:cc:dd:ee:01", "192.168.1.5"))

	if d := byMAC(t, s, "aa:bb:cc:dd:ee:01"); d.Group == nil || d.Group.ID != wide.ID {
		t.Fatalf("group = %+v, want the lower id (Wide)", d.Group)
	}
	if err := s.DeleteGroup(bg, wide.ID); err != nil {
		t.Fatal(err)
	}
	if d := byMAC(t, s, "aa:bb:cc:dd:ee:01"); d.Group == nil || d.Group.ID != narrow.ID {
		t.Fatalf("after deleting Wide: %+v, want Narrow", d.Group)
	}
}

func TestDeletingAGroupKeepsItsDevicesAndClearsTheirChoice(t *testing.T) {
	s := newTestStore(t)
	g := mkGroup(t, s, "Lab", nil)
	scanOnce(t, s, lan, dev("aa:bb:cc:dd:ee:01", "10.9.9.9"))
	d := byMAC(t, s, "aa:bb:cc:dd:ee:01")
	if _, err := s.UpdateDevice(bg, d.ID, model.DeviceEdit{GroupID: &g.ID, Version: d.Version}, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteGroup(bg, g.ID); err != nil {
		t.Fatal(err)
	}
	d = byMAC(t, s, "aa:bb:cc:dd:ee:01")
	if d.Group != nil || d.GroupID != nil {
		t.Errorf("after deleting its group: %+v, %v", d.Group, d.GroupID)
	}
	if err := s.DeleteGroup(bg, g.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: %v, want ErrNotFound", err)
	}
}

func TestAnUnknownGroupIsRefusedOnADeviceEdit(t *testing.T) {
	s := newTestStore(t)
	scanOnce(t, s, lan, dev("aa:bb:cc:dd:ee:01", "192.168.1.5"))
	d := byMAC(t, s, "aa:bb:cc:dd:ee:01")
	if _, err := s.UpdateDevice(bg, d.ID, model.DeviceEdit{GroupID: ptr(int64(999)), Version: d.Version}, 0); !errors.Is(err, ErrNoSuchGroup) {
		t.Fatalf("err = %v, want ErrNoSuchGroup", err)
	}
	if got := byMAC(t, s, "aa:bb:cc:dd:ee:01"); got.Version != d.Version {
		t.Errorf("a refused edit changed the device: version %d -> %d", d.Version, got.Version)
	}
}

func TestGroupCRUD(t *testing.T) {
	s := newTestStore(t)
	g := mkGroup(t, s, "Office", ptr(10), "192.168.1.0/24")
	if g.Version != 1 || g.VLAN == nil || *g.VLAN != 10 || len(g.Ranges) != 1 {
		t.Fatalf("created: %+v", g)
	}
	if _, err := s.CreateGroup(bg, model.ManualGroup{Name: "OFFICE"}); !errors.Is(err, ErrConflict) {
		t.Errorf("same name in capitals: %v, want ErrConflict", err)
	}

	name := "HQ"
	got, err := s.UpdateGroup(bg, g.ID, model.GroupEdit{Name: &name, VLAN: ptr(0), Version: 1})
	if err != nil || got.Name != "HQ" || got.VLAN != nil || got.Version != 2 || len(got.Ranges) != 1 {
		t.Fatalf("update: %+v, %v (a vlan of 0 removes the number, other fields stay)", got, err)
	}
	if _, err := s.UpdateGroup(bg, g.ID, model.GroupEdit{Name: &name, Version: 1}); !errors.Is(err, ErrVersionConflict) {
		t.Errorf("stale update: %v, want ErrVersionConflict", err)
	}
	other := mkGroup(t, s, "Lab", nil)
	taken := "hq"
	if _, err := s.UpdateGroup(bg, other.ID, model.GroupEdit{Name: &taken, Version: 1}); !errors.Is(err, ErrConflict) {
		t.Errorf("rename to a taken name: %v, want ErrConflict", err)
	}
	if _, err := s.UpdateGroup(bg, 999, model.GroupEdit{Version: 1}); !errors.Is(err, ErrNotFound) {
		t.Errorf("update of a missing group: %v", err)
	}
	if _, err := s.GetGroup(bg, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("get of a missing group: %v", err)
	}
}

func TestListGroupsCountsEffectiveDevicesAndSortsByName(t *testing.T) {
	s := newTestStore(t)
	mkGroup(t, s, "Zeta", nil, "192.168.1.0/24")
	b := mkGroup(t, s, "alpha", nil)
	scanOnce(t, s, lan, dev("aa:bb:cc:dd:ee:01", "192.168.1.5"), dev("aa:bb:cc:dd:ee:02", "192.168.1.6"), dev("aa:bb:cc:dd:ee:03", "10.0.0.1"))
	d := byMAC(t, s, "aa:bb:cc:dd:ee:03")
	if _, err := s.UpdateDevice(bg, d.ID, model.DeviceEdit{GroupID: &b.ID, Version: d.Version}, 0); err != nil {
		t.Fatal(err)
	}
	groups, err := s.ListGroups(bg)
	if err != nil || len(groups) != 2 || groups[0].Name != "alpha" || groups[1].Name != "Zeta" {
		t.Fatalf("groups = %+v, %v, want alpha then Zeta", groups, err)
	}
	if groups[0].DeviceCount != 1 || groups[1].DeviceCount != 2 {
		t.Errorf("counts = %d and %d, want 1 and 2", groups[0].DeviceCount, groups[1].DeviceCount)
	}
}

func TestListDevicesFiltersByEffectiveGroup(t *testing.T) {
	s := newTestStore(t)
	g := mkGroup(t, s, "Office", nil, "192.168.1.0/24")
	scanOnce(t, s, lan, dev("aa:bb:cc:dd:ee:01", "192.168.1.5"), dev("aa:bb:cc:dd:ee:02", "10.0.0.1"))

	if got := list(t, s, DeviceFilter{Group: &g.ID}); len(got) != 1 || got[0].MAC != "aa:bb:cc:dd:ee:01" {
		t.Errorf("group filter = %+v", got)
	}
	if got := list(t, s, DeviceFilter{Group: ptr(int64(0))}); len(got) != 1 || got[0].MAC != "aa:bb:cc:dd:ee:02" {
		t.Errorf("none filter = %+v", got)
	}
}
