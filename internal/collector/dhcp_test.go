package collector

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
)

type dhcpOpt struct {
	code byte
	val  []byte
}

// dhcpPacket builds an IPv4 packet that carries a DHCP message, the way a client sends it.
func dhcpPacket(op byte, mac []byte, srcPort, dstPort uint16, opts ...dhcpOpt) []byte {
	body := make([]byte, 240)
	body[0], body[1], body[2] = op, 1, 6
	copy(body[28:34], mac)
	copy(body[236:240], []byte{0x63, 0x82, 0x53, 0x63})
	for _, o := range opts {
		body = append(body, o.code, byte(len(o.val)))
		body = append(body, o.val...)
	}
	body = append(body, 255)
	udp := binary.BigEndian.AppendUint16(nil, srcPort)
	udp = binary.BigEndian.AppendUint16(udp, dstPort)
	udp = binary.BigEndian.AppendUint16(udp, uint16(8+len(body))) //nolint:gosec // a fixture of a few hundred bytes
	udp = append(udp, 0, 0)
	udp = append(udp, body...)
	ip := []byte{0x45, 0, 0, 0, 0, 0, 0, 0, 64, 17, 0, 0, 0, 0, 0, 0, 255, 255, 255, 255}
	binary.BigEndian.PutUint16(ip[2:4], uint16(20+len(udp))) //nolint:gosec // a fixture of a few hundred bytes
	return append(ip, udp...)
}

func request(msg byte, mac []byte, opts ...dhcpOpt) []byte {
	return dhcpPacket(1, mac, 68, 67, append([]dhcpOpt{{53, []byte{msg}}}, opts...)...)
}

var testMAC = []byte{0xaa, 0xbb, 0xcc, 0x00, 0x00, 0x07}

func TestParseDHCPReadsTheFieldsOfARequest(t *testing.T) {
	pkt := request(dhcpRequest, testMAC,
		dhcpOpt{12, []byte("galaxy-s23")},
		dhcpOpt{60, []byte("android-dhcp-13")},
		dhcpOpt{55, []byte{1, 3, 6, 15, 26, 28}},
		dhcpOpt{50, []byte{192, 168, 1, 77}},
	)
	got, err := parseDHCP(pkt)
	if err != nil {
		t.Fatal(err)
	}
	want := dhcpObservation{MAC: "aa:bb:cc:00:00:07", IP: "192.168.1.77", Hostname: "galaxy-s23", VendorClass: "android-dhcp-13", Fingerprint: "1,3,6,15,26,28"}
	if got != want {
		t.Errorf("observation = %+v, want %+v", got, want)
	}
}

func TestParseDHCPAcceptsOnlyRequestsOfAClient(t *testing.T) {
	good := func() []byte { return request(dhcpDiscover, testMAC) }
	if _, err := parseDHCP(good()); err != nil {
		t.Fatalf("a DISCOVER was refused: %v", err)
	}
	if _, err := parseDHCP(request(dhcpInform, testMAC)); err != nil {
		t.Errorf("an INFORM was refused: %v", err)
	}
	bad := map[string][]byte{
		"empty":                  nil,
		"too short":              good()[:25],
		"a server reply":         dhcpPacket(2, testMAC, 67, 68, dhcpOpt{53, []byte{2}}),
		"another port":           dhcpPacket(1, testMAC, 68, 5353, dhcpOpt{53, []byte{dhcpDiscover}}),
		"a release":              request(7, testMAC),
		"an offer":               request(2, testMAC),
		"no message type":        dhcpPacket(1, testMAC, 68, 67),
		"an all zero address":    request(dhcpDiscover, make([]byte, 6)),
		"a broadcast address":    request(dhcpDiscover, bytes.Repeat([]byte{0xff}, 6)),
		"an option past the end": append(good()[:len(good())-1], 12, 200),
	}
	notUDP := good()
	notUDP[9] = 6 // TCP
	bad["not UDP"] = notUDP
	notIPv4 := good()
	notIPv4[0] = 0x65
	bad["not IPv4"] = notIPv4
	noCookie := good()
	copy(noCookie[28+240-4:], []byte{0, 0, 0, 0})
	bad["no magic cookie"] = noCookie
	for name, pkt := range bad {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s: panic %v", name, r)
				}
			}()
			if obs, err := parseDHCP(pkt); err == nil {
				t.Errorf("%s: accepted as %+v, want an error", name, obs)
			}
		}()
	}
}

