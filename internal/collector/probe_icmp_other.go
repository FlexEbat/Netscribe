//go:build !linux

package collector

import "errors"

func newICMPPinger() (icmpPinger, error) {
	return nil, errors.New("unprivileged ICMP is only supported on Linux")
}
