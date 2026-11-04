package collector

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/netip"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
)

// u16 and u32 narrow a length of a test fixture, which is always small.
func u16(n int) uint16 {
	if n < 0 || n > math.MaxUint16 {
		panic("fixture too large")
	}
	return uint16(n)
}

func u32(n int) uint32 {
	if n < 0 || n > math.MaxUint32 {
		panic("fixture too large")
	}
	return uint32(n)
}

func nlAttr(typ uint16, payload []byte) []byte {
	b := binary.NativeEndian.AppendUint16(nil, u16(4+len(payload)))
	b = binary.NativeEndian.AppendUint16(b, typ)
	b = append(b, payload...)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

// nlNeigh builds one RTM_NEWNEIGH message for AF_INET6.
func nlNeigh(iface uint32, state uint16, attrs ...[]byte) []byte {
	body := []byte{afInet6, 0, 0, 0}
	body = binary.NativeEndian.AppendUint32(body, iface)
	body = binary.NativeEndian.AppendUint16(body, state)
	body = append(body, 0, 0) // flags, type
	for _, a := range attrs {
		body = append(body, a...)
	}
	return nlMessage(rtmNewNeigh, body)
}

func nlMessage(typ uint16, body []byte) []byte {
	b := binary.NativeEndian.AppendUint32(nil, u32(nlmsgHdrLen+len(body)))
	b = binary.NativeEndian.AppendUint16(b, typ)
	b = append(b, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0) // flags, seq, pid
	b = append(b, body...)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

func dst(ip string) []byte    { a := netip.MustParseAddr(ip).As16(); return nlAttr(ndaDst, a[:]) }
func lladdr(b ...byte) []byte { return nlAttr(ndaLLAddr, b) }

func TestParseNeighboursKeepsOnlyUsableRows(t *testing.T) {
	mac := []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}
	var dump []byte
	dump = append(dump, nlNeigh(2, nudReachable, dst("fd00::1"), lladdr(mac...))...)
	dump = append(dump, nlNeigh(2, nudStale, dst("fe80::1"), lladdr(mac...))...)
	dump = append(dump, nlNeigh(2, 0x01 /* incomplete */, dst("fd00::2"), lladdr(mac...))...)
	dump = append(dump, nlNeigh(2, 0x20 /* failed */, dst("fd00::3"), lladdr(mac...))...)
	dump = append(dump, nlNeigh(2, nudReachable, dst("fd00::4"))...)                           // no link layer address
	dump = append(dump, nlNeigh(2, nudReachable, dst("fd00::5"), lladdr(0, 0, 0, 0, 0, 0))...) // all-zero address
	dump = append(dump, nlNeigh(2, nudReachable, dst("ff02::1"), lladdr(mac...))...)           // multicast
	dump = append(dump, nlNeigh(2, nudReachable, dst("::ffff:10.0.0.9"), lladdr(mac...))...)   // IPv4-mapped
	dump = append(dump, nlNeigh(3, nudPermanent, dst("fd00::6"), lladdr(mac...))...)
	dump = append(dump, nlMessage(nlmsgDone, []byte{0, 0, 0, 0})...)
	dump = append(dump, nlNeigh(2, nudReachable, dst("fd00::7"), lladdr(mac...))...) // after DONE: ignored

	got := parseNeighbours(dump)
	var ips []string
	for _, e := range got {
		ips = append(ips, e.IP.String())
		if e.MAC != "00:11:22:33:44:55" {
			t.Errorf("mac of %s = %q, want 00:11:22:33:44:55", e.IP, e.MAC)
		}
	}
	want := []string{"fd00::1", "fe80::1", "fd00::6"}
	if len(ips) != len(want) || ips[0] != want[0] || ips[1] != want[1] || ips[2] != want[2] {
		t.Errorf("addresses = %v, want %v", ips, want)
	}
	if got[2].Iface != 3 {
		t.Errorf("interface of fd00::6 = %d, want 3", got[2].Iface)
	}
}

func TestParseNeighboursSurvivesBrokenBytes(t *testing.T) {
	good := nlNeigh(2, nudReachable, dst("fd00::1"), lladdr(1, 2, 3, 4, 5, 6))
	for name, buf := range map[string][]byte{
		"empty":            nil,
		"short header":     good[:7],
		"truncated body":   good[:len(good)-5],
		"length too small": append(binary.NativeEndian.AppendUint32(nil, 4), good[4:]...),
		"length too large": append(binary.NativeEndian.AppendUint32(nil, 9999), good[4:]...),
		"IPv4 family":      nlMessage(rtmNewNeigh, []byte{2, 0, 0, 0, 2, 0, 0, 0, 2, 0, 0, 0}),
		"attribute lies":   nlMessage(rtmNewNeigh, append([]byte{afInet6, 0, 0, 0, 2, 0, 0, 0, nudReachable, 0, 0, 0}, 0xff, 0xff, 1, 0)),
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s: panic %v", name, r)
				}
			}()
			if got := parseNeighbours(buf); name != "empty" && len(got) != 0 && name != "short header" {
				t.Errorf("%s: parsed %d entries from broken bytes, want none", name, len(got))
			}
		}()
	}
}

