//go:build linux

package collector

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"time"
)

const (
	ethPIP        = 0x0800 // ETH_P_IP
	dhcpReadChunk = 2048
)

// htons converts a 16 bit number to network byte order.
func htons(v uint16) uint16 { return v<<8 | v>>8 }

// openPacketSource opens a packet socket that receives the IPv4 packets of every interface,
// without their link headers, and hands each one to handle. The socket is never written to.
func openPacketSource(ctx context.Context, allow func(ifindex int) bool, handle func(pkt []byte)) error {
	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_DGRAM|syscall.SOCK_CLOEXEC, int(htons(ethPIP)))
	if err != nil {
		return fmt.Errorf("open packet socket: %w", err)
	}
	defer func() { _ = syscall.Close(fd) }() // the socket is only read
	// A receive timeout wakes the loop once a second so it can see that ctx ended.
	tv := syscall.NsecToTimeval(int64(time.Second))
	if err := syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv); err != nil {
		return fmt.Errorf("set receive timeout: %w", err)
	}
	buf := make([]byte, dhcpReadChunk)
	for ctx.Err() == nil {
		n, from, err := syscall.Recvfrom(fd, buf, 0)
		if err != nil {
			if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EINTR) {
				continue
			}
			return fmt.Errorf("read packet: %w", err)
		}
		if ll, ok := from.(*syscall.SockaddrLinklayer); !ok || !allow(ll.Ifindex) {
			continue
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		handle(pkt)
	}
	return nil
}
