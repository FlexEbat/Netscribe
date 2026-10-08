package collector

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
)

const (
	arpSendConcurrency = 128
	arpWait            = 2 * time.Second
	arpTablePath       = "/proc/net/arp"
	arpFlagComplete    = 0x2 // ATF_COM: the kernel resolved the hardware address
	discardPort        = 9
)

// prober sends one datagram to an address. Sending makes the kernel resolve the
// address with an ARP request, which needs no raw socket and no root.
type prober interface {
	Send(ctx context.Context, ip netip.Addr) error
	Close() error
}

type udpProber struct{ conn net.PacketConn }

func newUDPProber() (prober, error) {
	conn, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		return nil, err
	}
	return &udpProber{conn: conn}, nil
}

func (p *udpProber) Send(_ context.Context, ip netip.Addr) error {
	_, err := p.conn.WriteTo([]byte{0}, net.UDPAddrFromAddrPort(netip.AddrPortFrom(ip, discardPort)))
	return err
}

func (p *udpProber) Close() error { return p.conn.Close() }

func openProcARP() (io.ReadCloser, error) { return os.Open(arpTablePath) }

// ARP discovers the hosts of the local L2 segment: it warms the kernel ARP cache
// with a datagram per address and reads /proc/net/arp.
type ARP struct {
	newProber func() (prober, error)
	openTable func() (io.ReadCloser, error)
	wait      time.Duration
}

// NewARP returns the collector wired to the real socket and /proc/net/arp.
func NewARP() *ARP {
	return &ARP{newProber: newUDPProber, openTable: openProcARP, wait: arpWait}
}

func (*ARP) Name() string { return "arp" }

func (a *ARP) Collect(ctx context.Context, in Input) (Result, error) {
	addrs := hostAddrs(in.Targets)
	if len(addrs) == 0 {
		return Result{}, errors.New("no addresses to scan")
	}
	p, err := a.newProber()
	if err != nil {
		return Result{}, fmt.Errorf("open udp socket: %w", err)
	}
	defer func() { _ = p.Close() }() // the socket is read-only for us: nothing to flush

	if err := warm(ctx, p, addrs); err != nil {
		return Result{}, err
	}

	timer := time.NewTimer(a.wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case <-timer.C:
	}

	rc, err := a.openTable()
	if err != nil {
		return Result{}, fmt.Errorf("read arp table: %w", err)
	}
	defer func() { _ = rc.Close() }() // read-only file
	entries, err := parseARPTable(rc)
	if err != nil {
		return Result{}, fmt.Errorf("read arp table: %w", err)
	}
	return Result{Devices: devicesFromARP(entries, in.Targets)}, nil
}

// warm sends one datagram per address with at most arpSendConcurrency in flight.
// Individual send errors are normal (a host without a route), but a scan where
// nothing could be sent says nothing about the network and must not look like
// "everybody went offline".
func warm(ctx context.Context, p prober, addrs []netip.Addr) error {
	jobs := make(chan netip.Addr)
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		sent     int
		firstErr error
	)
	workers := min(arpSendConcurrency, len(addrs))
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ip := range jobs {
				err := p.Send(ctx, ip)
				mu.Lock()
				if err == nil {
					sent++
				} else if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}()
	}
feed:
	for _, ip := range addrs {
		select {
		case jobs <- ip:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()

	if err := ctx.Err(); err != nil {
		return err
	}
	if sent == 0 {
		return fmt.Errorf("no probe could be sent: %w", firstErr)
	}
	return nil
}

// hostAddrs lists the probe addresses of the targets without duplicates.
// Network and broadcast addresses are skipped for prefixes of /30 and shorter.
func hostAddrs(targets []netip.Prefix) []netip.Addr {
	seen := make(map[netip.Addr]struct{})
	var out []netip.Addr
	for _, t := range targets {
		t = t.Masked()
		if !t.Addr().Is4() {
			continue
		}
		skipEnds := t.Bits() <= 30
		for a := t.Addr(); t.Contains(a); a = a.Next() {
			if skipEnds && (a == t.Addr() || !t.Contains(a.Next())) {
				continue
			}
			if _, dup := seen[a]; dup {
				continue
			}
			seen[a] = struct{}{}
			out = append(out, a)
		}
	}
	return out
}

type arpEntry struct {
	IP  netip.Addr
	MAC string
}

// parseARPTable reads the /proc/net/arp format. Incomplete entries (flag 0x2 not
// set, or an all-zero address) and anything unparsable are skipped.
func parseARPTable(r io.Reader) ([]arpEntry, error) {
	var out []arpEntry
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 4 {
			continue
		}
		ip, err := netip.ParseAddr(f[0])
		if err != nil || !ip.Is4() {
			continue // also skips the header line
		}
		flags, err := strconv.ParseUint(f[2], 0, 32)
		if err != nil || flags&arpFlagComplete == 0 {
			continue
		}
		hw, err := net.ParseMAC(f[3])
		if err != nil || len(hw) != 6 || isZeroMAC(hw) {
			continue
		}
		out = append(out, arpEntry{IP: ip, MAC: strings.ToLower(hw.String())})
	}
	return out, sc.Err()
}

func isZeroMAC(hw net.HardwareAddr) bool {
	for _, b := range hw {
		if b != 0 {
			return false
		}
	}
	return true
}

// devicesFromARP keeps the entries inside the targets, one device per MAC
// (the lowest IP wins when a device answers on several addresses), ordered by IP.
func devicesFromARP(entries []arpEntry, targets []netip.Prefix) []model.DeviceInput {
	var inside []arpEntry
	for _, e := range entries {
		if slices.ContainsFunc(targets, func(t netip.Prefix) bool { return t.Contains(e.IP) }) {
			inside = append(inside, e)
		}
	}
	slices.SortFunc(inside, func(a, b arpEntry) int { return a.IP.Compare(b.IP) })

	seenMAC := make(map[string]struct{})
	seenIP := make(map[netip.Addr]struct{})
	var out []model.DeviceInput
	for _, e := range inside {
		if _, dup := seenMAC[e.MAC]; dup {
			continue
		}
		if _, dup := seenIP[e.IP]; dup {
			continue
		}
		seenMAC[e.MAC] = struct{}{}
		seenIP[e.IP] = struct{}{}
		out = append(out, model.DeviceInput{
			Key:    model.DeviceKey{MAC: e.MAC, IP: e.IP.String()},
			Kind:   model.KindUnknown,
			Source: "arp",
		})
	}
	return out
}
