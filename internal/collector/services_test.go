package collector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestServices(dial dialer) *Services {
	return &Services{dial: dial, log: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), ports: map[string][]int{}}
}

func servicesInput(ports []int, ips ...string) Input {
	cfg := testConfig()
	cfg.Scan.Ports = ports
	return Input{Devices: liveDevices(ips...), Config: cfg}
}

func TestServicesFindsOpenPortsWithNames(t *testing.T) {
	c := newTestServices(fakeDial(map[string]error{
		"192.168.1.1:22":   nil,
		"192.168.1.1:443":  nil,
		"192.168.1.1:80":   syscallRefused(),
		"192.168.1.2:9100": nil,
		"192.168.1.2:4444": nil, // open, but not in the table: no name
	}))
	res, err := c.Collect(context.Background(), servicesInput([]int{22, 80, 443, 4444, 9100}, "192.168.1.1", "192.168.1.2"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range res.Services {
		if s.Proto != "tcp" {
			t.Errorf("protocol = %q", s.Proto)
		}
		got = append(got, fmt.Sprintf("%s:%d=%s", s.Device.IP, s.Port, s.Name))
	}
	want := []string{"192.168.1.1:22=ssh", "192.168.1.1:443=https", "192.168.1.2:4444=", "192.168.1.2:9100=pdl-datastream"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("services = %v, want %v", got, want)
	}
	if len(res.ServicesPolled) != 2 {
		t.Errorf("polled = %+v, want both devices", res.ServicesPolled)
	}
	if open := c.openPorts("192.168.1.2"); len(open) != 2 || open[0] != 4444 || open[1] != 9100 {
		t.Errorf("open ports = %v", open)
	}
}

func syscallRefused() error { return errors.New("connection refused") }

func TestServicesEmptyPortListSwitchesTheStageOff(t *testing.T) {
	var dials atomic.Int32
	c := newTestServices(func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, errors.New("must not be called")
	})
	for _, ports := range [][]int{nil, {}} {
		res, err := c.Collect(context.Background(), servicesInput(ports, "192.168.1.1"))
		if err != nil || len(res.Services) != 0 || len(res.ServicesPolled) != 0 || dials.Load() != 0 {
			t.Errorf("ports %v: res = %+v, err = %v, dials = %d", ports, res, err, dials.Load())
		}
	}
}

func TestServicesNeverHoldsMoreThan256Connections(t *testing.T) {
	var cur, peak, total atomic.Int32
	c := newTestServices(func(ctx context.Context, _, _ string) (net.Conn, error) {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		total.Add(1)
		select {
		case <-time.After(3 * time.Millisecond):
		case <-ctx.Done():
		}
		cur.Add(-1)
		return nil, errors.New("closed")
	})
	var ports []int
	for p := 1; p <= 40; p++ {
		ports = append(ports, p)
	}
	var ips []string
	for i := 1; i <= 30; i++ {
		ips = append(ips, fmt.Sprintf("10.0.0.%d", i))
	}
	if _, err := c.Collect(context.Background(), servicesInput(ports, ips...)); err != nil {
		t.Fatal(err)
	}
	if total.Load() != 1200 {
		t.Errorf("%d connection attempts, want 1200 (30 devices x 40 ports)", total.Load())
	}
	if peak.Load() > servicesConcurrency || peak.Load() < 2 {
		t.Errorf("peak concurrent connections = %d, want between 2 and %d", peak.Load(), servicesConcurrency)
	}
}

func TestServicesGivesEachPortHalfASecond(t *testing.T) {
	var got time.Duration
	var mu sync.Mutex
	c := newTestServices(func(ctx context.Context, _, _ string) (net.Conn, error) {
		d, ok := ctx.Deadline()
		mu.Lock()
		if ok {
			got = time.Until(d)
		}
		mu.Unlock()
		return nil, errors.New("closed")
	})
	if _, err := c.Collect(context.Background(), servicesInput([]int{22}, "192.168.1.1")); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got <= 0 || got > servicesTimeout {
		t.Errorf("deadline in %v, want at most %v", got, servicesTimeout)
	}
}

func TestServicesStopsWhenCanceled(t *testing.T) {
	c := newTestServices(fakeDial(nil))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Collect(ctx, servicesInput([]int{22}, "192.168.1.1")); !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestPortNames(t *testing.T) {
	cases := map[int]string{22: "ssh", 80: "http", 443: "https", 445: "microsoft-ds", 3389: "ms-wbt-server", 9100: "pdl-datastream", 4444: ""}
	for port, want := range cases {
		if got := portNameOf(port); got != want {
			t.Errorf("portNameOf(%d) = %q, want %q", port, got, want)
		}
	}
	for port, name := range wellKnownPorts {
		if port < 1 || port > 65535 || name == "" {
			t.Errorf("bad table entry %d -> %q", port, name)
		}
	}
}

func TestMergePorts(t *testing.T) {
	if got := mergePorts([]int{22, 80}, []int{80, 443}); fmt.Sprint(got) != "[22 80 443]" {
		t.Errorf("merged = %v", got)
	}
	if got := mergePorts(nil, nil); len(got) != 0 {
		t.Errorf("merged = %v", got)
	}
}
