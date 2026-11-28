package model

import (
	"strings"
	"testing"
)

func TestCheckTag(t *testing.T) {
	good := map[string]string{"Core": "core", "  rack 4 ": "rack 4", "a_b-c": "a_b-c", "9": "9", strings.Repeat("x", 30): strings.Repeat("x", 30)}
	for in, want := range good {
		if got, err := CheckTag(in); err != nil || got != want {
			t.Errorf("CheckTag(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "   ", strings.Repeat("x", 31), "a.b", "é", "a\tb", "a/b", "<b>"} {
		if _, err := CheckTag(in); err != ErrTagInvalid {
			t.Errorf("CheckTag(%q) error = %v, want ErrTagInvalid", in, err)
		}
	}
}

func TestCheckTags(t *testing.T) {
	got, err := CheckTags([]string{"B", "a", "b", " A "})
	if err != nil || len(got) != 2 || got[0] != "b" || got[1] != "a" {
		t.Fatalf("CheckTags = %v, %v; want [b a] with the repeats gone", got, err)
	}
	if got, err := CheckTags(nil); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("CheckTags(nil) = %#v, %v; want an empty list that is not nil", got, err)
	}
	twelve := make([]string, 12)
	for i := range twelve {
		twelve[i] = string(rune('a' + i))
	}
	if _, err := CheckTags(twelve); err != nil {
		t.Fatalf("12 tags: %v", err)
	}
	if _, err := CheckTags(append(twelve, "m")); err != ErrTooManyTags {
		t.Fatalf("13 tags: %v, want ErrTooManyTags", err)
	}
	// Twelve different tags with repeats among them are still twelve.
	if _, err := CheckTags(append(twelve, "a", "b")); err != nil {
		t.Fatalf("12 tags and 2 repeats: %v", err)
	}
	if _, err := CheckTags([]string{"ok", "no!"}); err != ErrTagInvalid {
		t.Fatalf("a bad tag: %v", err)
	}
}

func TestCheckFields(t *testing.T) {
	got, err := CheckFields([]DeviceField{{" Rack ", " 4\x07 "}, {"Port", ""}})
	if err != nil || len(got) != 2 || got[0] != (DeviceField{"Rack", "4"}) || got[1].Value != "" {
		t.Fatalf("CheckFields = %v, %v", got, err)
	}
	if got, err := CheckFields(nil); err != nil || got == nil {
		t.Fatalf("CheckFields(nil) = %#v, %v; want an empty list that is not nil", got, err)
	}
	cases := []struct {
		name string
		in   []DeviceField
		want error
	}{
		{"empty name", []DeviceField{{"  ", "x"}}, ErrFieldName},
		{"long name", []DeviceField{{strings.Repeat("n", 31), "x"}}, ErrFieldName},
		{"long value", []DeviceField{{"n", strings.Repeat("v", 201)}}, ErrFieldValue},
		{"same name", []DeviceField{{"n", "1"}, {"n", "2"}}, ErrFieldTwice},
		{"eleven fields", func() []DeviceField {
			var l []DeviceField
			for i := 0; i < 11; i++ {
				l = append(l, DeviceField{Name: string(rune('a' + i))})
			}
			return l
		}(), ErrTooManyFields},
	}
	for _, c := range cases {
		if _, err := CheckFields(c.in); err != c.want {
			t.Errorf("%s: error = %v, want %v", c.name, err, c.want)
		}
	}
	if _, err := CheckFields([]DeviceField{{strings.Repeat("n", 30), strings.Repeat("v", 200)}}); err != nil {
		t.Errorf("a field at its limits: %v", err)
	}
}

func TestCheckTexts(t *testing.T) {
	if got, err := CheckOwner("  Ana\x00 "); err != nil || got != "Ana" {
		t.Errorf("CheckOwner = %q, %v", got, err)
	}
	if _, err := CheckOwner(strings.Repeat("o", 101)); err != ErrOwnerLong {
		t.Errorf("long owner: %v", err)
	}
	if _, err := CheckOwner(strings.Repeat("ё", 100)); err != nil {
		t.Errorf("100 runes of owner: %v", err)
	}
	if _, err := CheckLocation(strings.Repeat("l", 101)); err != ErrLocationLong {
		t.Errorf("long location: %v", err)
	}
	if _, err := CheckSerial(strings.Repeat("s", 65)); err != ErrSerialLong {
		t.Errorf("long serial: %v", err)
	}
	if _, err := CheckSerial(strings.Repeat("s", 64)); err != nil {
		t.Errorf("64 characters of serial: %v", err)
	}
}

func TestCheckWarranty(t *testing.T) {
	for _, in := range []string{"", "  ", "2026-11-28", "2028-02-29"} {
		if _, err := CheckWarranty(in); err != nil {
			t.Errorf("CheckWarranty(%q) = %v", in, err)
		}
	}
	for _, in := range []string{"2026-13-01", "2027-02-29", "28.11.2026", "2026-1-5", "2026-11-28T00:00:00Z", "soon"} {
		if _, err := CheckWarranty(in); err != ErrWarranty {
			t.Errorf("CheckWarranty(%q) = %v, want ErrWarranty", in, err)
		}
	}
}

func TestSavedFilterKeepsTheTag(t *testing.T) {
	got, err := CheckFilterQuery(PageInventory, "tag=Core&q=x")
	if err != nil || got != "q=x&tag=Core" {
		t.Fatalf("CheckFilterQuery = %q, %v", got, err)
	}
	if _, err := CheckFilterQuery(PageInventory, "tag=a.b"); err == nil {
		t.Fatal("a tag with a dot was accepted")
	}
	if _, err := CheckFilterQuery(PageServices, "tag=core"); err == nil {
		t.Fatal("the services page does not have a tag filter")
	}
}
