package store

import (
	"context"
	"fmt"

	"github.com/FlexEbat/Netscribe/internal/model"
)

// ListServices returns the services of one device, or of all devices when deviceID is 0.
// The result is never nil.
func (s *Store) ListServices(ctx context.Context, deviceID int64) ([]model.Service, error) {
	rows, err := s.q.ListServices(ctx)
	if deviceID != 0 {
		rows, err = s.q.ListServicesByDevice(ctx, deviceID)
	}
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	out := make([]model.Service, 0, len(rows))
	for _, r := range rows {
		first, err := parseTime(r.FirstSeenAt)
		if err != nil {
			return nil, fmt.Errorf("service %d: first_seen_at: %w", r.ID, err)
		}
		last, err := parseTime(r.LastSeenAt)
		if err != nil {
			return nil, fmt.Errorf("service %d: last_seen_at: %w", r.ID, err)
		}
		out = append(out, model.Service{
			ID: r.ID, DeviceID: r.DeviceID, Port: int(r.Port), Proto: r.Proto, Name: r.Name,
			FirstSeenAt: first, LastSeenAt: last,
		})
	}
	return out, nil
}