func TestChooseIP6PrefersAStableGlobalAddress(t *testing.T) {
	mac := "00:11:22:33:44:55"
	stable := netip.MustParseAddr("2001:db8::211:22ff:fe33:4455") // modified EUI-64 of the MAC
	privacy := netip.MustParseAddr("2001:db8::aaaa:bbbb:cccc:dddd")
	ula := netip.MustParseAddr("fd00::1")
	ll := netip.MustParseAddr("fe80::211:22ff:fe33:4455")

	tests := []struct {
		name  string
		cands []ip6Candidate
		want  string
	}{
		{"a stable global beats a temporary one", []ip6Candidate{{Addr: privacy, Temporary: true}, {Addr: stable}}, stable.String()},
		{"a stable global beats a deprecated one", []ip6Candidate{{Addr: privacy, Deprecated: true}, {Addr: ula}}, ula.String()},
		{"a temporary global beats link local", []ip6Candidate{{Addr: ll}, {Addr: privacy, Temporary: true}}, privacy.String()},
		{"link local is the last resort", []ip6Candidate{{Addr: ll}}, ll.String()},
		{"the address built from the MAC wins among equals", []ip6Candidate{{Addr: privacy}, {Addr: stable}}, stable.String()},
		{"the lowest address wins when nothing else separates them", []ip6Candidate{{Addr: netip.MustParseAddr("fd00::9")}, {Addr: ula}}, ula.String()},
		{"loopback and multicast are never chosen", []ip6Candidate{{Addr: netip.MustParseAddr("::1")}, {Addr: netip.MustParseAddr("ff02::1")}}, ""},
	}
	for _, tt := range tests {
		got, ok := chooseIP6(mac, tt.cands)
		if tt.want == "" {
			if ok {
				t.Errorf("%s: chose %s, want nothing", tt.name, got)
			}
			continue
		}
		if !ok || got.String() != tt.want {
			t.Errorf("%s: chose %v, want %s", tt.name, got, tt.want)
		}
	}
}

func TestChooseIP6IsTheSameWhateverTheOrder(t *testing.T) {
	a := ip6Candidate{Addr: netip.MustParseAddr("fd00::2")}
	b := ip6Candidate{Addr: netip.MustParseAddr("fd00::1")}
	c := ip6Candidate{Addr: netip.MustParseAddr("fe80::1")}
	first, _ := chooseIP6("00:00:00:00:00:01", []ip6Candidate{a, b, c})
	second, _ := chooseIP6("00:00:00:00:00:01", []ip6Candidate{c, b, a})
	if first != second || first.String() != "fd00::1" {
		t.Errorf("choices = %s and %s, want fd00::1 both times", first, second)
	}
}

type ndpFake struct {
	dump    []byte
	readErr error
	echoed  [][]string
	reads   int
	ifaces  []ndpInterface
}

