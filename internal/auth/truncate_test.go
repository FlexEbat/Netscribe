package auth

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateNeverSplitsACharacter(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"short", "abc", 10, "abc"},
		{"exact", "abc", 3, "abc"},
		{"ascii cut", "abcdef", 4, "abcd"},
		{"two byte rune on the edge", "aa\u00e9", 3, "aa"}, // the second byte of é would be cut off
		{"three byte rune on the edge", "a\u20ac", 3, "a"},
		{"rune fits", "a\u00e9", 3, "a\u00e9"},
		{"zero", "abc", 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := truncate(tc.in, tc.n)
			if got != tc.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("truncate(%q, %d) = %q is not valid UTF-8", tc.in, tc.n, got)
			}
		})
	}
	if got := truncate(strings.Repeat("\u00e9", 200), 256); len(got) != 256 || !utf8.ValidString(got) {
		t.Errorf("a long user agent became %d bytes, valid UTF-8 = %v", len(got), utf8.ValidString(got))
	}
}
