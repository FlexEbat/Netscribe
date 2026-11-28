package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/model"
)

func detailsOf() model.Details {
	return model.Details{
		Owner: "Ana", Location: "Rack 4", Serial: "SN-1", WarrantyUntil: "2028-02-29",
		Tags:   []string{"core", "lab"},
		Fields: []model.DeviceField{{Name: "VLAN", Value: "20"}, {Name: "Asset", Value: "A-77"}},
	}
}

func TestCreateKeepsTheDetailsAndTheOrderOfTheFields(t *testing.T) {
	s := newTestStore(t)
	d := mustCreate(t, s, model.ManualDevice{Label: "Core switch", Details: detailsOf()})
	if d.Owner != "Ana" || d.Location != "Rack 4" || d.Serial != "SN-1" || d.WarrantyUntil != "2028-02-29" {
		t.Errorf("device = %+v", d)
	}
	if !slices.Equal(d.Tags, []string{"core", "lab"}) {
		t.Errorf("tags = %v", d.Tags)
	}
	if len(d.Fields) != 2 || d.Fields[0].Name != "VLAN" || d.Fields[1].Name != "Asset" {
		t.Errorf("fields = %v, want them in the order they were made", d.Fields)
	}
	got, err := s.GetDevice(bg, d.ID)
	if err != nil || !slices.Equal(got.Tags, d.Tags) || len(got.Fields) != 2 || got.Fields[0].Name != "VLAN" {
		t.Errorf("GetDevice = %+v, %v", got, err)
	}
}

func TestADeviceWithoutDetailsHasEmptyListsNotNil(t *testing.T) {
	s := newTestStore(t)
	d := mustCreate(t, s, model.ManualDevice{Label: "Plain"})
	if d.Tags == nil || d.Fields == nil {
		t.Errorf("created: tags %#v, fields %#v, want empty lists", d.Tags, d.Fields)
	}
	scanOnce(t, s, "192.168.1.0/24", dev("aa:bb:cc:dd:ee:01", "192.168.1.5"))
	for _, x := range list(t, s, DeviceFilter{}) {
		if x.Tags == nil || x.Fields == nil {
			t.Errorf("device %d: tags %#v, fields %#v, want empty lists", x.ID, x.Tags, x.Fields)
		}
	}
}

// Acceptance 2: a scan never writes the details of a device.
func TestAScanNeverChangesTheDetails(t *testing.T) {
	s := newTestStore(t)
	scanOnce(t, s, "192.168.1.0/24", dev("aa:bb:cc:dd:ee:01", "192.168.1.5"))
	d := byMAC(t, s, "aa:bb:cc:dd:ee:01")
	e := model.DeviceEdit{
		Owner: ptr("Ana"), Location: ptr("Rack 4"), Serial: ptr("SN-1"), WarrantyUntil: ptr("2028-02-29"),
		Tags: ptr([]string{"core"}), Fields: ptr([]model.DeviceField{{Name: "VLAN", Value: "20"}}), Version: d.Version,
	}
	if _, err := s.UpdateDevice(bg, d.ID, e, 0); err != nil {
		t.Fatal(err)
	}
	// The same network again, then the device missing, then back.
	scanOnce(t, s, "192.168.1.0/24", dev("aa:bb:cc:dd:ee:01", "192.168.1.5"))
	scanOnce(t, s, "192.168.1.0/24")
	scanOnce(t, s, "192.168.1.0/24", dev("aa:bb:cc:dd:ee:01", "192.168.1.5"))
	got, err := s.GetDevice(bg, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Owner != "Ana" || got.Location != "Rack 4" || got.Serial != "SN-1" || got.WarrantyUntil != "2028-02-29" ||
		!slices.Equal(got.Tags, []string{"core"}) || len(got.Fields) != 1 || got.Fields[0].Value != "20" {
		t.Errorf("after scans: %+v", got)
	}
}

func TestUpdateReplacesTagsAndFieldsAndLeavesUntouchedListsAlone(t *testing.T) {
	s := newTestStore(t)
	d := mustCreate(t, s, model.ManualDevice{Label: "X", Details: detailsOf()})
	got, err := s.UpdateDevice(bg, d.ID, model.DeviceEdit{Tags: ptr([]string{"edge"}), Version: d.Version}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Tags, []string{"edge"}) || len(got.Fields) != 2 || got.Owner != "Ana" {
		t.Errorf("after a tags edit: %+v", got)
	}
	if got.Version != d.Version+1 {
		t.Errorf("version = %d, want %d", got.Version, d.Version+1)
	}
	got, err = s.UpdateDevice(bg, d.ID, model.DeviceEdit{Tags: ptr([]string{}), Fields: ptr([]model.DeviceField{}), Owner: ptr(""), Version: got.Version}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tags) != 0 || len(got.Fields) != 0 || got.Owner != "" || got.Location != "Rack 4" {
		t.Errorf("after clearing: %+v", got)
	}
}

// Acceptance 4 for this part: an edit that fails leaves the device as it was.
func TestAStaleEditChangesNeitherTheColumnsNorTheLists(t *testing.T) {
	s := newTestStore(t)
	d := mustCreate(t, s, model.ManualDevice{Label: "X", Details: detailsOf()})
	_, err := s.UpdateDevice(bg, d.ID, model.DeviceEdit{
		Owner: ptr("Someone else"), Tags: ptr([]string{"new"}), Fields: ptr([]model.DeviceField{{Name: "n", Value: "v"}}), Version: d.Version + 5,
	}, 0)
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("error = %v, want ErrVersionConflict", err)
	}
	got, err := s.GetDevice(bg, d.ID)
	if err != nil || got.Owner != "Ana" || !slices.Equal(got.Tags, []string{"core", "lab"}) || len(got.Fields) != 2 || got.Version != d.Version {
		t.Errorf("after the refused edit: %+v, %v", got, err)
	}
}

