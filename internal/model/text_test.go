package model

import "testing"

func TestUnsafeRune(t *testing.T) {
	unsafe := []rune{0, '\n', '\t', 0x7f, 0x85, '\u061C', '\u200E', '\u200F', '\u202A', '\u202B', '\u202C', '\u202D', '\u202E', '\u2066', '\u2067', '\u2068', '\u2069'}
	for _, r := range unsafe {
		if !UnsafeRune(r) {
			t.Errorf("U+%04X is safe, want unsafe", r)
		}
	}
	safe := []rune{'a', ' ', 'я', '漢', '😀', '\u200D', '\u2029', 'é', '-'}
	for _, r := range safe {
		if UnsafeRune(r) {
			t.Errorf("U+%04X is unsafe, want safe", r)
		}
	}
}
