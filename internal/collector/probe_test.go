package collector

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

type fakePinger struct {
	alive map[string]bool
	calls atomic.Int32
}

func (f *fakePinger) Ping(_ context.Context, ip netip.Addr, _ time.Duration) (bool, error) {
	f.calls.Add(1)
	return f.alive[ip.String()], nil
}

type fakeConn struct{ net.Conn }

func (fakeConn) Close() error { return nil }

// fakeDial: address -> nil (connects), syscall.ECONNREFUSED (a RST) or another error.
func fakeDial(answers map[string]error) dialer {
	return func(_ context.Context, _, address string) (net.Conn, error) {
		err, ok := answers[address]
		if !ok {
			return nil, errors.New("i/o timeout")
		}
		if err != nil {
			return nil, err
		}
		return fakeConn{}, nil
	}
}

func newTestProbe(ping icmpPinger, dial dialer, local ...string) (*Probe, *bytes.Buffer) {
	var logs bytes.Buffer
	var prefixes []netip.Prefix
	for _, l := range local {
		prefixes = append(prefixes, netip.MustParsePrefix(l))
	}
	return &Probe{
		ping: ping, dial: dial, local: func() []netip.Prefix { return prefixes },
		log:   slog.New(slog.NewTextHandler(&logs, nil)),
		ports: map[string][]int{},
	}, &logs
}

func probeInput(enabled bool, targets ...string) Input {
	in := testConfig()
	in.Discovery.ProbeRoutedTargets = enabled
	var ts []netip.Prefix
	for _, t := range targets {
		ts = append(ts, netip.MustParsePrefix(t))
	}
	return Input{Targets: ts, Config: in}
}

func TestProbeIsOffByDefaultAndSaysWhy(t *testing.T) {
	ping := &fakePinger{}
	p, logs := newTestProbe(ping, fakeDial(nil), "192.168.1.0/24")
	res, err := p.Collect(context.Background(), probeInput(false, "192.168.1.0/24", "10.1.0.0/29"))
	if err != nil || len(res.Devices) != 0 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if ping.calls.Load() != 0 {
		t.Error("a probe was sent although probeRoutedTargets is false")
	}
	if !strings.Contains(logs.String(), "discovery.probeRoutedTargets is false") {
		t.Errorf("the log does not give the reason: %s", logs)
	}
}

func TestProbeStaysQuietWhenEveryTargetIsLocal(t *testing.T) {
	p, logs := newTestProbe(&fakePinger{}, fakeDial(nil), "192.168.1.0/24")
	if _, err := p.Collect(context.Background(), probeInput(false, "192.168.1.0/25")); err != nil {
		t.Fatal(err)
	}
	if logs.Len() != 0 {
		t.Errorf("nothing is routed, nothing to say: %s", logs)
	}
}

func TestProbeFindsHostsByICMPAndTCP(t *testing.T) {
	ping := &fakePinger{alive: map[string]bool{"10.1.0.1": true}}
	dial := fakeDial(map[string]error{
		"10.1.0.2:22":  nil,                    // ssh open
		"10.1.0.2:443": nil,                    // https open
		"10.1.0.3:80":  syscall.ECONNREFUSED,   // a RST: alive, nothing open
		"10.1.0.4:80":  errors.New("filtered"), // no answer: not alive
	})
	p, _ := newTestProbe(ping, dial, "192.168.1.0/24")
	res, err := p.Collect(context.Background(), probeInput(true, "10.1.0.0/29"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range res.Devices {
		if d.Source != "probe" || d.Key.MAC != "" {
			t.Errorf("device = %+v", d)
		}
		got = append(got, d.Key.IP)
	}
	if strings.Join(got, ",") != "10.1.0.1,10.1.0.2,10.1.0.3" {
		t.Errorf("live hosts = %v", got)
	}
	if open := p.openPorts("10.1.0.2"); len(open) != 2 || open[0] != 22 || open[1] != 443 {
		t.Errorf("open ports of 10.1.0.2 = %v", open)
	}
	if open := p.openPorts("10.1.0.3"); len(open) != 0 {
		t.Errorf("a refused port is not open: %v", open)
	}
}

func TestProbeWorksWithoutICMP(t *testing.T) {
	p, _ := newTestProbe(nil, fakeDial(map[string]error{"10.1.0.2:80": nil}), "192.168.1.0/24")
	res, err := p.Collect(context.Background(), probeInput(true, "10.1.0.0/30"))
	if err != nil || len(res.Devices) != 1 || res.Devices[0].Key.IP != "10.1.0.2" {
		t.Errorf("res = %+v, err = %v", res, err)
	}
}

func TestProbeSkipsHugeTargets(t *testing.T) {
	ping := &fakePinger{}
	p, logs := newTestProbe(ping, fakeDial(nil), "192.168.1.0/24")
	if _, err := p.Collect(context.Background(), probeInput(true, "10.0.0.0/8")); err != nil {
		t.Fatal(err)
	}
	if ping.calls.Load() != 0 || !strings.Contains(logs.String(), "too large") {
		t.Errorf("calls = %d, log = %s", ping.calls.Load(), logs)
	}
}

func TestProbeCapsTheNumberOfHosts(t *testing.T) {
	ping := &fakePinger{}
	p, logs := newTestProbe(ping, fakeDial(nil), "192.168.1.0/24")
	if _, err := p.Collect(context.Background(), probeInput(true, "10.2.0.0/16")); err != nil {
		t.Fatal(err)
	}
	if got := ping.calls.Load(); got != maxProbeHosts {
		t.Errorf("probed %d hosts, want the limit %d", got, maxProbeHosts)
	}
	if !strings.Contains(logs.String(), "too many hosts") {
		t.Errorf("no warning about the limit: %s", logs)
	}
}

func TestProbeStopsWhenCanceled(t *testing.T) {
	p, _ := newTestProbe(&fakePinger{}, fakeDial(nil), "192.168.1.0/24")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Collect(ctx, probeInput(true, "10.1.0.0/24")); !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestRoutedTargets(t *testing.T) {
	local := []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")}
	targets := []netip.Prefix{
		netip.MustParsePrefix("192.168.1.0/25"), // inside the local network
		netip.MustParsePrefix("192.168.0.0/16"), // wider than the local network: not inside
		netip.MustParsePrefix("10.1.0.0/24"),
	}
	got := routedTargets(targets, local)
	if len(got) != 2 || got[0].String() != "192.168.0.0/16" || got[1].String() != "10.1.0.0/24" {
		t.Errorf("routed = %v", got)
	}
}

func TestEchoRequestChecksum(t *testing.T) {
	msg := echoRequest(7)
	if msg[0] != icmpEchoRequest || msg[7] != 7 {
		t.Errorf("message = % x", msg)
	}
	if icmpChecksum(msg) != 0 {
		t.Errorf("a message with a correct checksum sums to zero, got %#04x", icmpChecksum(msg))
	}
}