func (f *ndpFake) collector() *NDP {
	return &NDP{
		read:  func(context.Context) ([]byte, error) { f.reads++; return f.dump, f.readErr },
		echo:  func(_ context.Context, names []string) error { f.echoed = append(f.echoed, names); return nil },
		ifmap: func() ([]ndpInterface, error) { return f.ifaces, nil },
		log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func newNDPFake() *ndpFake {
	known := []byte{0xaa, 0xbb, 0xcc, 0x00, 0x00, 0x01}
	other := []byte{0xaa, 0xbb, 0xcc, 0x00, 0x00, 0x02}
	var dump []byte
	dump = append(dump, nlNeigh(2, nudReachable, dst("fd00::1"), lladdr(known...))...)
	dump = append(dump, nlNeigh(2, nudReachable, dst("fe80::1"), lladdr(known...))...)
	dump = append(dump, nlNeigh(2, nudReachable, dst("fd00::22"), lladdr(other...))...)
	dump = append(dump, nlNeigh(9, nudReachable, dst("fd00::99"), lladdr(0xaa, 0xbb, 0xcc, 0, 0, 3))...) // a segment nobody scans
	return &ndpFake{dump: dump, ifaces: []ndpInterface{
		{Index: 2, Name: "eth0", Prefixes: []netip.Prefix{netip.MustParsePrefix("192.168.1.5/24"), netip.MustParsePrefix("fd00::5/64")}},
		{Index: 9, Name: "docker0", Prefixes: []netip.Prefix{netip.MustParsePrefix("172.17.0.1/16")}},
	}}
}

func ndpInput() Input {
	return Input{
		Targets: []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")},
		Devices: []model.Device{{MAC: "aa:bb:cc:00:00:01", IP: "192.168.1.10"}},
	}
}

func TestNDPAttachesAddressesToKnownDevicesAndAddsTheRest(t *testing.T) {
	f := newNDPFake()
	res, err := f.collector().Collect(context.Background(), ndpInput())
	if err != nil {
		t.Fatal(err)
	}
	if len(f.echoed) != 1 || len(f.echoed[0]) != 1 || f.echoed[0][0] != "eth0" {
		t.Errorf("echo went to %v, want one echo on eth0 only", f.echoed)
	}
	if len(res.Devices) != 2 {
		t.Fatalf("devices = %+v, want 2 (the segment of docker0 is not scanned)", res.Devices)
	}
	known, unknown := res.Devices[0], res.Devices[1]
	if known.Key.MAC != "aa:bb:cc:00:00:01" || known.Key.IP != "192.168.1.10" || known.IP6 != "fd00::1" {
		t.Errorf("known device = %+v, want its IPv4 kept and fd00::1 (global before link local)", known)
	}
	if unknown.Key.MAC != "aa:bb:cc:00:00:02" || unknown.Key.IP != "" || unknown.IP6 != "fd00::22" || unknown.Source != "ndp" {
		t.Errorf("unknown device = %+v, want a device with a MAC and an IPv6 address and no IPv4", unknown)
	}
}

func TestNDPIsOffWhenTheConfigSaysSo(t *testing.T) {
	f := newNDPFake()
	in := ndpInput()
	off := false
	in.Config = config.Config{Discovery: config.DiscoveryConfig{IPv6: &off}}
	res, err := f.collector().Collect(context.Background(), in)
	if err != nil || len(res.Devices) != 0 || f.reads != 0 || len(f.echoed) != 0 {
		t.Errorf("res = %+v, err = %v, reads = %d, echoes = %d, want nothing at all", res, err, f.reads, len(f.echoed))
	}
}

func TestNDPDoesNothingWithoutAScannedInterface(t *testing.T) {
	f := newNDPFake()
	in := ndpInput()
	in.Targets = []netip.Prefix{netip.MustParsePrefix("10.9.9.0/24")}
	res, err := f.collector().Collect(context.Background(), in)
	if err != nil || len(res.Devices) != 0 || f.reads != 0 || len(f.echoed) != 0 {
		t.Errorf("res = %+v, err = %v, reads = %d, echoes = %d, want nothing at all", res, err, f.reads, len(f.echoed))
	}
}

func TestNDPOnASystemWithoutNetlinkCostsNothing(t *testing.T) {
	f := newNDPFake()
	f.readErr = errNoNDP
	res, err := f.collector().Collect(context.Background(), ndpInput())
	if err != nil || len(res.Devices) != 0 {
		t.Errorf("res = %+v, err = %v, want an empty result and no error", res, err)
	}
}

func TestNDPReportsAReadFailure(t *testing.T) {
	f := newNDPFake()
	f.readErr = errors.New("boom")
	if _, err := f.collector().Collect(context.Background(), ndpInput()); err == nil || !bytes.Contains([]byte(err.Error()), []byte("boom")) {
		t.Errorf("err = %v, want the read failure", err)
	}
}

func TestIPv6TargetSelectsItsInterface(t *testing.T) {
	ifaces := []ndpInterface{
		{Index: 2, Name: "eth0", Prefixes: []netip.Prefix{netip.MustParsePrefix("fd00::5/64")}},
		{Index: 3, Name: "eth1", Prefixes: []netip.Prefix{netip.MustParsePrefix("fd01::5/64")}},
	}
	got := scannedInterfaces(ifaces, []netip.Prefix{netip.MustParsePrefix("fd00::/112")})
	if len(got) != 1 || got[0].Name != "eth0" {
		t.Errorf("scanned = %+v, want only eth0", got)
	}
}
