package model

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// The limits of what a person writes about a device (slice 49).
const (
	MaxOwnerRunes     = 100
	MaxLocationRunes  = 100
	MaxSerialRunes    = 64
	MaxTags           = 12
	MaxTagRunes       = 30
	MaxFields         = 10
	MaxFieldNameRunes = 30
	MaxFieldValRunes  = 200
)

// The checks, in the words the API returns.
var (
	ErrOwnerLong     = errors.New("owner is longer than 100 characters")
	ErrLocationLong  = errors.New("location is longer than 100 characters")
	ErrSerialLong    = errors.New("serial is longer than 64 characters")
	ErrWarranty      = errors.New("warranty must be a date as YYYY-MM-DD, or empty")
	ErrTagInvalid    = errors.New("a tag is 1 to 30 characters of a-z, 0-9, space, _ and -")
	ErrTooManyTags   = errors.New("a device has at most 12 tags")
	ErrFieldName     = errors.New("the name of a field is 1 to 30 characters")
	ErrFieldValue    = errors.New("the value of a field is longer than 200 characters")
	ErrTooManyFields = errors.New("a device has at most 10 fields of your own")
	ErrFieldTwice    = errors.New("two fields have the same name")
)

// DeviceField is a name and a value a person put on a device.
type DeviceField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Details is what a person writes about a device. A scan never writes any of it.
type Details struct {
	Owner, Location, Serial, WarrantyUntil string
	Tags                                   []string
	Fields                                 []DeviceField
}

// CleanText removes the characters that have no place in a name and trims the rest.
func CleanText(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if UnsafeRune(r) {
			return -1
		}
		return r
	}, s))
}

// CheckOwner cleans and checks the owner of a device.
func CheckOwner(s string) (string, error) {
	s = CleanText(s)
	if utf8.RuneCountInString(s) > MaxOwnerRunes {
		return "", ErrOwnerLong
	}
	return s, nil
}

// CheckLocation cleans and checks where a device stands.
func CheckLocation(s string) (string, error) {
	s = CleanText(s)
	if utf8.RuneCountInString(s) > MaxLocationRunes {
		return "", ErrLocationLong
	}
	return s, nil
}

// CheckSerial cleans and checks a serial number.
func CheckSerial(s string) (string, error) {
	s = CleanText(s)
	if utf8.RuneCountInString(s) > MaxSerialRunes {
		return "", ErrSerialLong
	}
	return s, nil
}

// CheckWarranty accepts an empty string or a real date as YYYY-MM-DD.
func CheckWarranty(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil || t.Format("2006-01-02") != s {
		return "", ErrWarranty
	}
	return s, nil
}

// CheckTag trims a tag, folds it to lower case and checks it.
func CheckTag(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	n := utf8.RuneCountInString(s)
	if n < 1 || n > MaxTagRunes {
		return "", ErrTagInvalid
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == ' ', r == '_', r == '-':
		default:
			return "", ErrTagInvalid
		}
	}
	return s, nil
}

// CheckTags checks a list of tags and returns it without repeats, in the order given. More
// than MaxTags different tags are refused.
func CheckTags(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, raw := range in {
		t, err := CheckTag(raw)
		if err != nil {
			return nil, err
		}
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	if len(out) > MaxTags {
		return nil, ErrTooManyTags
	}
	return out, nil
}

// CheckFields cleans and checks the fields of a person's own. Two fields with one name are
// refused, because the name is the key.
func CheckFields(in []DeviceField) ([]DeviceField, error) {
	if len(in) > MaxFields {
		return nil, ErrTooManyFields
	}
	out := make([]DeviceField, 0, len(in))
	seen := map[string]bool{}
	for _, f := range in {
		name, value := CleanText(f.Name), CleanText(f.Value)
		if n := utf8.RuneCountInString(name); n < 1 || n > MaxFieldNameRunes {
			return nil, ErrFieldName
		}
		if utf8.RuneCountInString(value) > MaxFieldValRunes {
			return nil, ErrFieldValue
		}
		if seen[name] {
			return nil, ErrFieldTwice
		}
		seen[name] = true
		out = append(out, DeviceField{Name: name, Value: value})
	}
	return out, nil
}
