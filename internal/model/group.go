package model

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"unicode/utf8"
)

const (
	maxGroupNameRunes = 40
	maxGroupDescRunes = 200
	maxGroupRanges    = 16
)

// The checks of a group, in the words the API returns.
var (
	ErrGroupName       = errors.New("name must be 1 to 40 characters without control characters")
	ErrGroupDesc       = errors.New("description must be at most 200 characters without control characters")
	ErrGroupVLAN       = errors.New("vlan must be a number from 1 to 4094")
	ErrGroupRangeCount = errors.New("a group has at most 16 ranges")
)

func hasUnsafe(s string) bool {
	for _, r := range s {
		if UnsafeRune(r) || r == '\n' || r == '\r' || r == '\t' {
			return true
		}
	}
	return false
}

// CheckGroupName trims a group name and checks it.
func CheckGroupName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > maxGroupNameRunes || hasUnsafe(name) {
		return "", ErrGroupName
	}
	return name, nil
}

// CheckGroupDescription trims a description and checks it.
func CheckGroupDescription(desc string) (string, error) {
	desc = strings.TrimSpace(desc)
	if utf8.RuneCountInString(desc) > maxGroupDescRunes || hasUnsafe(desc) {
		return "", ErrGroupDesc
	}
	return desc, nil
}

// CheckGroupVLAN accepts nil and the numbers 1 to 4094.
func CheckGroupVLAN(v *int) error {
	if v != nil && (*v < 1 || *v > 4094) {
		return ErrGroupVLAN
	}
	return nil
}

// CheckGroupRanges checks the list and returns it in the form it is stored in. Every range
// is an IPv4 network written as its network address, so 192.168.1.5/24 is refused instead of
// being silently widened.
func CheckGroupRanges(ranges []string) ([]string, error) {
	if len(ranges) > maxGroupRanges {
		return nil, ErrGroupRangeCount
	}
	out := make([]string, 0, len(ranges))
	seen := map[netip.Prefix]bool{}
	for _, r := range ranges {
		p, err := netip.ParsePrefix(strings.TrimSpace(r))
		switch {
		case err != nil || !p.Addr().Is4():
			return nil, fmt.Errorf("range %q is not an IPv4 network like 192.168.1.0/24", r)
		case p.Masked() != p:
			return nil, fmt.Errorf("range %q has host bits set, write %s", r, p.Masked())
		case seen[p]:
			return nil, fmt.Errorf("range %q is listed twice", r)
		}
		seen[p] = true
		out = append(out, p.String())
	}
	return out, nil
}