func TestDeletingADeviceDeletesItsTagsAndFields(t *testing.T) {
	s := newTestStore(t)
	d := mustCreate(t, s, model.ManualDevice{Label: "X", Details: detailsOf()})
	if err := s.DeleteDevice(bg, d.ID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"device_tags", "device_fields"} {
		var n int
		if err := s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s holds %d rows after the delete, %v", table, n, err)
		}
	}
}

func TestSearchAndTagFilterSeeTheDetails(t *testing.T) {
	s := newTestStore(t)
	a := mustCreate(t, s, model.ManualDevice{Label: "A", Details: model.Details{Owner: "Ivanova", Location: "Basement", Serial: "ZX-900", Tags: []string{"core"}, Fields: []model.DeviceField{{Name: "Contract", Value: "Acme-2027"}}}})
	b := mustCreate(t, s, model.ManualDevice{Label: "B", Details: model.Details{Tags: []string{"lab", "core"}}})
	c := mustCreate(t, s, model.ManualDevice{Label: "C"})
	ids := func(f DeviceFilter) []int64 {
		var out []int64
		for _, d := range list(t, s, f) {
			out = append(out, d.ID)
		}
		slices.Sort(out)
		return out
	}
	for q, want := range map[string][]int64{
		"ivanova": {a.ID}, "BASEMENT": {a.ID}, "zx-9": {a.ID}, "acme-2027": {a.ID}, "lab": {b.ID}, "core": {a.ID, b.ID}, "nothing like it": nil,
	} {
		if got := ids(DeviceFilter{Query: q}); !slices.Equal(got, want) {
			t.Errorf("q=%q: %v, want %v", q, got, want)
		}
	}
	// The name of a field is not searched, only its value.
	if got := ids(DeviceFilter{Query: "contract"}); len(got) != 0 {
		t.Errorf("q=contract matched %v", got)
	}
	if got := ids(DeviceFilter{Tag: "core"}); !slices.Equal(got, []int64{a.ID, b.ID}) {
		t.Errorf("tag=core: %v", got)
	}
	if got := ids(DeviceFilter{Tag: "lab"}); !slices.Equal(got, []int64{b.ID}) {
		t.Errorf("tag=lab: %v", got)
	}
	if got := ids(DeviceFilter{Tag: "lab", Query: "core"}); !slices.Equal(got, []int64{b.ID}) {
		t.Errorf("tag=lab with q=core: %v", got)
	}
	_ = c
}

