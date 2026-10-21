package model

import "unicode"

// UnsafeRune reports whether text from outside must lose r before it is stored or shown:
// a control character, or one of the characters that change the direction of the text
// around them. A name that ends in U+202E, for example, makes what follows it read
// backwards in a table, a log or an export, which hides what the name really is.
func UnsafeRune(r rune) bool {
	switch {
	case unicode.IsControl(r):
		return true
	case r == '\u061C', r == '\u200E', r == '\u200F': // Arabic letter mark, left-to-right and right-to-left marks
		return true
	case r >= '\u202A' && r <= '\u202E': // embeddings and overrides
		return true
	case r >= '\u2066' && r <= '\u2069': // isolates
		return true
	}
	return false
}
