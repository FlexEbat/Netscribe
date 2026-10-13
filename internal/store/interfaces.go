package store

import (
	"context"
	"fmt"

	"github.com/FlexEbat/Netscribe/internal/model"
)

// ListInterfaces returns the interfaces of one device, never nil.
func (s *Store) ListInterfaces(ctx context.Context, deviceID int64) ([]model.Interface, error) {
	rows, err := s.q.ListInterfacesByDevice(ctx, deviceID)
	if err != nil {
		return nil, fmt.Errorf("list interfaces: %w", err)
	}
	out := make([]model.Interface, 0, len(rows))
	for _, r := range rows {
		out = append(out, model.Interface{
			ID: r.ID, DeviceID: r.DeviceID, Name: r.Name, MAC: r.Mac, IP: r.Ip, Up: r.Up != 0,
		})
	}
	return out, nil
}