func TestBulkEditAddsAndRemovesTags(t *testing.T) {
	s := newTestStore(t)
	a := mustCreate(t, s, model.ManualDevice{Label: "A", Details: model.Details{Tags: []string{"old", "keep"}}})
	b := mustCreate(t, s, model.ManualDevice{Label: "B"})
	c := mustCreate(t, s, model.ManualDevice{Label: "C", Details: model.Details{Tags: []string{"old"}}})
	n, err := s.BulkEdit(bg, []int64{a.ID, b.ID}, BulkChange{AddTags: []string{"new", "keep"}, RemoveTags: []string{"old"}}, 0)
	if err != nil || n != 2 {
		t.Fatalf("BulkEdit = %d, %v", n, err)
	}
	for id, want := range map[int64][]string{a.ID: {"keep", "new"}, b.ID: {"keep", "new"}, c.ID: {"old"}} {
		got, err := s.GetDevice(bg, id)
		if err != nil || !slices.Equal(got.Tags, want) {
			t.Errorf("device %d tags = %v, %v; want %v", id, got.Tags, err, want)
		}
	}
	if got, _ := s.GetDevice(bg, c.ID); got.Version != c.Version {
		t.Errorf("an unlisted device was touched: version %d", got.Version)
	}
	if got, _ := s.GetDevice(bg, a.ID); got.Version != a.Version+1 {
		t.Errorf("a listed device: version %d, want %d", got.Version, a.Version+1)
	}
}

func TestBulkEditRefusesAThirteenthTagAndChangesNothing(t *testing.T) {
	s := newTestStore(t)
	var twelve []string
	for i := 0; i < model.MaxTags; i++ {
		twelve = append(twelve, fmt.Sprintf("t%d", i))
	}
	full := mustCreate(t, s, model.ManualDevice{Label: "Full", Details: model.Details{Tags: twelve}})
	room := mustCreate(t, s, model.ManualDevice{Label: "Room"})
	_, err := s.BulkEdit(bg, []int64{room.ID, full.ID}, BulkChange{AddTags: []string{"extra"}}, 0)
	if !errors.Is(err, model.ErrTooManyTags) {
		t.Fatalf("error = %v, want ErrTooManyTags", err)
	}
	if got, _ := s.GetDevice(bg, room.ID); len(got.Tags) != 0 {
		t.Errorf("the device with room got %v although the bulk edit failed", got.Tags)
	}
	// Adding a tag the device already has is not a thirteenth tag.
	if _, err := s.BulkEdit(bg, []int64{full.ID}, BulkChange{AddTags: []string{"t0"}}, 0); err != nil {
		t.Errorf("a tag it has already: %v", err)
	}
}

// Acceptance 6: tags and fields do not change how many queries a read sends.
func TestTagsAndFieldsDoNotGrowTheQueriesOfARead(t *testing.T) {
	ctx := context.Background()
	counts := func(n int) map[string]int64 {
		s, c := networkOf(t, n)
		devices, err := s.ListDevices(ctx, DeviceFilter{})
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range devices {
			e := model.DeviceEdit{Tags: ptr([]string{"a", "b"}), Fields: ptr([]model.DeviceField{{Name: "n", Value: "v"}}), Owner: ptr("o"), Version: d.Version}
			if _, err := s.UpdateDevice(ctx, d.ID, e, 0); err != nil {
				t.Fatal(err)
			}
		}
		out := map[string]int64{}
		for name, read := range map[string]func() error{
			"ListDevices":      func() error { _, err := s.ListDevices(ctx, DeviceFilter{Query: "v", Tag: "a"}); return err },
			"Topology":         func() error { _, err := s.Topology(ctx); return err },
			"GetDevice":        func() error { _, err := s.GetDevice(ctx, devices[0].ID); return err },
			"ListDevicesPlain": func() error { _, err := s.ListDevices(ctx, DeviceFilter{}); return err },
		} {
			before := c.n.Load()
			if err := read(); err != nil {
				t.Fatalf("%s at %d devices: %v", name, n, err)
			}
			out[name] = c.n.Load() - before
		}
		return out
	}
	small, large := counts(30), counts(300)
	for name, n := range small {
		if n == 0 || large[name] != n {
			t.Errorf("%s: %d queries at 30 devices, %d at 300; want the same, not zero", name, n, large[name])
		}
	}
}
