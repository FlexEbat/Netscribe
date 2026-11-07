package main

import (
	"context"
	"reflect"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/store"
)

func TestLargeNetworkIsTheSameForTheSameArguments(t *testing.T) {
	spec := largeSpec{Devices: 500, Links: 800, Services: 3000, Scans: 1}
	a, b := largeNetwork(spec), largeNetwork(spec)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("two runs with the same arguments made different networks")
	}
	if len(a.Devices) != 500 || len(a.Links) != 800 || len(a.Services) != 3000 {
		t.Errorf("sizes = %d devices, %d links, %d services, want 500, 800, 3000", len(a.Devices), len(a.Links), len(a.Services))
	}
}

func TestLargeNetworkHasNoRepeatedKeysLinksOrServices(t *testing.T) {
	res := largeNetwork(largeSpec{Devices: 2000, Links: 3200, Services: 12000})
	macs, ips := map[string]bool{}, map[string]bool{}
	for _, d := range res.Devices {
		if macs[d.Key.MAC] || ips[d.Key.IP] {
			t.Fatalf("device %s %s is repeated", d.Key.MAC, d.Key.IP)
		}
		macs[d.Key.MAC], ips[d.Key.IP] = true, true
	}
	ends := map[[2]string]bool{}
	ports := map[[2]string]bool{}
	for _, l := range res.Links {
		pair := [2]string{l.A.MAC, l.B.MAC}
		if l.A.MAC == l.B.MAC || ends[pair] || ends[[2]string{l.B.MAC, l.A.MAC}] {
			t.Fatalf("link %v is a loop or a repeat", pair)
		}
		ends[pair] = true
		for _, p := range [][2]string{{l.A.MAC, l.APort}, {l.B.MAC, l.BPort}} {
			if ports[p] {
				t.Fatalf("port %v is used twice", p)
			}
			ports[p] = true
		}
	}
	svc := map[[2]any]bool{}
	for _, s := range res.Services {
		k := [2]any{s.Device.MAC, s.Port}
		if svc[k] {
			t.Fatalf("service %v is repeated", k)
		}
		svc[k] = true
	}
}

func TestSeedLargeStoresTheNetworkAndItsHistory(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	spec := largeSpec{Devices: 120, Links: 190, Services: 700, Scans: 4}

	if n, err := seedLarge(ctx, st, spec); err != nil || n != 120 {
		t.Fatalf("seedLarge() = %d, %v, want 120 devices", n, err)
	}
	if devs, err := st.ListDevices(ctx, store.DeviceFilter{}); err != nil || len(devs) != 120 {
		t.Errorf("devices = %d, %v, want 120", len(devs), err)
	}
	if links, err := st.ListLinks(ctx); err != nil || len(links) != 190 {
		t.Errorf("links = %d, %v, want 190", len(links), err)
	}
	if scans, err := st.ListScans(ctx, 100); err != nil || len(scans) != 4 {
		t.Errorf("scans = %d, %v, want 4", len(scans), err)
	}
}
