package api

import "testing"

func TestCleanLabelAndNotesDropDirectionOverrides(t *testing.T) {
	if got := cleanLabel("router\u202Efdp.exe\x00"); got != "routerfdp.exe" {
		t.Errorf("cleanLabel = %q, want the override and the control character removed", got)
	}
	if got := cleanNotes("line one\n\u2066line\u2069 two\ttab\u200F"); got != "line one\nline two\ttab" {
		t.Errorf("cleanNotes = %q, want line breaks and tabs kept and the direction marks removed", got)
	}
}
