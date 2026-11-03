package collector

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"slices"
	"strings"

	"github.com/FlexEbat/Netscribe/internal/model"
)

// The neighbour states that mean a host answered recently enough to be real. A neighbour
// that is incomplete, failed or has no address resolution is skipped.
const (
	nudReachable = 0x02
	nudStale     = 0x04
	nudDelay     = 0x08
	nudProbe     = 0x10
	nudPermanent = 0x80

	nudUsable = nudReachable | nudStale | nudDelay | nudProbe | nudPermanent
)

// Netlink and rtnetlink numbers, fixed by the kernel ABI.
const (
	rtmNewNeigh = 28
	nlmsgDone   = 3
	ndaDst      = 1
	ndaLLAddr   = 2
	afInet6     = 10
	ndmsgLen    = 12
	nlmsgHdrLen = 16
)

// ndpEntry is one usable row of the IPv6 neighbour table.
type ndpEntry struct {
	IP    netip.Addr
	MAC   string
	Iface int // interface index
}

// parseNeighbours reads the bytes of an RTM_GETNEIGH dump for AF_INET6. Rows that are not
// usable (incomplete, failed, no link layer address, multicast) and anything malformed are
// skipped: the bytes come from the kernel, but a short buffer must not panic.
func parseNeighbours(buf []byte) []ndpEntry {
	var out []ndpEntry
	for len(buf) >= nlmsgHdrLen {
		length := int(binary.NativeEndian.Uint32(buf[0:4]))
		typ := binary.NativeEndian.Uint16(buf[4:6])
		if length < nlmsgHdrLen || length > len(buf) {
			break
		}
		msg := buf[nlmsgHdrLen:length]
		next := (length + 3) &^ 3 // netlink messages are aligned to 4 bytes
		if typ == nlmsgDone {
			break
		}
		if typ == rtmNewNeigh {
			if e, ok := parseNeighbour(msg); ok {
				out = append(out, e)
			}
		}
		if next > len(buf) {
			break
		}
		buf = buf[next:]
	}
	return out
}

func parseNeighbour(msg []byte) (ndpEntry, bool) {
	if len(msg) < ndmsgLen || msg[0] != afInet6 {
		return ndpEntry{}, false
	}
	iface := int(binary.NativeEndian.Uint32(msg[4:8])) // an interface index is a small positive number
	state := binary.NativeEndian.Uint16(msg[8:10])
	if state&nudUsable == 0 {
		return ndpEntry{}, false
	}
	var ip netip.Addr
	var mac net.HardwareAddr
	for attrs := msg[ndmsgLen:]; len(attrs) >= 4; {
		alen := int(binary.NativeEndian.Uint16(attrs[0:2]))
		atyp := binary.NativeEndian.Uint16(attrs[2:4])
		if alen < 4 || alen > len(attrs) {
			break
		}
		payload := attrs[4:alen]
		switch atyp {
		case ndaDst:
			if len(payload) == 16 {
				ip, _ = netip.AddrFromSlice(payload)
			}
		case ndaLLAddr:
			if len(payload) == 6 {
				mac = net.HardwareAddr(slices.Clone(payload))
			}
		}
		step := (alen + 3) &^ 3
		if step > len(attrs) {
			break
		}
		attrs = attrs[step:]
	}
	if !ip.IsValid() || len(mac) != 6 || isZeroMAC(mac) || ip.Is4In6() || ip.IsMulticast() || ip.IsUnspecified() || ip.IsLoopback() {
		return ndpEntry{}, false
	}
	return ndpEntry{IP: ip, MAC: strings.ToLower(mac.String()), Iface: iface}, true
}

