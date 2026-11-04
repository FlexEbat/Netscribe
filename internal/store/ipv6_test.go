package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/collector"
	"github.com/FlexEbat/Netscribe/internal/model"
)

func dev6(mac, ip, ip6 string) model.DeviceInput {
	d := dev(mac, ip)
	d.IP6 = ip6
	return d
}

func TestScanFillsTheIPv6ColumnOfAKnownDevice(t *testing.T) {
	s := newTestStore(t)
	scanOnce(t, s, lan, dev("aa:aa:aa:aa:aa:01", "192.168.1.1"))
	scanOnce(t, s, lan, dev6("aa:aa:aa:aa:aa:01", "192.168.1.1", "FD00:0:0:0:0:0:0:1"))

	all := list(t, s, DeviceFilter{})
	if len(all) != 1 {
		t.Fatalf("devices = %d, want 1: a MAC is one device", len(all))
	}
	if all[0].IP != "192.168.1.1" || all[0].IP6 != "fd00::1" {
		t.Errorf("addresses = %q and %q, want 192.168.1.1 and the canonical fd00::1", all[0].IP, all[0].IP6)
	}
}

func TestADeviceWithOnlyAnIPv6AddressIsKeptByItsMAC(t *testing.T) {
	s := newTestStore(t)
	scanOnce(t, s, "fd00::/112", dev6("aa:aa:aa:aa:aa:09", "", "fd00::9"))
	scanOnce(t, s, "fd00::/112", dev6("aa:aa:aa:aa:aa:09", "", "fd00::a"))

	all := list(t, s, DeviceFilter{})
	if len(all) != 1 || all[0].IP != "" || all[0].IP6 != "fd00::a" {
		t.Errorf("devices = %+v, want one device with no IPv4 and the new IPv6 address", all)
	}
	if got := list(t, s, DeviceFilter{Query: "fd00::a"}); len(got) != 1 {
		t.Errorf("a search for the IPv6 address found %d devices, want 1", len(got))
	}
}

func TestAnAddressThatMovesToAnotherDeviceLeavesTheFirst(t *testing.T) {
	s := newTestStore(t)
	scanOnce(t, s, lan, dev6("aa:aa:aa:aa:aa:01", "192.168.1.1", "fd00::5"))
	scanOnce(t, s, lan, dev6("aa:aa:aa:aa:aa:02", "192.168.1.2", "fd00::5"))

	if got := byMAC(t, s, "aa:aa:aa:aa:aa:01").IP6; got != "" {
		t.Errorf("the first device still has %q, want its IPv6 address released", got)
	}
	if got := byMAC(t, s, "aa:aa:aa:aa:aa:02").IP6; got != "fd00::5" {
		t.Errorf("the second device has %q, want fd00::5", got)
	}
}

func TestAnInvalidIPv6AddressIsIgnored(t *testing.T) {
	s := newTestStore(t)
	scanOnce(t, s, lan, dev6("aa:aa:aa:aa:aa:01", "192.168.1.1", "192.168.1.1"), dev6("aa:aa:aa:aa:aa:02", "192.168.1.2", "not an address"),
		dev6("aa:aa:aa:aa:aa:03", "192.168.1.3", "::ffff:192.168.1.3"), dev6("aa:aa:aa:aa:aa:04", "192.168.1.4", "fe80::1%eth0"))

	for mac, want := range map[string]string{
		"aa:aa:aa:aa:aa:01": "", "aa:aa:aa:aa:aa:02": "", "aa:aa:aa:aa:aa:03": "", "aa:aa:aa:aa:aa:04": "fe80::1",
	} {
		if got := byMAC(t, s, mac).IP6; got != want {
			t.Errorf("%s ip6 = %q, want %q", mac, got, want)
		}
	}
}

func TestADeviceWithOnlyIPv6GoesOfflineInsideAnIPv6Target(t *testing.T) {
	s := newTestStore(t)
	scanOnce(t, s, lan+",fd00::/112", dev("aa:aa:aa:aa:aa:01", "192.168.1.1"), dev6("aa:aa:aa:aa:aa:09", "", "fd00::9"), dev6("aa:aa:aa:aa:aa:0a", "", "fd01::9"))
	scanOnce(t, s, lan+",fd00::/112", dev("aa:aa:aa:aa:aa:01", "192.168.1.1"))

	if byMAC(t, s, "aa:aa:aa:aa:aa:09").Online {
		t.Error("an unseen device inside the IPv6 target stayed online")
	}
	if !byMAC(t, s, "aa:aa:aa:aa:aa:0a").Online {
		t.Error("an unseen device outside every target was marked offline")
	}
}

func TestServicesRememberTheAddressTheyAnsweredOn(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	key := model.DeviceKey{MAC: "aa:aa:aa:aa:aa:01", IP: "192.168.1.1"}
	scanOnce(t, s, lan, dev6(key.MAC, key.IP, "fd00::1"))

	sc, err := s.StartScan(ctx, lan)
	if err != nil {
		t.Fatal(err)
	}
	res := collector.Result{
		Services: []model.ServiceInput{
			{Device: key, Port: 80, Proto: "tcp", Name: "http"},
			{Device: key, IP: "FD00:0:0:0:0:0:0:1", Port: 22, Proto: "tcp", Name: "ssh"},
			{Device: key, IP: "fd00::1", Port: 80, Proto: "tcp", Name: "http"}, // a different answer than IPv4: its own row
		},
		ServicesPolled: []model.DeviceKey{key},
	}
	if err := s.ApplyResult(ctx, sc.ID, res); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishScan(ctx, sc.ID, "done", ""); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListServices(ctx, byMAC(t, s, key.MAC).ID)
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	for _, sv := range got {
		seen = append(seen, sv.IP+"|"+sv.Name)
	}
	want := []string{"fd00::1|ssh", "|http", "fd00::1|http"}
	if len(seen) != len(want) {
		t.Fatalf("services = %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Errorf("services = %v, want %v", seen, want)
			break
		}
	}
}

func TestAManualDeviceMayHaveAnIPv6Address(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	d, err := s.CreateDevice(ctx, model.ManualDevice{Label: "nas", IP6: "FD00::0:7"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if d.IP6 != "fd00::7" || d.IP != "" {
		t.Errorf("device = %+v, want only the canonical IPv6 address", d)
	}
	if _, err := s.CreateDevice(ctx, model.ManualDevice{Label: "other", IP6: "fd00::7"}, 0); !errors.Is(err, ErrConflict) {
		t.Errorf("a second device with the same IPv6 address: err = %v, want ErrConflict", err)
	}
}

func TestIPv6ChangesAppearInTheChangeFeed(t *testing.T) {
	s := newTestStore(t)
	scanOnce(t, s, lan, dev6("aa:aa:aa:aa:aa:01", "192.168.1.1", "fd00::1"))
	scanOnce(t, s, lan, dev6("aa:aa:aa:aa:aa:01", "192.168.1.1", "fd00::2"))

	changes, err := s.ListChanges(context.Background(), 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range changes {
		if c.Kind == ChangeDeviceChanged && strings.Contains(c.Summary, "ip6 fd00::1 -> fd00::2") {
			found = true
		}
	}
	if !found {
		t.Errorf("changes = %+v, want a device_changed entry for the IPv6 address", changes)
	}
}
