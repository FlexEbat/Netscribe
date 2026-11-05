//go:build !linux

package collector

import (
	"context"
	"errors"
)

func openPacketSource(context.Context, func(int) bool, func([]byte)) error {
	return errors.New("DHCP listening needs Linux packet sockets")
}
