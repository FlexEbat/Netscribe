package api

import (
	"errors"
	"net"
	"net/netip"
	"strings"
	"unicode/utf8"

	"github.com/FlexEbat/Netscribe/internal/model"
)

// Limits of manual input (section 8 of the spec).
const (
	maxLabelRunes = 100
	maxNotesRunes = 2000
	maxPortRunes  = 64
	maxHostRunes  = 255
)

// The messages are one line of English each, as section 8 lists them. The few that the
// list does not name are marked.
var (
	errIPInvalid     = errors.New("ip is not valid")
	errMACInvalid    = errors.New("mac is not valid")
	errNoIdentity    = errors.New("device has no ip, mac or label")
	errLabelLong     = errors.New("label is longer than 100 characters")
	errNotesLong     = errors.New("notes are longer than 2000 characters")
	errHostnameLong  = errors.New("hostname is longer than 255 characters") // not in the list of section 8
	errKindUnknown   = errors.New("kind is unknown")
	errSelfLink      = errors.New("link connects a device to itself")
	errPortLong      = errors.New("port is longer than 64 characters") // not in the list of section 8
	errVersionNeeded = errors.New("version is required")               // not in the list of section 8
)

// normalizeIP accepts an empty string or an IP address and returns it in canonical form.
func normalizeIP(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil || a.Zone() != "" {
		return "", errIPInvalid
	}
	return a.Unmap().String(), nil
}

// normalizeMAC accepts an empty string or an EUI-48 address in any common notation and
// returns it in lower case with colons.
func normalizeMAC(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	hw, err := net.ParseMAC(s)
	if err != nil || len(hw) != 6 {
		return "", errMACInvalid
	}
	return hw.String(), nil
}

// cleanLabel removes control characters, which have no place in a name, and trims it.
func cleanLabel(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if model.UnsafeRune(r) {
			return -1
		}
		return r
	}, s))
}

// cleanNotes keeps line breaks and tabs and removes the other control characters.
func cleanNotes(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if model.UnsafeRune(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, strings.ReplaceAll(s, "\r\n", "\n")))
}

func validLabel(s string) error {
	if utf8.RuneCountInString(s) > maxLabelRunes {
		return errLabelLong
	}
	return nil
}

func validNotes(s string) error {
	if utf8.RuneCountInString(s) > maxNotesRunes {
		return errNotesLong
	}
	return nil
}

type deviceBody struct {
	MAC      string `json:"mac"`
	IP       string `json:"ip"`
	Hostname string `json:"hostname"`
	Label    string `json:"label"`
	Notes    string `json:"notes"`
	Kind     string `json:"kind"`
}

// manualDevice validates the body of POST /api/devices.
func (b deviceBody) manualDevice() (model.ManualDevice, error) {
	mac, err := normalizeMAC(b.MAC)
	if err != nil {
		return model.ManualDevice{}, err
	}
	ip, err := normalizeIP(b.IP)
	if err != nil {
		return model.ManualDevice{}, err
	}
	label, notes := cleanLabel(b.Label), cleanNotes(b.Notes)
	if mac == "" && ip == "" && label == "" {
		return model.ManualDevice{}, errNoIdentity
	}
	if err := validLabel(label); err != nil {
		return model.ManualDevice{}, err
	}
	if err := validNotes(notes); err != nil {
		return model.ManualDevice{}, err
	}
	hostname := cleanLabel(b.Hostname)
	if utf8.RuneCountInString(hostname) > maxHostRunes {
		return model.ManualDevice{}, errHostnameLong
	}
	kind := model.DeviceKind(b.Kind)
	if kind == "" {
		kind = model.KindUnknown
	}
	if !validKind(kind) {
		return model.ManualDevice{}, errKindUnknown
	}
	return model.ManualDevice{MAC: mac, IP: ip, Hostname: hostname, Label: label, Notes: notes, Kind: kind}, nil
}

type editBody struct {
	Label   *string `json:"label"`
	Notes   *string `json:"notes"`
	Kind    *string `json:"kind"`
	Trusted *bool   `json:"trusted"`
	GroupID *int64  `json:"groupId"`
	Version int64   `json:"version"`
}

// deviceEdit validates the body of PATCH /api/devices/{id}.
func (b editBody) deviceEdit() (model.DeviceEdit, error) {
	if b.Version < 1 {
		return model.DeviceEdit{}, errVersionNeeded
	}
	e := model.DeviceEdit{Version: b.Version}
	if b.Label != nil {
		l := cleanLabel(*b.Label)
		if err := validLabel(l); err != nil {
			return model.DeviceEdit{}, err
		}
		e.Label = &l
	}
	if b.Notes != nil {
		n := cleanNotes(*b.Notes)
		if err := validNotes(n); err != nil {
			return model.DeviceEdit{}, err
		}
		e.Notes = &n
	}
	if b.Kind != nil {
		k := model.DeviceKind(*b.Kind)
		if k == "" || !validKind(k) {
			return model.DeviceEdit{}, errKindUnknown
		}
		e.Kind = &k
	}
	e.Trusted = b.Trusted
	if b.GroupID != nil {
		if *b.GroupID < 0 {
			return model.DeviceEdit{}, errors.New("groupId must be 0 or a group id")
		}
		e.GroupID = b.GroupID
	}
	return e, nil
}

type linkBody struct {
	ADeviceID int64  `json:"aDeviceId"`
	APort     string `json:"aPort"`
	BDeviceID int64  `json:"bDeviceId"`
	BPort     string `json:"bPort"`
}

// manualLink validates the body of POST /api/links. That both devices exist is the store's concern.
func (b linkBody) manualLink() (model.ManualLink, error) {
	if b.ADeviceID == b.BDeviceID {
		return model.ManualLink{}, errSelfLink
	}
	aPort, bPort := cleanLabel(b.APort), cleanLabel(b.BPort)
	if utf8.RuneCountInString(aPort) > maxPortRunes || utf8.RuneCountInString(bPort) > maxPortRunes {
		return model.ManualLink{}, errPortLong
	}
	return model.ManualLink{ADeviceID: b.ADeviceID, APort: aPort, BDeviceID: b.BDeviceID, BPort: bPort}, nil
}
