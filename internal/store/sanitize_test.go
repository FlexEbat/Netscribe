package store

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestApplyResultCleansTextFromTheNetwork(t *testing.T) {
	s := newTestStore(t)
	in := dev("aa:bb:cc:dd:ee:01", "192.168.1.5")
	in.Hostname = "host\x1b[31m\nname\u0000"
	in.Vendor = "  Acme  "
	in.Description = strings.Repeat("\u00e9", 300)
	scanOnce(t, s, "192.168.1.0/24", in)

	got := list(t, s, DeviceFilter{})
	if len(got) != 1 {
		t.Fatalf("devices = %d, want 1", len(got))
	}
	d := got[0]
	if d.Hostname != "host[31mname" {
		t.Errorf("hostname = %q, want control characters removed", d.Hostname)
	}
	if d.Vendor != "Acme" {
		t.Errorf("vendor = %q, want trimmed", d.Vendor)
	}
	if n := utf8.RuneCountInString(d.Description); n != 255 || !utf8.ValidString(d.Description) {
		t.Errorf("description has %d characters, valid UTF-8 = %v, want 255 whole characters", n, utf8.ValidString(d.Description))
	}
}

func TestCleanTextLeavesShortNamesAlone(t *testing.T) {
	for _, in := range []string{"", "router.lan", "\u043d\u0430\u0441-\u0441\u0435\u0440\u0432\u0435\u0440"} {
		if got := cleanText(in); got != in {
			t.Errorf("cleanText(%q) = %q", in, got)
		}
	}
}
