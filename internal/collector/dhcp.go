package collector

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"log/slog"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/FlexEbat/Netscribe/internal/model"
)

// What the listener keeps of one DHCP request. The text comes from the network, so the parser
// cleans it and cuts it before it is stored anywhere.
const (
	maxDHCPHostname = 63  // the longest label of a host name
	maxDHCPClass    = 100 // characters of a vendor class
	maxDHCPFinger   = 255 // characters of the parameter list fingerprint
	maxDHCPSeen     = 4096
	maxDHCPPerSec   = 50             // packets read in one second, the rest are dropped
	dhcpKeep        = 24 * time.Hour // how long a request stays interesting
)

// The DHCP message types of a client that asks for an address.
const (
	dhcpDiscover = 1
	dhcpRequest  = 3
	dhcpInform   = 8
)

// dhcpObservation is what one request said about the client.
type dhcpObservation struct {
	MAC         string
	IP          string // the requested address (option 50), else the one the client already has
	Hostname    string // option 12
	VendorClass string // option 60
	Fingerprint string // option 55, the list of parameters asked for, as "1,3,6,15"
	At          time.Time
}

var errNotDHCP = errors.New("not a DHCP request")

// parseDHCP reads an IPv4 packet and returns the request a client broadcast in it. It accepts
// only a client talking to port 67 with a hardware address of 6 bytes and one of the message
// types DISCOVER, REQUEST and INFORM; everything else, and anything malformed, is an error.
func parseDHCP(pkt []byte) (dhcpObservation, error) {
	if len(pkt) < 20 || pkt[0]>>4 != 4 {
		return dhcpObservation{}, errNotDHCP
	}
	ihl := int(pkt[0]&0x0f) * 4
	if ihl < 20 || len(pkt) < ihl+8 || pkt[9] != 17 { // 17 is UDP
		return dhcpObservation{}, errNotDHCP
	}
	udp := pkt[ihl:]
	if binary.BigEndian.Uint16(udp[0:2]) != 68 || binary.BigEndian.Uint16(udp[2:4]) != 67 {
		return dhcpObservation{}, errNotDHCP
	}
	body := udp[8:]
	if n := int(binary.BigEndian.Uint16(udp[4:6])) - 8; n >= 0 && n < len(body) {
		body = body[:n] // the UDP length, not the padding of the frame
	}
	if len(body) < 240 || body[0] != 1 || body[1] != 1 || body[2] != 6 ||
		!bytes.Equal(body[236:240], []byte{0x63, 0x82, 0x53, 0x63}) {
		return dhcpObservation{}, errNotDHCP
	}
	mac := body[28:34]
	if isZeroMAC(mac) || bytes.Equal(mac, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}) {
		return dhcpObservation{}, errNotDHCP
	}
	obs := dhcpObservation{MAC: macString(mac)}
	if ci, ok := netip.AddrFromSlice(body[12:16]); ok && !ci.IsUnspecified() {
		obs.IP = ci.String()
	}
	msg := 0
	for opts := body[240:]; len(opts) > 0; {
		code := opts[0]
		if code == 255 {
			break
		}
		if code == 0 {
			opts = opts[1:]
			continue
		}
		if len(opts) < 2 || int(opts[1]) > len(opts)-2 {
			return dhcpObservation{}, errNotDHCP
		}
		val := opts[2 : 2+int(opts[1])]
		opts = opts[2+int(opts[1]):]
		switch code {
		case 53:
			if len(val) == 1 {
				msg = int(val[0])
			}
		case 12:
			obs.Hostname = cleanDHCPHostname(val)
		case 60:
			obs.VendorClass = cleanDHCPText(val, maxDHCPClass)
		case 50:
			if a, ok := netip.AddrFromSlice(val); ok && len(val) == 4 && !a.IsUnspecified() {
				obs.IP = a.String()
			}
		case 55:
			parts := make([]string, 0, len(val))
			for _, b := range val {
				parts = append(parts, strconv.Itoa(int(b)))
			}
			obs.Fingerprint = cutAtComma(strings.Join(parts, ","), maxDHCPFinger)
		}
	}
	if msg != dhcpDiscover && msg != dhcpRequest && msg != dhcpInform {
		return dhcpObservation{}, errNotDHCP
	}
	return obs, nil
}

// cutAtComma cuts a comma separated list to at most n characters without leaving half a number.
func cutAtComma(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	if i := strings.LastIndexByte(s, ','); i >= 0 {
		s = s[:i]
	}
	return s
}

func macString(b []byte) string {
	const hexd = "0123456789abcdef"
	out := make([]byte, 0, 17)
	for i, x := range b {
		if i > 0 {
			out = append(out, ':')
		}
		out = append(out, hexd[x>>4], hexd[x&0x0f])
	}
	return string(out)
}

// cleanDHCPHostname keeps the characters a host name may have and cuts it to one label's length.
func cleanDHCPHostname(v []byte) string {
	v = bytes.TrimRight(v, "\x00")
	var b strings.Builder
	for _, r := range string(v) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		}
		if b.Len() >= maxDHCPHostname {
			break
		}
	}
	return strings.Trim(b.String(), ".-")
}

