package model

import (
	"strings"
	"testing"
)

func TestCheckFilterQueryKeepsTheCanonicalForm(t *testing.T) {
	for _, tc := range []struct{ page, in, want string }{
		{PageInventory, "trust=unknown&status=offline", "status=offline&trust=unknown"},
		{PageInventory, "status=offline&trust=unknown", "status=offline&trust=unknown"},
		{PageInventory, "q=cam+1&group=none", "group=none&q=cam+1"},
		{PageInventory, "group=12&status=&trust=trusted", "group=12&trust=trusted"},
		{PageInventory, "q=%20padded%20", "q=padded"},
		{PageServices, "q=ssh", "q=ssh"},
		{PageContainers, "q=nginx", "q=nginx"},
	} {
		got, err := CheckFilterQuery(tc.page, tc.in)
		if err != nil || got != tc.want {
			t.Errorf("CheckFilterQuery(%s, %q) = %q, %v, want %q", tc.page, tc.in, got, err, tc.want)
		}
	}
}

func TestCheckFilterQueryRefusals(t *testing.T) {
	long := "q=" + strings.Repeat("a", 101)
	for name, tc := range map[string]struct{ page, in string }{
		"unknown page":          {"topology", "q=x"},
		"unknown key":           {PageInventory, "colour=red"},
		"a key of another page": {PageServices, "status=online"},
		"key twice":             {PageInventory, "q=a&q=b"},
		"bad status":            {PageInventory, "status=sideways"},
		"bad trust":             {PageInventory, "trust=maybe"},
		"group zero":            {PageInventory, "group=0"},
		"group negative":        {PageInventory, "group=-3"},
		"group with a prefix":   {PageInventory, "group=01"},
		"group text":            {PageInventory, "group=office"},
		"q too long":            {PageServices, long},
		"q with a control":      {PageServices, "q=a%00b"},
		"q with a break":        {PageServices, "q=a%0Ab"},
		"query too long":        {PageInventory, "q=" + strings.Repeat("a", 301)},
		"only empty values":     {PageInventory, "q=&status="},
		"nothing at all":        {PageInventory, ""},
		"broken escape":         {PageInventory, "q=%zz"},
	} {
		if got, err := CheckFilterQuery(tc.page, tc.in); err == nil {
			t.Errorf("%s: %q accepted as %q", name, tc.in, got)
		}
	}
}

func TestCheckFilterName(t *testing.T) {
	if got, err := CheckFilterName("  Unknown at the office "); err != nil || got != "Unknown at the office" {
		t.Errorf("good name: %q, %v", got, err)
	}
	for _, in := range []string{"", "   ", strings.Repeat("a", 41), "a\x00b", "a\nb", "evil\u202ename"} {
		if _, err := CheckFilterName(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
	if _, err := CheckFilterName(strings.Repeat("ä", 40)); err != nil {
		t.Errorf("40 characters: %v", err)
	}
}
