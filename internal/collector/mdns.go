package collector

import (
	"context"
	"encoding/binary"
	"log/slog"
	"net"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
)

const (
	multicastWindow   = 3 * time.Second // how long answers to a multicast query are collected
	maxDatagram       = 9000            // a longer datagram is dropped
	maxDatagramsPerIf = 512             // the most answers read on one interface
)

var (
	mdnsGroup = netip.MustParseAddrPort("224.0.0.251:5353")
	// mdnsQuestion is the name every device that offers a service answers to.
	mdnsQuestion = "_services._dns-sd._udp.local"
)

// datagram is one packet that came back from a multicast query.
type datagram struct {
	from netip.Addr
	data []byte
}

// multicastTransport sends one packet to a multicast group from every local interface
// and returns the packets that came back within the window. Tests replace it.
type multicastTransport interface {
	Exchange(ctx context.Context, group netip.AddrPort, payload []byte, window time.Duration) []datagram
}

// udpMulticast is the real transport. It sends from a socket bound to the address of each
// interface that is up, can multicast and is not loopback, so the kernel sends the packet
// out of that interface and the packet stays in its segment. An answer counts only when
// it comes from an address inside the subnet of that interface.
type udpMulticast struct{ log *slog.Logger }

func (t udpMulticast) Exchange(ctx context.Context, group netip.AddrPort, payload []byte, window time.Duration) []datagram {
	ifaces, err := net.Interfaces()
	if err != nil {
		t.log.Warn("list network interfaces", "err", err)
		return nil
	}
	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		out []datagram
	)
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagMulticast == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil {
				continue
			}
			wg.Add(1)
			go func(ipnet *net.IPNet) {
				defer wg.Done()
				got := t.exchangeOn(ctx, ipnet, group, payload, window)
				mu.Lock()
				out = append(out, got...)
				mu.Unlock()
			}(ipnet)
		}
	}
	wg.Wait()
	return out
}

func (t udpMulticast) exchangeOn(ctx context.Context, ipnet *net.IPNet, group netip.AddrPort, payload []byte, window time.Duration) []datagram {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ipnet.IP})
	if err != nil {
		t.log.Debug("multicast socket", "addr", ipnet.IP, "err", err)
		return nil
	}
	defer func() { _ = conn.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	deadline := time.Now().Add(window)
	_ = conn.SetDeadline(deadline)
	if _, err := conn.WriteToUDP(payload, net.UDPAddrFromAddrPort(group)); err != nil {
		t.log.Debug("multicast send", "addr", ipnet.IP, "err", err)
		return nil
	}
	var out []datagram
	buf := make([]byte, maxDatagram+1)
	for len(out) < maxDatagramsPerIf {
		n, from, err := conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			return out // the window ended or the scan was canceled
		}
		src := from.Addr().Unmap()
		if n > maxDatagram || !ipnet.Contains(src.AsSlice()) {
			continue
		}
		out = append(out, datagram{from: src, data: append([]byte(nil), buf[:n]...)})
	}
	return out
}

// MDNS asks the local segments which services their devices offer. It sends one PTR query
// for _services._dns-sd._udp.local, reads the answers for three seconds and keeps the
// host name (.local) and the types of the services, such as _ipp._tcp.
type MDNS struct {
	log *slog.Logger
	net multicastTransport

	mu    sync.Mutex
	types map[string][]string // ip -> service types of the last Collect
}

func NewMDNS(log *slog.Logger) *MDNS {
	return &MDNS{log: log, net: udpMulticast{log: log}, types: map[string][]string{}}
}

func (c *MDNS) Name() string { return "mdns" }

// serviceTypes returns the service types ip announced in the last Collect.
func (c *MDNS) serviceTypes(ip string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.types[ip]
}

