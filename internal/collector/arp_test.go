package collector

import (
	"context"
	"errors"
	"io"
	"net/netip"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
)

func prefixes(t *testing.T, list ...string) []netip.Prefix {
	t.Helper()
	out := make([]netip.Prefix, len(list))
	for i, s := range list {
		out[i] = netip.MustParsePrefix(s)
	}
	return out
}

func TestParseARPTable(t *testing.T) {
	f, err := os.Open("testdata/arp.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	got, err := parseARPTable(f)
	if err != nil {
		t.Fatal(err)
	}
	want := []arpEntry{
		{netip.MustParseAddr("192.168.1.1"), "aa:bb:cc:00:00:01"},  // upper case MAC is normalized
		{netip.MustParseAddr("192.168.1.20"), "aa:bb:cc:00:00:20"}, // flag 0x2
		{netip.MustParseAddr("192.168.1.21"), "aa:bb:cc:00:00:21"}, // flags 0x6 include 0x2
		{netip.MustParseAddr("192.168.1.40"), "aa:bb:cc:00:00:01"}, // same MAC on a second IP is kept here
		{netip.MustParseAddr("10.9.9.9"), "aa:bb:cc:00:00:99"},     // target filtering happens later
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestParseARPTableEmptyAndHeaderOnly(t *testing.T) {
	for _, in := range []string{"", "IP address       HW type     Flags       HW address            Mask     Device\n"} {
		got, err := parseARPTable(strings.NewReader(in))
		if err != nil || len(got) != 0 {
			t.Errorf("parseARPTable(%q) = %v, %v; want nothing", in, got, err)
		}
	}
}

func TestHostAddrs(t *testing.T) {
	tests := []struct {
		name    string
		targets []string
		count   int
		first   string
		last    string
	}{
		{"/24 skips network and broadcast", []string{"192.168.1.0/24"}, 254, "192.168.1.1", "192.168.1.254"},
		{"/30 has two hosts", []string{"192.168.1.0/30"}, 2, "192.168.1.1", "192.168.1.2"},
		{"/31 keeps both", []string{"192.168.1.4/31"}, 2, "192.168.1.4", "192.168.1.5"},
		{"/32 is one address", []string{"192.168.1.9/32"}, 1, "192.168.1.9", "192.168.1.9"},
		{"unmasked host bits", []string{"192.168.1.77/24"}, 254, "192.168.1.1", "192.168.1.254"},
		{"overlap is deduplicated", []string{"192.168.1.0/24", "192.168.1.0/30"}, 254, "192.168.1.1", "192.168.1.254"},
		{"/16", []string{"10.1.0.0/16"}, 65534, "10.1.0.1", "10.1.255.254"},
		{"top of the address space", []string{"255.255.255.254/31"}, 2, "255.255.255.254", "255.255.255.255"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hostAddrs(prefixes(t, tt.targets...))
			if len(got) != tt.count {
				t.Fatalf("got %d addresses, want %d", len(got), tt.count)
			}
			if got[0].String() != tt.first || got[len(got)-1].String() != tt.last {
				t.Errorf("range %s..%s, want %s..%s", got[0], got[len(got)-1], tt.first, tt.last)
			}
		})
	}
}

func TestDevicesFromARP(t *testing.T) {
	entries := []arpEntry{
		{netip.MustParseAddr("192.168.1.40"), "aa:bb:cc:00:00:01"}, // second IP of the router
		{netip.MustParseAddr("192.168.1.20"), "aa:bb:cc:00:00:20"},
		{netip.MustParseAddr("192.168.1.3"), "aa:bb:cc:00:00:03"},
		{netip.MustParseAddr("192.168.1.1"), "aa:bb:cc:00:00:01"},
		{netip.MustParseAddr("10.9.9.9"), "aa:bb:cc:00:00:99"},    // outside the targets
		{netip.MustParseAddr("192.168.1.3"), "aa:bb:cc:00:00:33"}, // IP seen twice
	}
	got := devicesFromARP(entries, prefixes(t, "192.168.1.0/24"))
	var ips []string
	for _, d := range got {
		ips = append(ips, d.Key.IP+"="+d.Key.MAC)
		if d.Source != "arp" || d.Kind != model.KindUnknown {
			t.Errorf("device %v: source %q kind %q", d.Key, d.Source, d.Kind)
		}
	}
	want := "192.168.1.1=aa:bb:cc:00:00:01 192.168.1.3=aa:bb:cc:00:00:03 192.168.1.20=aa:bb:cc:00:00:20"
	if strings.Join(ips, " ") != want {
		t.Errorf("devices = %v\nwant     %s", ips, want)
	}
}

type fakeProber struct {
	mu      sync.Mutex
	sent    []netip.Addr
	cur     atomic.Int32
	max     atomic.Int32
	hold    time.Duration
	failAll error
	failIP  map[netip.Addr]bool
	closed  atomic.Bool
}

func (p *fakeProber) Send(ctx context.Context, ip netip.Addr) error {
	n := p.cur.Add(1)
	defer p.cur.Add(-1)
	for {
		m := p.max.Load()
		if n <= m || p.max.CompareAndSwap(m, n) {
			break
		}
	}
	if p.hold > 0 {
		select {
		case <-time.After(p.hold):
		case <-ctx.Done():
		}
	}
	if p.failAll != nil {
		return p.failAll
	}
	if p.failIP[ip] {
		return errors.New("no route to host")
	}
	p.mu.Lock()
	p.sent = append(p.sent, ip)
	p.mu.Unlock()
	return nil
}

func (p *fakeProber) Close() error { p.closed.Store(true); return nil }

func newTestARP(p *fakeProber, table string) *ARP {
	return &ARP{
		newProber: func() (prober, error) { return p, nil },
		openTable: func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(table)), nil },
		wait:      0,
	}
}