func TestParseDHCPCleansAndCapsTheText(t *testing.T) {
	host := append([]byte("evil\x07\x1b host\u202e name"), bytes.Repeat([]byte("a"), 250)...)
	class := append([]byte("class\x00\x07"), bytes.Repeat([]byte("é"), 120)...)
	obs, err := parseDHCP(request(dhcpDiscover, testMAC, dhcpOpt{12, host[:255]}, dhcpOpt{60, class[:240]}))
	if err != nil {
		t.Fatal(err)
	}
	if len(obs.Hostname) > maxDHCPHostname || strings.ContainsAny(obs.Hostname, "\x07\x1b \u202e") {
		t.Errorf("hostname = %q, want letters, digits, dots and dashes only, at most 63", obs.Hostname)
	}
	if strings.ContainsAny(obs.VendorClass, "\x00\x07") || len([]rune(obs.VendorClass)) > maxDHCPClass {
		t.Errorf("vendor class = %q, want no control characters and at most 100 characters", obs.VendorClass)
	}
}

func newTestDHCP(t *testing.T) *DHCP {
	t.Helper()
	c := NewDHCP(slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.now = func() time.Time { return time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC) }
	return c
}

func TestDHCPCollectFillsAKnownDeviceAndAddsOneInATarget(t *testing.T) {
	c := newTestDHCP(t)
	c.handle(request(dhcpRequest, testMAC, dhcpOpt{12, []byte("phone")}, dhcpOpt{60, []byte("android-dhcp-13")}, dhcpOpt{50, []byte{192, 168, 1, 77}}))
	other := []byte{0xaa, 0xbb, 0xcc, 0x00, 0x00, 0x08}
	c.handle(request(dhcpRequest, other, dhcpOpt{12, []byte("laptop")}, dhcpOpt{50, []byte{192, 168, 1, 78}}))
	outside := []byte{0xaa, 0xbb, 0xcc, 0x00, 0x00, 0x09}
	c.handle(request(dhcpRequest, outside, dhcpOpt{12, []byte("guest")}, dhcpOpt{50, []byte{10, 9, 9, 9}}))

	in := Input{
		Targets: []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")},
		Devices: []model.Device{{MAC: "aa:bb:cc:00:00:07", IP: "192.168.1.50"}},
	}
	res, err := c.Collect(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Devices) != 2 {
		t.Fatalf("devices = %+v, want the known one and the one inside the target", res.Devices)
	}
	known, added := res.Devices[0], res.Devices[1]
	if known.Key.IP != "192.168.1.50" || known.Hostname != "phone" || known.Source != "dhcp" {
		t.Errorf("known = %+v, want its own address kept and the host name from DHCP", known)
	}
	if added.Key.MAC != "aa:bb:cc:00:00:08" || added.Key.IP != "192.168.1.78" || added.Hostname != "laptop" {
		t.Errorf("added = %+v, want the laptop at its requested address", added)
	}
	if facts := c.facts("aa:bb:cc:00:00:07"); strings.Join(facts, "|") != "android-dhcp-13|phone" {
		t.Errorf("facts = %v, want the vendor class and the host name", facts)
	}
}

func TestDHCPForgetsOldRequestsAndCapsMemory(t *testing.T) {
	c := newTestDHCP(t)
	c.handle(request(dhcpRequest, testMAC, dhcpOpt{12, []byte("old")}))
	c.now = func() time.Time { return time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC) } // two days later
	res, err := c.Collect(context.Background(), Input{Devices: []model.Device{{MAC: "aa:bb:cc:00:00:07", IP: "10.0.0.1"}}})
	if err != nil || len(res.Devices) != 0 {
		t.Errorf("res = %+v, err = %v, want a request older than a day forgotten", res, err)
	}

	flood := newTestDHCP(t)
	tick := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	flood.now = func() time.Time { tick = tick.Add(time.Second); return tick } // one packet a second: under the rate limit
	for i := 0; i < maxDHCPSeen+500; i++ {
		mac := []byte{0x02, 0, byte(i >> 16), byte(i >> 8), byte(i), 1}
		flood.handle(request(dhcpDiscover, mac))
	}
	flood.mu.Lock()
	n := len(flood.seen)
	flood.mu.Unlock()
	if n != maxDHCPSeen {
		t.Errorf("%d clients kept, want the cap of %d", n, maxDHCPSeen)
	}
}