// Collect sends the query. discovery.mdns off sends nothing. The host name of a device
// goes into the result only when the device has no name yet.
func (c *MDNS) Collect(ctx context.Context, in Input) (Result, error) {
	if !in.Config.Discovery.MDNS {
		return Result{}, nil
	}
	known := map[string]model.Device{}
	for _, d := range in.Devices {
		if d.IP != "" {
			known[d.IP] = d
		}
	}
	types := map[string][]string{}
	names := map[string]string{}
	for _, dg := range c.net.Exchange(ctx, mdnsGroup, mdnsQuery(), multicastWindow) {
		ip := dg.from.String()
		if _, ok := known[ip]; !ok {
			continue // only devices the scan found
		}
		msg, ok := parseMDNS(dg.data)
		if !ok {
			continue
		}
		for _, t := range msg.serviceTypes {
			if len(types[ip]) < maxNetworkFields && !contains(types[ip], t) {
				types[ip] = append(types[ip], t)
			}
		}
		if name := msg.hostFor(dg.from); name != "" && names[ip] == "" {
			names[ip] = name
		}
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	for _, list := range types {
		sort.Strings(list)
	}
	c.mu.Lock()
	c.types = types
	c.mu.Unlock()

	var res Result
	ips := make([]string, 0, len(names))
	for ip := range names {
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	for _, ip := range ips {
		d := known[ip]
		if d.Hostname != "" {
			continue // a name that is known stays
		}
		res.Devices = append(res.Devices, model.DeviceInput{
			Key: model.DeviceKey{MAC: d.MAC, IP: d.IP}, Hostname: names[ip], Source: "mdns",
		})
	}
	return res, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// mdnsQuery builds the PTR question for _services._dns-sd._udp.local. The ID is 0 and
// the unicast-response bit is not set: the source port is not 5353, so answers come back
// to the socket as unicast.
func mdnsQuery() []byte {
	msg := make([]byte, 12, 64)
	binary.BigEndian.PutUint16(msg[4:], 1) // one question
	for _, label := range strings.Split(mdnsQuestion, ".") {
		msg = append(msg, byte(len(label))) //nolint:gosec // the labels are constants, none is longer than 63 bytes
		msg = append(msg, label...)
	}
	msg = append(msg, 0)
	msg = binary.BigEndian.AppendUint16(msg, 12) // PTR
	msg = binary.BigEndian.AppendUint16(msg, 1)  // class IN
	return msg
}

// mdnsMessage is what is kept from one answer packet.
type mdnsMessage struct {
	serviceTypes []string
	hosts        map[string]netip.Addr // a .local name -> its A record
}

// hostFor returns the .local name whose address record points at from.
func (m mdnsMessage) hostFor(from netip.Addr) string {
	var found []string
	for name, addr := range m.hosts {
		if addr == from {
			found = append(found, name)
		}
	}
	sort.Strings(found)
	if len(found) == 0 {
		return ""
	}
	return found[0]
}

const (
	dnsTypeA   = 1
	dnsTypePTR = 12
	maxDNSRRs  = 256 // the most records read from one packet
)

// parseMDNS reads an answer. A packet that is cut short, has a loop in its name
// compression, is not a response or holds text that is not UTF-8 is dropped whole.
func parseMDNS(b []byte) (mdnsMessage, bool) {
	if len(b) < 12 || b[2]&0x80 == 0 { // the QR bit: a response
		return mdnsMessage{}, false
	}
	qd := int(binary.BigEndian.Uint16(b[4:]))
	rrs := int(binary.BigEndian.Uint16(b[6:])) + int(binary.BigEndian.Uint16(b[8:])) + int(binary.BigEndian.Uint16(b[10:]))
	if qd > 16 || rrs > maxDNSRRs {
		return mdnsMessage{}, false
	}
	off := 12
	for i := 0; i < qd; i++ {
		_, next, ok := readDNSName(b, off)
		if !ok || next+4 > len(b) {
			return mdnsMessage{}, false
		}
		off = next + 4
	}
	msg := mdnsMessage{hosts: map[string]netip.Addr{}}
	for i := 0; i < rrs; i++ {
		name, next, ok := readDNSName(b, off)
		if !ok || next+10 > len(b) {
			return mdnsMessage{}, false
		}
		typ := binary.BigEndian.Uint16(b[next:])
		rdlen := int(binary.BigEndian.Uint16(b[next+8:]))
		start := next + 10
		if start+rdlen > len(b) {
			return mdnsMessage{}, false
		}
		switch typ {
		case dnsTypePTR:
			target, _, ok := readDNSName(b, start)
			if !ok {
				return mdnsMessage{}, false
			}
			if strings.EqualFold(name, mdnsQuestion) {
				if t, ok := serviceType(target); ok {
					msg.serviceTypes = append(msg.serviceTypes, t)
				}
			}
		case dnsTypeA:
			if rdlen == 4 {
				addr := netip.AddrFrom4([4]byte(b[start : start+4]))
				if host := strings.ToLower(name); strings.HasSuffix(host, ".local") && networkText(host) == host && len(host) <= 255 {
					msg.hosts[host] = addr
				}
			}
		}
		off = start + rdlen
	}
	return msg, true
}

// serviceType turns "_ipp._tcp.local" into "_ipp._tcp". Anything that does not look like
// a service type is refused.
func serviceType(name string) (string, bool) {
	t := strings.TrimSuffix(strings.ToLower(name), ".local")
	parts := strings.Split(t, ".")
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "_") || (parts[1] != "_tcp" && parts[1] != "_udp") {
		return "", false
	}
	if len(parts[0]) < 2 || len(parts[0]) > 40 || networkText(t) != t || strings.ContainsAny(t, " ,\"") {
		return "", false
	}
	return t, true
}

// readDNSName reads a name that starts at off and returns it with the offset after it.
// Compression pointers are followed at most 16 times, and only backwards.
func readDNSName(b []byte, off int) (name string, next int, ok bool) {
	var labels []string
	next = -1
	jumps := 0
	total := 0
	for {
		if off >= len(b) {
			return "", 0, false
		}
		l := int(b[off])
		switch {
		case l == 0:
			if next < 0 {
				next = off + 1
			}
			return strings.Join(labels, "."), next, true
		case l&0xC0 == 0xC0:
			if off+1 >= len(b) {
				return "", 0, false
			}
			ptr := (l&0x3F)<<8 | int(b[off+1])
			if next < 0 {
				next = off + 2
			}
			jumps++
			if jumps > 16 || ptr >= off {
				return "", 0, false
			}
			off = ptr
		case l&0xC0 != 0:
			return "", 0, false
		default:
			if l > 63 || off+1+l > len(b) {
				return "", 0, false
			}
			total += l + 1
			if total > 255 {
				return "", 0, false
			}
			labels = append(labels, string(b[off+1:off+1+l]))
			off += 1 + l
		}
	}
}