const tableOneHost = "IP address HW type Flags HW address Mask Device\n192.168.1.5 0x1 0x2 aa:bb:cc:00:00:05 * eth0\n"

func TestARPCollect(t *testing.T) {
	p := &fakeProber{}
	res, err := newTestARP(p, tableOneHost).Collect(context.Background(), Input{Targets: prefixes(t, "192.168.1.0/24")})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.sent) != 254 {
		t.Errorf("sent %d probes, want 254", len(p.sent))
	}
	if !p.closed.Load() {
		t.Error("the socket was not closed")
	}
	if len(res.Devices) != 1 || res.Devices[0].Key.MAC != "aa:bb:cc:00:00:05" || res.Devices[0].Key.IP != "192.168.1.5" {
		t.Errorf("devices = %+v", res.Devices)
	}
}

func TestARPCollectLimitsConcurrentSends(t *testing.T) {
	p := &fakeProber{hold: 2 * time.Millisecond}
	if _, err := newTestARP(p, "").Collect(context.Background(), Input{Targets: prefixes(t, "10.1.0.0/22")}); err != nil {
		t.Fatal(err)
	}
	if got := p.max.Load(); got > arpSendConcurrency {
		t.Errorf("%d sends in flight, limit is %d", got, arpSendConcurrency)
	}
	if got := p.max.Load(); got < 2 {
		t.Errorf("sends never overlapped (max %d): the pool is not concurrent", got)
	}
}

func TestARPCollectCancelStopsQuickly(t *testing.T) {
	p := &fakeProber{hold: time.Hour}
	a := newTestARP(p, "")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := a.Collect(ctx, Input{Targets: prefixes(t, "10.1.0.0/16")})
		done <- err
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Collect() = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Collect did not return within 2s of cancellation")
	}
}

func TestARPCollectCancelDuringWait(t *testing.T) {
	a := newTestARP(&fakeProber{}, tableOneHost)
	a.wait = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := a.Collect(ctx, Input{Targets: prefixes(t, "192.168.1.0/30")})
		done <- err
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Collect() = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Collect did not leave the wait on cancellation")
	}
}

func TestARPCollectFailsWhenNothingCouldBeSent(t *testing.T) {
	p := &fakeProber{failAll: errors.New("network is unreachable")}
	_, err := newTestARP(p, tableOneHost).Collect(context.Background(), Input{Targets: prefixes(t, "192.168.1.0/24")})
	if err == nil || !strings.Contains(err.Error(), "network is unreachable") {
		t.Fatalf("Collect() = %v, want the send error", err)
	}
}

func TestARPCollectToleratesSomeSendFailures(t *testing.T) {
	p := &fakeProber{failIP: map[netip.Addr]bool{netip.MustParseAddr("192.168.1.1"): true}}
	if _, err := newTestARP(p, tableOneHost).Collect(context.Background(), Input{Targets: prefixes(t, "192.168.1.0/30")}); err != nil {
		t.Fatalf("Collect() = %v, want success when only some hosts have no route", err)
	}
}

func TestARPCollectErrors(t *testing.T) {
	ctx := context.Background()
	in := Input{Targets: prefixes(t, "192.168.1.0/30")}

	a := newTestARP(&fakeProber{}, "")
	a.openTable = func() (io.ReadCloser, error) { return nil, errors.New("permission denied") }
	if _, err := a.Collect(ctx, in); err == nil || !strings.Contains(err.Error(), "read arp table") {
		t.Errorf("table error = %v", err)
	}

	b := newTestARP(&fakeProber{}, "")
	b.newProber = func() (prober, error) { return nil, errors.New("too many open files") }
	if _, err := b.Collect(ctx, in); err == nil || !strings.Contains(err.Error(), "open udp socket") {
		t.Errorf("socket error = %v", err)
	}

	if _, err := newTestARP(&fakeProber{}, "").Collect(ctx, Input{}); err == nil {
		t.Error("Collect() without targets succeeded")
	}
}

func TestARPWaitIsRespected(t *testing.T) {
	a := newTestARP(&fakeProber{}, tableOneHost)
	a.wait = 80 * time.Millisecond
	start := time.Now()
	if _, err := a.Collect(context.Background(), Input{Targets: prefixes(t, "192.168.1.0/30")}); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 80*time.Millisecond {
		t.Errorf("Collect returned after %v, before the ARP wait of 80ms", elapsed)
	}
}

func TestARPName(t *testing.T) {
	if NewARP().Name() != "arp" {
		t.Error("collector name must be arp")
	}
}
