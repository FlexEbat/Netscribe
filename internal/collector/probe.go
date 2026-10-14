package collector

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
)

const (
	probeConcurrency = 128 // section 9.6 of the spec
	probeTimeout     = time.Second
	maxProbeHosts    = 4096
	minProbeBits     = 16 // a shorter prefix is not probed host by host
)

// probePorts are the TCP ports a probe tries. Any answer, a refusal included, means a
// host is there.
var probePorts = []int{22, 80, 443}

// icmpPinger sends one ICMP echo request and tells whether the echo reply came back.
type icmpPinger interface {
	Ping(ctx context.Context, ip netip.Addr, timeout time.Duration) (bool, error)
}

// dialer opens a TCP connection. *net.Dialer.DialContext satisfies it.
type dialer func(ctx context.Context, network, address string) (net.Conn, error)

// Probe finds live hosts in subnets behind a router, where ARP does not reach. It only
// runs when discovery.probeRoutedTargets is true.
type Probe struct {
	ping  icmpPinger // nil when ICMP is not available
	dial  dialer
	local func() []netip.Prefix
	log   *slog.Logger

	mu    sync.Mutex
	ports map[string][]int // ip -> open probe ports
}

// NewProbe returns a probe that uses an unprivileged ICMP socket and TCP connects. When
// the process may not open the ICMP socket (net.ipv4.ping_group_range), only TCP is used.
func NewProbe(log *slog.Logger) *Probe {
	p := &Probe{
		dial:  (&net.Dialer{Timeout: probeTimeout}).DialContext,
		local: localPrefixes,
		log:   log,
		ports: map[string][]int{},
	}
	pinger, err := newICMPPinger()
	if err != nil {
		log.Info("ICMP probes are off, TCP probes only (see net.ipv4.ping_group_range)", "err", err)
	} else {
		p.ping = pinger
	}
	return p
}

func (p *Probe) Name() string { return "probe" }

// openPorts returns the probe ports that accepted a connection on ip.
func (p *Probe) openPorts(ip string) []int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ports[ip]
}

// localPrefixes lists the networks of the machine's own interfaces, the L2 segments ARP
// can see.
func localPrefixes() []netip.Prefix {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []netip.Prefix
	for _, a := range addrs {
		n, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		if ip, ok := netip.AddrFromSlice(n.IP); ok {
			ones, _ := n.Mask.Size()
			out = append(out, netip.PrefixFrom(ip.Unmap(), ones).Masked())
		}
	}
	return out
}

// routedTargets returns the targets that are not inside a local network.
func routedTargets(targets, local []netip.Prefix) []netip.Prefix {
	var out []netip.Prefix
	for _, t := range targets {
		t = t.Masked()
		inside := false
		for _, l := range local {
			if l.Bits() <= t.Bits() && l.Contains(t.Addr()) {
				inside = true
				break
			}
		}
		if !inside {
			out = append(out, t)
		}
	}
	return out
}

func (p *Probe) Collect(ctx context.Context, in Input) (Result, error) {
	routed := routedTargets(in.Targets, p.local())
	if len(routed) == 0 {
		return Result{}, nil
	}
	if !in.Config.Discovery.ProbeRoutedTargets {
		p.log.Info("targets outside the local segment are not probed: discovery.probeRoutedTargets is false",
			"targets", len(routed))
		return Result{}, nil
	}

	var usable []netip.Prefix
	for _, t := range routed {
		if t.Bits() < minProbeBits {
			p.log.Warn("target is too large to probe host by host, skipped", "target", t.String(), "minBits", minProbeBits)
			continue
		}
		usable = append(usable, t)
	}
	addrs := hostAddrs(usable)
	if len(addrs) > maxProbeHosts {
		p.log.Warn("too many hosts to probe, only the first ones are probed", "hosts", len(addrs), "limit", maxProbeHosts)
		addrs = addrs[:maxProbeHosts]
	}

	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		res Result
		sem = make(chan struct{}, probeConcurrency)
	)
loop:
	for _, a := range addrs {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break loop
		}
		wg.Add(1)
		go func(a netip.Addr) {
			defer wg.Done()
			defer func() { <-sem }()
			alive, open := p.probeHost(ctx, a)
			if !alive {
				return
			}
			ip := a.String()
			p.mu.Lock()
			p.ports[ip] = open
			p.mu.Unlock()
			mu.Lock()
			res.Devices = append(res.Devices, model.DeviceInput{Key: model.DeviceKey{IP: ip}, Source: "probe"})
			mu.Unlock()
		}(a)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	sort.Slice(res.Devices, func(i, j int) bool {
		a, _ := netip.ParseAddr(res.Devices[i].Key.IP)
		b, _ := netip.ParseAddr(res.Devices[j].Key.IP)
		return a.Less(b)
	})
	return res, nil
}

// probeHost tries ICMP, then the TCP ports. open lists the ports that accepted.
func (p *Probe) probeHost(ctx context.Context, a netip.Addr) (alive bool, open []int) {
	if p.ping != nil {
		if ok, err := p.ping.Ping(ctx, a, probeTimeout); err == nil && ok {
			alive = true
		}
	}
	for _, port := range probePorts {
		if ctx.Err() != nil {
			return alive, open
		}
		cctx, cancel := context.WithTimeout(ctx, probeTimeout)
		conn, err := p.dial(cctx, "tcp", net.JoinHostPort(a.String(), strconv.Itoa(port)))
		cancel()
		switch {
		case err == nil:
			_ = conn.Close() // only the fact that it connected matters
			alive = true
			open = append(open, port)
		case errors.Is(err, syscall.ECONNREFUSED):
			alive = true // a RST: the host is there, the port is closed
		}
	}
	return alive, open
}
