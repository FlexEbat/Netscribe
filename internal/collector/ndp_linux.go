//go:build linux

package collector

import (
	"context"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"
)

// ndpSettle is how long the hosts get to answer the echo before the table is read.
const ndpSettle = 2 * time.Second

// readNeighbours dumps the IPv6 neighbour table over rtnetlink. It needs no privilege.
func readNeighbours(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return syscall.NetlinkRIB(syscall.RTM_GETNEIGH, syscall.AF_INET6)
}

// echoAllNodes sends one ICMPv6 echo request to ff02::1 on each named interface, then waits
// ndpSettle for the answers. The datagram socket of protocol ICMPv6 needs no privilege
// beyond net.ipv4.ping_group_range, which the kernel also applies to IPv6.
func echoAllNodes(ctx context.Context, ifaces []string) error {
	fd, err := syscall.Socket(syscall.AF_INET6, syscall.SOCK_DGRAM|syscall.SOCK_CLOEXEC, syscall.IPPROTO_ICMPV6)
	if err != nil {
		return fmt.Errorf("open ICMPv6 socket: %w", err)
	}
	f := os.NewFile(uintptr(fd), "icmp6")
	defer func() { _ = f.Close() }() // FilePacketConn duplicates the descriptor
	pc, err := net.FilePacketConn(f)
	if err != nil {
		return fmt.Errorf("open ICMPv6 socket: %w", err)
	}
	defer func() { _ = pc.Close() }() // a datagram socket, nothing to flush

	const echoRequest6 = 128
	packet := []byte{echoRequest6, 0, 0, 0, 0, 0, 0, 1} // the kernel fills in checksum and identifier
	var firstErr error
	sent := 0
	for _, name := range ifaces {
		dst := &net.UDPAddr{IP: net.ParseIP("ff02::1"), Zone: name}
		if _, err := pc.WriteTo(packet, dst); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("echo on %s: %w", name, err)
			}
			continue
		}
		sent++
	}
	if sent > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(ndpSettle):
		}
	}
	return firstErr
}