// cleanDHCPText drops control characters and cuts the text to n characters.
func cleanDHCPText(v []byte, n int) string {
	s := strings.TrimSpace(strings.Map(func(r rune) rune {
		if model.UnsafeRune(r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(string(bytes.TrimRight(v, "\x00")), "")))
	if utf8.RuneCountInString(s) > n {
		s = string([]rune(s)[:n])
	}
	return s
}

// DHCP learns about devices from the requests they broadcast. The listener only reads: it
// never answers, never sends and never leases. What it hears waits in memory until the next
// scan takes it.
type DHCP struct {
	log *slog.Logger
	// open starts the packet source and calls handle for every IPv4 packet that arrives on an
	// interface allow accepts, until ctx ends. A variable so a test feeds packets and a system
	// without packet sockets says so.
	open func(ctx context.Context, allow func(ifindex int) bool, handle func(pkt []byte)) error
	now  func() time.Time
	// ifaces lists the interfaces and their prefixes, replaced in tests.
	ifaces func() ([]ndpInterface, error)

	mu       sync.Mutex
	seen     map[string]dhcpObservation // by MAC
	started  bool
	targets  func() []netip.Prefix
	allowed  map[int]struct{}
	allowAt  time.Time
	window   time.Time // the second that is being counted
	inWindow int
}

// NewDHCP returns the collector for this system.
func NewDHCP(log *slog.Logger) *DHCP {
	return &DHCP{log: log, open: openPacketSource, now: time.Now, ifaces: localInterfaces, seen: map[string]dhcpObservation{}}
}

func (c *DHCP) Name() string { return "dhcp" }

// Listen starts the listener once, when the config asks for it. A missing right is one line
// in the log and nothing else: the scan goes on without DHCP.
func (c *DHCP) Listen(ctx context.Context, enabled bool, targets func() []netip.Prefix) {
	if !enabled {
		return
	}
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return
	}
	c.started = true
	c.targets = targets
	c.mu.Unlock()
	go func() {
		err := c.open(ctx, c.allow, c.handle)
		if err != nil && ctx.Err() == nil {
			c.log.Warn("DHCP listening is off", "err", err, "hint", "it needs CAP_NET_RAW, see discovery.dhcpListen")
		}
	}()
}

// allow says whether packets of the interface with this index are read: only the interfaces
// that hold an address inside a scan target. The answer is kept for half a minute.
func (c *DHCP) allow(ifindex int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.allowed == nil || c.now().Sub(c.allowAt) > 30*time.Second {
		c.allowed = map[int]struct{}{}
		c.allowAt = c.now()
		if ifaces, err := c.ifaces(); err == nil && c.targets != nil {
			for _, i := range scannedInterfaces(ifaces, c.targets()) {
				c.allowed[i.Index] = struct{}{}
			}
		}
	}
	_, ok := c.allowed[ifindex]
	return ok
}

// handle keeps one packet when it is a request. More than 50 packets in one second are
// dropped before they are parsed.
func (c *DHCP) handle(pkt []byte) {
	now := c.now()
	c.mu.Lock()
	if now.Sub(c.window) >= time.Second {
		c.window, c.inWindow = now, 0
	}
	c.inWindow++
	over := c.inWindow > maxDHCPPerSec
	c.mu.Unlock()
	if over {
		return
	}
	obs, err := parseDHCP(pkt)
	if err != nil {
		return
	}
	obs.At = now
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, known := c.seen[obs.MAC]; !known && len(c.seen) >= maxDHCPSeen {
		return // a flood of made-up addresses cannot grow the memory
	}
	c.seen[obs.MAC] = obs
}

// facts returns the vendor class and host name a device sent, lower case, for the classifier.
func (c *DHCP) facts(mac string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	o, ok := c.seen[mac]
	if !ok {
		return nil
	}
	var out []string
	for _, s := range []string{o.VendorClass, o.Hostname} {
		if s != "" {
			out = append(out, strings.ToLower(s))
		}
	}
	return out
}

// Collect hands over the requests heard since the last scan and forgets the old ones. A host
// name only fills an empty one: the store knows source "dhcp" as a weak one. A request from a
// MAC the scan does not know becomes a device only when its address lies in a scan target.
func (c *DHCP) Collect(ctx context.Context, in Input) (Result, error) {
	var res Result
	known := map[string]string{}
	for _, d := range in.Devices {
		if d.MAC != "" {
			known[d.MAC] = d.IP
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	macs := make([]string, 0, len(c.seen))
	for mac, o := range c.seen {
		if c.now().Sub(o.At) > dhcpKeep {
			delete(c.seen, mac)
			continue
		}
		macs = append(macs, mac)
	}
	sort.Strings(macs)
	for _, mac := range macs {
		o := c.seen[mac]
		ip, isKnown := known[mac]
		if !isKnown {
			a, err := netip.ParseAddr(o.IP)
			if err != nil || !inAnyTarget(in.Targets, a) {
				continue
			}
			ip = o.IP
		}
		res.Devices = append(res.Devices, model.DeviceInput{
			Key: model.DeviceKey{MAC: mac, IP: ip}, Hostname: o.Hostname, Source: "dhcp",
		})
	}
	return res, ctx.Err()
}

func inAnyTarget(targets []netip.Prefix, a netip.Addr) bool {
	for _, t := range targets {
		if t.Contains(a) {
			return true
		}
	}
	return false
}
