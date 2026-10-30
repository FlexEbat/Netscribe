package main

import (
	"context"

	"github.com/FlexEbat/Netscribe/internal/events"
	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/store"
)

// notifySource gives the notifier the three reads it needs from the database.
type notifySource struct{ *store.Store }

func (n notifySource) Devices(ctx context.Context) ([]model.Device, error) {
	return n.ListDevices(ctx, store.DeviceFilter{})
}

// scanHandler is the part of the notifier that reacts to a finished scan.
type scanHandler interface {
	Handle(ctx context.Context, scan model.Scan)
}

// notifyOnScans hands every finished scan to h, until ctx ends. The scanner publishes
// "scan.finished" with the scan as data, so the scanner itself knows nothing about
// notifications.
func notifyOnScans(ctx context.Context, hub *events.Hub, h scanHandler) {
	ch, cancel := hub.Subscribe()
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			if scan, isScan := e.Data.(model.Scan); isScan && e.Name == "scan.finished" {
				h.Handle(ctx, scan)
			}
		}
	}
}
