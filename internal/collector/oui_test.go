package collector

import (
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/model"
)

func TestVendor(t *testing.T) {
	cases := []struct{ mac, want string }{
		{"00:00:0c:12:34:56", "Cisco Systems"},
		{"00:00:0C:12:34:56", "Cisco Systems"},
		{" b8:27:eb:00:00:01 ", "Raspberry Pi Foundation"},
		{"00:50:56:ab:cd:ef", "VMware"},
		{"02:00:0c:12:34:56", ""}, // locally administered: no manufacturer
		{"fe:ff:ff:ff:ff:ff", ""},
		{"zz:zz:zz:00:00:00", ""},
		{"00:00", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := Vendor(c.mac); got != c.want {
			t.Errorf("Vendor(%q) = %q, want %q", c.mac, got, c.want)
		}
	}
}

func TestOUITableIsWellFormed(t *testing.T) {
	ouiOnce.Do(loadOUI)
	if len(ouiTable) < 20 {
		t.Fatalf("only %d prefixes loaded", len(ouiTable))
	}
	for prefix, vendor := range ouiTable {
		if prefix != strings.ToLower(prefix) || len(prefix) != 8 || vendor == "" {
			t.Errorf("bad entry %q -> %q", prefix, vendor)
		}
		if hexByte(prefix[:2])&0x03 != 0 {
			t.Errorf("%s: a manufacturer prefix has neither the group nor the local bit set", prefix)
		}
	}
}

func TestFillVendorsKeepsWhatIsThere(t *testing.T) {
	devs := []model.DeviceInput{
		{Key: model.DeviceKey{MAC: "00:00:0c:00:00:01"}},
		{Key: model.DeviceKey{MAC: "00:00:0c:00:00:02"}, Vendor: "Custom"},
		{Key: model.DeviceKey{IP: "10.0.0.1"}},
	}
	fillVendors(devs)
	if devs[0].Vendor != "Cisco Systems" || devs[1].Vendor != "Custom" || devs[2].Vendor != "" {
		t.Errorf("devs = %+v", devs)
	}
}
