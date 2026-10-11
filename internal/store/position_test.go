package store

import (
	"context"
	"errors"
	"testing"
)

func TestSetDevicePositionSurvivesLaterScans(t *testing.T) {
	s := newTestStore(t)
	scanOnce(t, s, "192.168.1.0/24", dev("aa:bb:cc:dd:ee:01", "192.168.1.5"))
	id := list(t, s, DeviceFilter{})[0].ID

	if err := s.SetDevicePosition(context.Background(), id, 10.5, -20); err != nil {
		t.Fatal(err)
	}
	// A scan that sees the device again must not forget where it sits.
	scanOnce(t, s, "192.168.1.0/24", dev("aa:bb:cc:dd:ee:01", "192.168.1.5"))

	got, err := s.GetDevice(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.X == nil || got.Y == nil || *got.X != 10.5 || *got.Y != -20 {
		t.Errorf("position = %v, %v, want 10.5, -20", got.X, got.Y)
	}
}

func TestSetDevicePositionOfAMissingDevice(t *testing.T) {
	s := newTestStore(t)
	if err := s.SetDevicePosition(context.Background(), 404, 1, 2); !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestTopologyListsDevicesAndNeverReturnsNilLists(t *testing.T) {
	s := newTestStore(t)
	empty, err := s.Topology(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if empty.Devices == nil || empty.Links == nil || empty.Containers == nil {
		t.Errorf("nil list in %+v", empty)
	}
	scanOnce(t, s, "192.168.1.0/24", dev("aa:bb:cc:dd:ee:01", "192.168.1.5"))
	full, err := s.Topology(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(full.Devices) != 1 {
		t.Errorf("devices = %d, want 1", len(full.Devices))
	}
}
