package collector

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"
)

func TestMagicPacketIsSixFFThenTheMACSixteenTimes(t *testing.T) {
	mac := net.HardwareAddr{0x00, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e}
	got := MagicPacket(mac)

	want := bytes.Repeat([]byte{0xFF}, 6)
	want = append(want, bytes.Repeat(mac, 16)...)
	if !bytes.Equal(got, want) || len(got) != 102 {
		t.Errorf("packet = % x (%d bytes), want 6 x FF and 16 copies of the MAC (102 bytes)", got, len(got))
	}
}

func TestBroadcastForFindsTheSubnetThatHoldsTheAddress(t *testing.T) {
	local := []netip.Prefix{
		netip.MustParsePrefix("172.16.5.9/24"),
		netip.MustParsePrefix("10.1.2.3/16"),
		netip.MustParsePrefix("192.168.7.1/22"),
		netip.MustParsePrefix("192.168.50.1/30"),
	}
	for ip, want := range map[string]string{
		"172.16.5.200":    "172.16.5.255",
		"10.1.77.4":       "10.1.255.255",
		"192.168.5.20":    "192.168.7.255",
		"192.168.50.2":    "192.168.50.3",
		"::ffff:10.1.9.9": "10.1.255.255",
	} {
		got, err := BroadcastFor(netip.MustParseAddr(ip), local)
		if err != nil || got.String() != want {
			t.Errorf("BroadcastFor(%s) = %v, %v, want %s", ip, got, err, want)
		}
	}
}

func TestBroadcastForRefusesWhatHasNoPrivateBroadcast(t *testing.T) {
	local := []netip.Prefix{
		netip.MustParsePrefix("192.168.1.5/24"),
		netip.MustParsePrefix("8.8.8.1/24"),
		netip.MustParsePrefix("192.168.9.1/32"),
		netip.MustParsePrefix("192.168.10.1/31"),
		netip.MustParsePrefix("fd00::1/64"),
	}
	for _, tc := range []struct {
		ip   string
		want error
	}{
		{"192.168.2.20", ErrNoLocalSubnet}, // in no local subnet
		{"8.8.8.20", ErrPublicBroadcast},   // the broadcast address is public
		{"192.168.9.1", ErrNoLocalSubnet},  // a /32 has no broadcast address
		{"192.168.10.0", ErrNoLocalSubnet}, // nor has a /31
		{"fd00::5", ErrNoLocalSubnet},      // IPv6 has no broadcast
	} {
		if got, err := BroadcastFor(netip.MustParseAddr(tc.ip), local); !errors.Is(err, tc.want) || got.IsValid() {
			t.Errorf("BroadcastFor(%s) = %v, %v, want %v", tc.ip, got, err, tc.want)
		}
	}
}

type sentPacket struct {
	dst  netip.AddrPort
	data []byte
}

func fakeWaker(sent *[]sentPacket, prefixes ...string) *Waker {
	var local []netip.Prefix
	for _, p := range prefixes {
		local = append(local, netip.MustParsePrefix(p))
	}
	return &Waker{
		Prefixes: func() ([]netip.Prefix, error) { return local, nil },
		Send: func(_ context.Context, dst netip.AddrPort, packet []byte) error {
			*sent = append(*sent, sentPacket{dst: dst, data: append([]byte(nil), packet...)})
			return nil
		},
		Gap: time.Nanosecond,
	}
}

func TestWakeSendsThreeMagicPacketsToThePortNineBroadcast(t *testing.T) {
	var sent []sentPacket
	w := fakeWaker(&sent, "192.168.1.5/24")

	if err := w.Wake(context.Background(), "192.168.1.40", "00:1a:2b:3c:4d:5e"); err != nil {
		t.Fatal(err)
	}
	want := MagicPacket(net.HardwareAddr{0x00, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e})
	if len(sent) != 3 {
		t.Fatalf("sent %d packets, want 3", len(sent))
	}
	for i, s := range sent {
		if s.dst.String() != "192.168.1.255:9" || !bytes.Equal(s.data, want) {
			t.Errorf("packet %d = %s % x, want 192.168.1.255:9 and the magic packet", i, s.dst, s.data)
		}
	}
}

func TestWakeRefusesWithoutSendingAnything(t *testing.T) {
	for name, tc := range map[string]struct {
		ip, mac string
		prefix  string
		want    error
	}{
		"no MAC":           {"192.168.1.40", "", "192.168.1.5/24", ErrNoMAC},
		"not a MAC":        {"192.168.1.40", "not-a-mac", "192.168.1.5/24", ErrNoMAC},
		"an 8 byte MAC":    {"192.168.1.40", "00:1a:2b:3c:4d:5e:6f:70", "192.168.1.5/24", ErrNoMAC},
		"no local subnet":  {"192.168.2.40", "00:1a:2b:3c:4d:5e", "192.168.1.5/24", ErrNoLocalSubnet},
		"not an address":   {"printer.local", "00:1a:2b:3c:4d:5e", "192.168.1.5/24", ErrNoLocalSubnet},
		"public broadcast": {"8.8.8.40", "00:1a:2b:3c:4d:5e", "8.8.8.1/24", ErrPublicBroadcast},
	} {
		var sent []sentPacket
		w := fakeWaker(&sent, tc.prefix)
		if err := w.Wake(context.Background(), tc.ip, tc.mac); !errors.Is(err, tc.want) {
			t.Errorf("%s: error = %v, want %v", name, err, tc.want)
		}
		if len(sent) != 0 {
			t.Errorf("%s: %d packets were sent, want none", name, len(sent))
		}
	}
}

func TestWakeStopsBetweenRepeatsWhenTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var sent []sentPacket
	w := fakeWaker(&sent, "192.168.1.5/24")
	w.Gap = time.Hour // the cancel, not the gap, ends the wait
	inner := w.Send
	w.Send = func(c context.Context, dst netip.AddrPort, p []byte) error {
		defer cancel()
		return inner(c, dst, p)
	}

	if err := w.Wake(ctx, "192.168.1.40", "00:1a:2b:3c:4d:5e"); !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
	if len(sent) != 1 {
		t.Errorf("sent %d packets, want 1", len(sent))
	}
}

func TestSendUDPDeliversTheDatagramToAListener(t *testing.T) {
	l, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }() // the test is over, a close error changes nothing
	packet := MagicPacket(net.HardwareAddr{1, 2, 3, 4, 5, 6})

	dst := l.LocalAddr().(*net.UDPAddr).AddrPort()
	if err := sendUDP(context.Background(), dst, packet); err != nil {
		t.Fatal(err)
	}
	if err := l.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 256)
	n, _, err := l.ReadFromUDP(buf)
	if err != nil || !bytes.Equal(buf[:n], packet) {
		t.Errorf("received % x, %v, want the packet", buf[:n], err)
	}
}
