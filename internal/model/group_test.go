package model

import (
	"strings"
	"testing"
)

func TestCheckGroupName(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{"Office", "Office", true},
		{"  Lab  ", "Lab", true},
		{strings.Repeat("ä", 40), strings.Repeat("ä", 40), true},
		{"", "", false},
		{"   ", "", false},
		{strings.Repeat("a", 41), "", false},
		{"a\x00b", "", false},
		{"a\nb", "", false},
		{"evil\u202ename", "", false},
	} {
		got, err := CheckGroupName(tc.in)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("CheckGroupName(%q) = %q, %v", tc.in, got, err)
		}
	}
}

func TestCheckGroupDescription(t *testing.T) {
	if _, err := CheckGroupDescription(""); err != nil {
		t.Errorf("empty: %v", err)
	}
	if _, err := CheckGroupDescription(strings.Repeat("x", 201)); err == nil {
		t.Error("201 characters accepted")
	}
	if _, err := CheckGroupDescription("two\nlines"); err == nil {
		t.Error("a line break was accepted")
	}
}

func TestCheckGroupVLAN(t *testing.T) {
	ok := func(n int) *int { return &n }
	for _, v := range []*int{nil, ok(1), ok(4094)} {
		if err := CheckGroupVLAN(v); err != nil {
			t.Errorf("%v: %v", v, err)
		}
	}
	for _, n := range []int{0, -1, 4095, 100000} {
		if err := CheckGroupVLAN(ok(n)); err == nil {
			t.Errorf("vlan %d accepted", n)
		}
	}
}

func TestCheckGroupRanges(t *testing.T) {
	got, err := CheckGroupRanges([]string{" 192.168.1.0/24 ", "10.0.0.0/8", "172.16.0.0/12"})
	if err != nil || strings.Join(got, ",") != "192.168.1.0/24,10.0.0.0/8,172.16.0.0/12" {
		t.Errorf("good list: %v, %v", got, err)
	}
	if got, err := CheckGroupRanges(nil); err != nil || got == nil || len(got) != 0 {
		t.Errorf("no ranges: %v, %v, want an empty list that is not nil", got, err)
	}
	for name, in := range map[string][]string{
		"host bits":   {"192.168.1.5/24"},
		"not a cidr":  {"192.168.1.0"},
		"ipv6":        {"fd00::/64"},
		"garbage":     {"hello"},
		"twice":       {"10.0.0.0/8", "10.0.0.0/8"},
		"empty entry": {""},
	} {
		if _, err := CheckGroupRanges(in); err == nil {
			t.Errorf("%s: %v accepted", name, in)
		}
	}
	var many []string
	for i := 0; i < 17; i++ {
		many = append(many, "10."+string(rune('0'+i/10))+"."+string(rune('0'+i%10))+".0/24")
	}
	if _, err := CheckGroupRanges(many); err != ErrGroupRangeCount {
		t.Errorf("17 ranges: %v, want ErrGroupRangeCount", err)
	}
	if _, err := CheckGroupRanges(many[:16]); err != nil {
		t.Errorf("16 ranges: %v", err)
	}
}
