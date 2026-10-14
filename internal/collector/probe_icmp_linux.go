//go:build linux

package collector

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"syscall"
	"time"
)

// pingSocket is an unprivileged ICMP echo sender: a datagram socket of protocol ICMP.
// The kernel allows it for groups in net.ipv4.ping_group_range, no root or capability is
// needed, and it fills in the identifier itself.
type pingSocket struct{}

func newICMPPinger() (icmpPinger, error) {
	pc, err := openPingSocket()
	if err != nil {
		return nil, fmt.Errorf("open ICMP socket: %w", err)
	}
	_ = pc.Close() // only the check mattered
	return pingSocket{}, nil
}

func openPingSocket() (net.PacketConn, error) {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM|syscall.SOCK_CLOEXEC, syscall.IPPROTO_ICMP)
	if err != nil {
		return nil, err
	}
	if err := syscall.SetNonblock(fd, true); err != nil {
		_ = syscall.Close(fd)
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "icmp")
	defer func() { _ = f.Close() }() // FilePacketConn duplicates the descriptor
	return net.FilePacketConn(f)
}

func (pingSocket) Ping(ctx context.Context, ip netip.Addr, timeout time.Duration) (bool, error) {
	pc, err := openPingSocket()
	if err != nil {
		return false, err
	}
	defer func() { _ = pc.Close() }() // a UDP-like socket, nothing to flush

	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := pc.SetDeadline(deadline); err != nil {
		return false, err
	}
	if _, err := pc.WriteTo(echoRequest(1), &net.UDPAddr{IP: net.IP(ip.AsSlice())}); err != nil {
		return false, err
	}
	buf := make([]byte, 1500)
	for ctx.Err() == nil {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				return false, nil
			}
			return false, err
		}
		if u, ok := from.(*net.UDPAddr); ok && u.IP.Equal(net.IP(ip.AsSlice())) && n >= 1 && buf[0] == icmpEchoReply {
			return true, nil
		}
	}
	return false, ctx.Err()
}
