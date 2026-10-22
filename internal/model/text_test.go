package model

import "testing"

func TestIsBidiControl(t *testing.T) {
	for _, r := range []rune{0x202A, 0x202B, 0x202C, 0x202D, 0x202E, 0x2066, 0x2067, 0x2068, 0x2069} {
		if !IsBidiControl(r) {
			t.Errorf("U+%04X is not reported as a bidi control", r)
		}
	}
	for _, r := range []rune{'a', ' ', 0x200E, 0x200F, 0x0627, 0x2029, 0x202F, 0x206A, 0x1F600} {
		if IsBidiControl(r) {
			t.Errorf("U+%04X is reported as a bidi control", r)
		}
	}
}
