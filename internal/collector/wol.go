package collector

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"
)

// The reasons Wake refuses to send. The API maps each of them to a 422.
var (
	// ErrNoMAC means the device has no usable MAC address to put into the packet.
	ErrNoMAC = errors.New("the device has no MAC address")
	// ErrNoLocalSubnet means the device address is in no subnet of a local interface.
	ErrNoLocalSubnet = errors.New("the device address is in no local subnet")
	// ErrPublicBroadcast means the broadcast address of that subnet is outside the private ranges.
	ErrPublicBroadcast = errors.New("the broadcast address is outside the private ranges")
)

const (
	wolPort    = 9
	wolRepeats = 3
	wolGap     = 100 * time.Millisecond
)

// MagicPacket returns the Wake on LAN payload for a 6 byte MAC: six bytes of 0xFF, then
// the MAC sixteen times.
func MagicPacket(mac net.HardwareAddr) []byte {
	p := make([]byte, 0, 6+16*len(mac))
	for range 6 {
		p = append(p, 0xFF)
	}
	for range 16 {
		p = append(p, mac...)
	}
	return p
}

// BroadcastFor returns the broadcast address of the first local prefix that holds ip. It
// returns ErrNoLocalSubnet when none does and ErrPublicBroadcast when the broadcast
// address is not in a private range. Only IPv4 has a broadcast address.
func BroadcastFor(ip netip.Addr, local []netip.Prefix) (netip.Addr, error) {
	ip = ip.Unmap()
	if !ip.Is4() {
		return netip.Addr{}, ErrNoLocalSubnet
	}
	for _, p := range local {
		p = p.Masked()
		if !p.Addr().Is4() || p.Bits() > 30 || !p.Contains(ip) {
			continue // a /31 or /32 has no broadcast address
		}
		b := p.Addr().As4()
		host := uint(32 - p.Bits())
		for i := range b {
			shift := uint(8 * (3 - i))
			if shift < host {
				b[i] |= byte(0xFF >> (8 - min(host-shift, 8)))
			}
		}
		bc := netip.AddrFrom4(b)
		if !bc.IsPrivate() {
			return netip.Addr{}, ErrPublicBroadcast
		}
		return bc, nil
	}
	return netip.Addr{}, ErrNoLocalSubnet
}

// Waker sends magic packets. It only sends: it never reads a reply and never probes a device.
type Waker struct {
	// Prefixes lists the subnets of the local interfaces. Nil reads the running interfaces.
	Prefixes func() ([]netip.Prefix, error)
	// Send writes one datagram. Nil sends a UDP datagram.
	Send func(ctx context.Context, dst netip.AddrPort, packet []byte) error
	// Gap is the pause between the repeats. Zero means 100 ms.
	Gap time.Duration
}

// Wake sends the magic packet for mac three times, as a UDP broadcast to port 9 on the
// broadcast address of the local subnet that holds ip. The MAC must be the stored one.
func (w *Waker) Wake(ctx context.Context, ip, mac string) error {
	hw, err := net.ParseMAC(mac)
	if err != nil || len(hw) != 6 {
		return ErrNoMAC
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ErrNoLocalSubnet
	}
	prefixes := w.Prefixes
	if prefixes == nil {
		prefixes = wakePrefixes
	}
	local, err := prefixes()
	if err != nil {
		return fmt.Errorf("list local interfaces: %w", err)
	}
	bc, err := BroadcastFor(addr, local)
	if err != nil {
		return err
	}
	send := w.Send
	if send == nil {
		send = sendUDP
	}
	gap := w.Gap
	if gap == 0 {
		gap = wolGap
	}
	dst := netip.AddrPortFrom(bc, wolPort)
	packet := MagicPacket(hw)
	for i := range wolRepeats {
		if i > 0 {
			t := time.NewTimer(gap)
			select {
			case <-ctx.Done():
				t.Stop()
				return ctx.Err()
			case <-t.C:
			}
		}
		if err := send(ctx, dst, packet); err != nil {
			return fmt.Errorf("send magic packet: %w", err)
		}
	}
	return nil
}

// wakePrefixes returns the IPv4 subnets of the interfaces that are up and can broadcast.
func wakePrefixes() ([]netip.Prefix, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var out []netip.Prefix
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagBroadcast == 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue // an interface that vanished while we looked is not an error
		}
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip, ok := netip.AddrFromSlice(n.IP)
			if !ok {
				continue
			}
			ones, _ := n.Mask.Size()
			out = append(out, netip.PrefixFrom(ip.Unmap(), ones))
		}
	}
	return out, nil
}

// sendUDP writes one datagram. Go sets SO_BROADCAST on UDP sockets, so a broadcast
// destination works.
func sendUDP(ctx context.Context, dst netip.AddrPort, packet []byte) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp4", dst.String())
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write(packet)
	return err
}
