package model

// IsBidiControl reports whether r is a Unicode bidirectional override, embedding or
// isolate. Text that comes from the network or from a person can carry one to make a name
// read as another name in the panel or in an export, so every cleaner drops them. The
// left-to-right and right-to-left marks stay: right-to-left scripts need them.
func IsBidiControl(r rune) bool {
	return (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069)
}
