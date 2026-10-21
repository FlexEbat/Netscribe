package api

import "testing"

func TestCleanersDropBidiOverridesAndKeepDirectionMarks(t *testing.T) {
	if got := cleanLabel("ab\u202ec\u2066d\u2069"); got != "abcd" {
		t.Errorf("cleanLabel = %q, want the overrides and isolates gone", got)
	}
	if got := cleanLabel("a\u200fb"); got != "a\u200fb" {
		t.Errorf("cleanLabel = %q, want the right-to-left mark kept", got)
	}
	if got := cleanNotes("one\nab\u202ec\tx"); got != "one\nabc\tx" {
		t.Errorf("cleanNotes = %q, want line breaks and tabs kept and the override gone", got)
	}
}