func TestDHCPListenStartsOnceAndOnlyWhenAsked(t *testing.T) {
	var opened atomic.Int32
	done := make(chan struct{}, 4)
	c := newTestDHCP(t)
	c.open = func(ctx context.Context, allow func(int) bool, handle func([]byte)) error {
		opened.Add(1)
		handle(request(dhcpRequest, testMAC, dhcpOpt{12, []byte("phone")}))
		done <- struct{}{}
		return fmt.Errorf("no CAP_NET_RAW") // a missing right is logged, not fatal
	}
	ctx := context.Background()
	c.Listen(ctx, false, nil)
	if opened.Load() != 0 {
		t.Fatal("the listener opened a socket while discovery.dhcpListen is off")
	}
	c.Listen(ctx, true, nil)
	c.Listen(ctx, true, nil)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the listener never started")
	}
	time.Sleep(50 * time.Millisecond)
	if opened.Load() != 1 {
		t.Errorf("the listener opened %d times, want once", opened.Load())
	}
	if f := c.facts("aa:bb:cc:00:00:07"); len(f) == 0 {
		t.Error("the request heard before the error was not kept")
	}
}

func TestDHCPReadsAtMostFiftyPacketsASecond(t *testing.T) {
	c := newTestDHCP(t)
	for i := 0; i < 80; i++ {
		c.handle(request(dhcpDiscover, []byte{0x02, 0, 0, 0, byte(i), 1}))
	}
	c.mu.Lock()
	first := len(c.seen)
	c.mu.Unlock()
	if first != maxDHCPPerSec {
		t.Errorf("%d clients kept from 80 packets in one second, want %d", first, maxDHCPPerSec)
	}
	c.now = func() time.Time { return time.Date(2026, 11, 4, 12, 0, 2, 0, time.UTC) }
	c.handle(request(dhcpDiscover, []byte{0x02, 0, 0, 0, 200, 1}))
	c.mu.Lock()
	second := len(c.seen)
	c.mu.Unlock()
	if second != maxDHCPPerSec+1 {
		t.Errorf("%d clients after a quiet second, want the budget to start over", second)
	}
}

func TestDHCPCapsTheFingerprint(t *testing.T) {
	params := make([]byte, 200)
	for i := range params {
		params[i] = byte(100 + i%100)
	}
	obs, err := parseDHCP(request(dhcpDiscover, testMAC, dhcpOpt{55, params}))
	if err != nil {
		t.Fatal(err)
	}
	if len(obs.Fingerprint) > maxDHCPFinger || strings.HasSuffix(obs.Fingerprint, ",") {
		t.Errorf("fingerprint has %d characters, want at most %d and no half entry", len(obs.Fingerprint), maxDHCPFinger)
	}
}

func TestDHCPListensOnlyOnTheInterfacesOfTheScanTargets(t *testing.T) {
	c := newTestDHCP(t)
	c.ifaces = func() ([]ndpInterface, error) {
		return []ndpInterface{
			{Index: 2, Name: "eth0", Prefixes: []netip.Prefix{netip.MustParsePrefix("192.168.1.5/24")}},
			{Index: 3, Name: "docker0", Prefixes: []netip.Prefix{netip.MustParsePrefix("172.17.0.1/16")}},
		}, nil
	}
	c.targets = func() []netip.Prefix { return []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")} }
	if !c.allow(2) || c.allow(3) || c.allow(9) {
		t.Error("want only the interface that holds an address inside a scan target to be read")
	}
}

func TestTheDHCPListenerNeverWritesToItsSocket(t *testing.T) {
	src, err := os.ReadFile("dhcp_linux.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range []string{"syscall.Write", "syscall.Sendto", "syscall.Sendmsg", "f.Write", "Send("} {
		if strings.Contains(string(src), call) {
			t.Errorf("dhcp_linux.go calls %s, want a read only socket", call)
		}
	}
}