// addrClass ranks an address for the device column: 0 for a stable global or unique local
// address, 1 for a temporary or deprecated one, 2 for link local. Anything else (loopback,
// multicast) is not listed at all.
func addrClass(a netip.Addr, temporary, deprecated bool) (int, bool) {
	switch {
	case a.IsLinkLocalUnicast():
		return 2, true
	case a.IsGlobalUnicast():
		if temporary || deprecated {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// eui64Matches reports whether the interface identifier of a is the modified EUI-64 of mac:
// the address a device builds from its own hardware address, which stays the same for as
// long as the hardware does.
func eui64Matches(a netip.Addr, mac string) bool {
	hw, err := net.ParseMAC(mac)
	if err != nil || len(hw) != 6 {
		return false
	}
	b := a.As16()
	want := [8]byte{hw[0] ^ 0x02, hw[1], hw[2], 0xff, 0xfe, hw[3], hw[4], hw[5]}
	return bytes.Equal(b[8:], want[:])
}

// ip6Candidate is an address of one device with what is known about it. A neighbour table
// does not say whether an address is temporary; the interface address list of the host
// itself does, and that is where the flags come from when they are known.
type ip6Candidate struct {
	Addr       netip.Addr
	Temporary  bool
	Deprecated bool
}

// chooseIP6 picks the one address that goes into the device column: a global or unique
// local address that is neither temporary nor deprecated, then any other global or unique
// local one, then a link local one. Among equals the address built from the hardware
// address wins, because it does not change, and then the lowest address, so the choice is
// the same on every scan.
func chooseIP6(mac string, cands []ip6Candidate) (netip.Addr, bool) {
	type ranked struct {
		addr  netip.Addr
		class int
		stab  bool
	}
	var all []ranked
	for _, c := range cands {
		class, ok := addrClass(c.Addr, c.Temporary, c.Deprecated)
		if !ok {
			continue
		}
		all = append(all, ranked{addr: c.Addr, class: class, stab: eui64Matches(c.Addr, mac)})
	}
	if len(all) == 0 {
		return netip.Addr{}, false
	}
	slices.SortFunc(all, func(a, b ranked) int {
		if a.class != b.class {
			return a.class - b.class
		}
		if a.stab != b.stab {
			if a.stab {
				return -1
			}
			return 1
		}
		return a.addr.Compare(b.addr)
	})
	return all[0].addr, true
}

// neighbourReader returns the raw bytes of the IPv6 neighbour table. A variable so a test
// can feed a fixture, and so the platforms without netlink can say so.
type neighbourReader func(ctx context.Context) ([]byte, error)

// echoer sends one echo request to the all-nodes group on the named interfaces, so every
// host there answers and enters the neighbour table.
type echoer func(ctx context.Context, ifaces []string) error

// errNoNDP means this system has no way to read the IPv6 neighbour table.
var errNoNDP = errors.New("the IPv6 neighbour table can only be read on Linux")

// NDP finds IPv6 addresses. It sends one echo to ff02::1 on every interface that has a
// scan target, waits a moment and reads the neighbour table. A neighbour whose MAC belongs
// to a device the scan already has fills that device's IPv6 column; any other neighbour
// becomes a device of its own. It sends one packet per interface and never probes a host.
type NDP struct {
	read  neighbourReader
	echo  echoer
	ifmap func() ([]ndpInterface, error)
	log   *slog.Logger
}

// ndpInterface is a local interface with the prefixes of its addresses.
type ndpInterface struct {
	Index    int
	Name     string
	Prefixes []netip.Prefix
}

// NewNDP returns the collector for this system.
func NewNDP(log *slog.Logger) *NDP {
	return &NDP{read: readNeighbours, echo: echoAllNodes, ifmap: localInterfaces, log: log}
}

func (c *NDP) Name() string { return "ndp" }

// Collect does nothing when discovery.ipv6 is off. A system that cannot read the table
// costs one log line and nothing else.
func (c *NDP) Collect(ctx context.Context, in Input) (Result, error) {
	if !in.Config.Discovery.IPv6Enabled() {
		return Result{}, nil
	}
	ifaces, err := c.ifmap()
	if err != nil {
		return Result{}, fmt.Errorf("list interfaces: %w", err)
	}
	scanned := scannedInterfaces(ifaces, in.Targets)
	if len(scanned) == 0 {
		return Result{}, nil
	}
	names := make([]string, 0, len(scanned))
	index := make(map[int]struct{}, len(scanned))
	for _, i := range scanned {
		names = append(names, i.Name)
		index[i.Index] = struct{}{}
	}
	if err := c.echo(ctx, names); err != nil && ctx.Err() == nil {
		c.log.Info("the IPv6 echo was not sent, only the neighbours already known are read", "err", err)
	}
	raw, err := c.read(ctx)
	if err != nil {
		if errors.Is(err, errNoNDP) {
			c.log.Info("IPv6 discovery is off on this system", "err", err)
			return Result{}, nil
		}
		return Result{}, fmt.Errorf("read the neighbour table: %w", err)
	}
	return resultFromNeighbours(parseNeighbours(raw), index, in.Devices), nil
}

// scannedInterfaces are the interfaces that hold an address inside a scan target of either
// family. Those are the segments the scan was told to look at.
func scannedInterfaces(ifaces []ndpInterface, targets []netip.Prefix) []ndpInterface {
	var out []ndpInterface
	for _, i := range ifaces {
		for _, p := range i.Prefixes {
			if slices.ContainsFunc(targets, func(t netip.Prefix) bool { return t.Contains(p.Addr()) }) {
				out = append(out, i)
				break
			}
		}
	}
	return out
}

// resultFromNeighbours turns neighbour rows into device inputs. Devices without an IPv4
// address are keyed by MAC alone, the way the store identifies every device.
func resultFromNeighbours(entries []ndpEntry, allowed map[int]struct{}, known []model.Device) Result {
	byMAC := map[string][]ip6Candidate{}
	var order []string
	for _, e := range entries {
		if _, ok := allowed[e.Iface]; !ok {
			continue
		}
		if _, seen := byMAC[e.MAC]; !seen {
			order = append(order, e.MAC)
		}
		byMAC[e.MAC] = append(byMAC[e.MAC], ip6Candidate{Addr: e.IP})
	}
	slices.Sort(order)
	ip4 := map[string]string{}
	for _, d := range known {
		if d.MAC != "" {
			ip4[d.MAC] = d.IP
		}
	}
	var res Result
	for _, mac := range order {
		addr, ok := chooseIP6(mac, byMAC[mac])
		if !ok {
			continue
		}
		res.Devices = append(res.Devices, model.DeviceInput{
			Key:    model.DeviceKey{MAC: mac, IP: ip4[mac]},
			IP6:    addr.String(),
			Source: "ndp",
		})
	}
	return res
}

// localInterfaces lists the running interfaces with the prefixes of their addresses.
func localInterfaces() ([]ndpInterface, error) {
	all, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var out []ndpInterface
	for _, ifc := range all {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue // an interface that vanished while we looked is not an error
		}
		item := ndpInterface{Index: ifc.Index, Name: ifc.Name}
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if ip, ok := netip.AddrFromSlice(n.IP); ok {
				ones, _ := n.Mask.Size()
				item.Prefixes = append(item.Prefixes, netip.PrefixFrom(ip.Unmap(), ones))
			}
		}
		out = append(out, item)
	}
	return out, nil
}
