package store

import (
	"context"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/collector"
	"github.com/FlexEbat/Netscribe/internal/model"
)

// scanResult runs a complete scan of lan with a ready result.
func scanResult(t *testing.T, s *Store, r collector.Result) model.Scan {
	t.Helper()
	ctx := context.Background()
	sc, err := s.StartScan(ctx, lan)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyResult(ctx, sc.ID, r); err != nil {
		t.Fatalf("ApplyResult: %v", err)
	}
	if err := s.FinishScan(ctx, sc.ID, "done", ""); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetScan(ctx, sc.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

var (
	keyA = model.DeviceKey{MAC: "aa:aa:aa:aa:aa:01", IP: "192.168.1.1"}
	keyB = model.DeviceKey{MAC: "aa:aa:aa:aa:aa:02", IP: "192.168.1.2"}
	keyC = model.DeviceKey{MAC: "aa:aa:aa:aa:aa:03", IP: "192.168.1.3"}
)

func twoDevices() []model.DeviceInput {
	return []model.DeviceInput{dev(keyA.MAC, keyA.IP), dev(keyB.MAC, keyB.IP)}
}

func links(t *testing.T, s *Store) []model.Link {
	t.Helper()
	out, err := s.ListLinks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestApplyInterfacesAreReplacedNotDuplicated(t *testing.T) {
	s := newTestStore(t)
	ifaces := []model.InterfaceInput{
		{Device: keyA, Name: "eth0", MAC: "02:11:22:33:44:01", IP: "192.168.1.1", Up: true},
		{Device: keyA, Name: "eth0", MAC: "02:11:22:33:44:01", IP: "192.168.1.1", Up: true}, // the same row twice
		{Device: keyA, Name: "eth1", MAC: "02:11:22:33:44:02"},
		{Device: keyA, Name: "  ", IP: "10.0.0.1"}, // no name, no row
	}
	scanResult(t, s, collector.Result{Devices: twoDevices(), Interfaces: ifaces})
	scanResult(t, s, collector.Result{Devices: twoDevices(), Interfaces: ifaces})

	a := byMAC(t, s, keyA.MAC)
	got, err := s.ListInterfaces(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("interfaces = %+v, want 2 after two scans", got)
	}
	if got[0].Name != "eth0" || got[0].IP != "192.168.1.1" || !got[0].Up || got[1].Name != "eth1" || got[1].Up {
		t.Errorf("interfaces = %+v", got)
	}

	// A scan in which the device reported nothing leaves its interfaces alone.
	scanResult(t, s, collector.Result{Devices: twoDevices()})
	if got, _ := s.ListInterfaces(context.Background(), a.ID); len(got) != 2 {
		t.Errorf("silent scan changed the interfaces: %+v", got)
	}

	// A scan that reports fewer interfaces replaces the old set.
	scanResult(t, s, collector.Result{Devices: twoDevices(), Interfaces: ifaces[:1]})
	if got, _ := s.ListInterfaces(context.Background(), a.ID); len(got) != 1 {
		t.Errorf("interfaces = %+v, want the one that is left", got)
	}
}

func TestListInterfacesOfUnknownDeviceIsEmpty(t *testing.T) {
	s := newTestStore(t)
	got, err := s.ListInterfaces(context.Background(), 999)
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("got %#v, err %v, want an empty non-nil list", got, err)
	}
}

func TestApplyLinkReportedFromBothEndsIsOneRow(t *testing.T) {
	s := newTestStore(t)
	sc := scanResult(t, s, collector.Result{
		Devices: twoDevices(),
		Links: []model.LinkInput{
			{A: keyA, B: keyB, APort: "Gi1", BPort: "Gi24", Source: "lldp"},
			{A: keyB, B: keyA, APort: "Gi24", BPort: "Gi1", Source: "lldp"},
		},
	})
	got := links(t, s)
	if len(got) != 1 {
		t.Fatalf("links = %+v, want one", got)
	}
	a, b := byMAC(t, s, keyA.MAC), byMAC(t, s, keyB.MAC)
	l := got[0]
	if l.ADeviceID != a.ID || l.BDeviceID != b.ID || l.ADeviceID >= l.BDeviceID || l.APort != "Gi1" || l.BPort != "Gi24" || l.Source != "lldp" {
		t.Errorf("link = %+v", l)
	}
	if sc.LinkCount != 1 {
		t.Errorf("scan link count = %d, want 1", sc.LinkCount)
	}

	// The same cable seen again is the same row.
	scanResult(t, s, collector.Result{
		Devices: twoDevices(),
		Links:   []model.LinkInput{{A: keyB, B: keyA, APort: "Gi24", BPort: "Gi1", Source: "lldp"}},
	})
	if got := links(t, s); len(got) != 1 || got[0].ID != l.ID || !got[0].LastSeenAt.After(l.LastSeenAt) {
		t.Errorf("links = %+v, want the first row seen again", got)
	}
}

func TestApplyLinkToNewNeighborCreatesTheDevice(t *testing.T) {
	s := newTestStore(t)
	neighbor := model.DeviceInput{
		Key: keyC, Hostname: "ap-lobby", Kind: model.KindAP, Source: "snmp",
	}
	scanResult(t, s, collector.Result{
		Devices: append(twoDevices(), neighbor),
		Links:   []model.LinkInput{{A: keyA, B: keyC, APort: "Gi2", BPort: "ap0", Source: "lldp"}},
	})
	c := byMAC(t, s, keyC.MAC)
	if c.Source != "snmp" || c.Kind != model.KindAP || c.Hostname != "ap-lobby" {
		t.Errorf("neighbor = %+v", c)
	}
	if got := links(t, s); len(got) != 1 || got[0].BDeviceID != c.ID {
		t.Errorf("links = %+v", got)
	}
}

func TestApplyLinkWithAnUnknownEndIsSkipped(t *testing.T) {
	s := newTestStore(t)
	scanResult(t, s, collector.Result{
		Devices: twoDevices(),
		Links: []model.LinkInput{
			{A: keyA, B: keyC, Source: "lldp"}, // keyC was never stored
			{A: keyA, B: keyA, Source: "lldp"}, // a device is not linked to itself
		},
	})
	if got := links(t, s); len(got) != 0 {
		t.Errorf("links = %+v, want none", got)
	}
}

func TestApplyRemovesLinkThatVanishedFromLLDP(t *testing.T) {
	s := newTestStore(t)
	devs := append(twoDevices(), dev(keyC.MAC, keyC.IP))
	lldp := []model.LinkInput{
		{A: keyA, B: keyB, APort: "Gi1", BPort: "Gi24", Source: "lldp"},
		{A: keyA, B: keyC, APort: "Gi2", BPort: "eth0", Source: "lldp"},
	}
	scanResult(t, s, collector.Result{Devices: devs, Links: lldp, LinksPolled: []model.DeviceKey{keyA}})
	if got := links(t, s); len(got) != 2 {
		t.Fatalf("links = %+v", got)
	}

	// B and C did not answer, A did and no longer sees B. Only A-B goes... and A-C stays,
	// because A still reports it.
	scanResult(t, s, collector.Result{Devices: devs, Links: lldp[1:], LinksPolled: []model.DeviceKey{keyA}})
	got := links(t, s)
	if len(got) != 1 || got[0].APort != "Gi2" {
		t.Fatalf("links = %+v, want only A-C", got)
	}

	// A scan in which nobody was polled removes nothing.
	scanResult(t, s, collector.Result{Devices: devs})
	if got := links(t, s); len(got) != 1 {
		t.Errorf("links = %+v, a scan without LLDP must keep them", got)
	}

	// A device that was polled and sees nothing loses its links.
	scanResult(t, s, collector.Result{Devices: devs, LinksPolled: []model.DeviceKey{keyC}})
	if got := links(t, s); len(got) != 0 {
		t.Errorf("links = %+v, want none", got)
	}
}

func TestApplyKeepsManualLinks(t *testing.T) {
	s := newTestStore(t)
	scanResult(t, s, collector.Result{Devices: twoDevices()})
	a, b := byMAC(t, s, keyA.MAC), byMAC(t, s, keyB.MAC)
	manual, err := s.CreateLink(context.Background(), model.ManualLink{ADeviceID: a.ID, BDeviceID: b.ID, APort: "Gi1", BPort: "Gi24"})
	if err != nil {
		t.Fatal(err)
	}

	// LLDP reports the same cable: the row stays manual. Then LLDP stops reporting it.
	scanResult(t, s, collector.Result{
		Devices: twoDevices(), LinksPolled: []model.DeviceKey{keyA, keyB},
		Links: []model.LinkInput{{A: keyA, B: keyB, APort: "Gi1", BPort: "Gi24", Source: "lldp"}},
	})
	scanResult(t, s, collector.Result{Devices: twoDevices(), LinksPolled: []model.DeviceKey{keyA, keyB}})
	got := links(t, s)
	if len(got) != 1 || got[0].ID != manual.ID || got[0].Source != "manual" {
		t.Errorf("links = %+v, want the manual link untouched", got)
	}
}

func TestApplySourceNeverGoesDown(t *testing.T) {
	s := newTestStore(t)
	scanResult(t, s, collector.Result{Devices: []model.DeviceInput{dev(keyA.MAC, keyA.IP)}})
	if d := byMAC(t, s, keyA.MAC); d.Source != "arp" {
		t.Fatalf("source = %q", d.Source)
	}

	upgraded := model.DeviceInput{Key: keyA, Hostname: "sw1", Description: "Acme", Kind: model.KindUnknown, Source: "snmp"}
	scanResult(t, s, collector.Result{Devices: []model.DeviceInput{dev(keyA.MAC, keyA.IP), upgraded}})
	d := byMAC(t, s, keyA.MAC)
	if d.Source != "snmp" || d.Hostname != "sw1" || d.Description != "Acme" {
		t.Errorf("device = %+v, want the SNMP facts and source", d)
	}

	// The next scan finds it by ARP only: the facts and the source stay.
	scanResult(t, s, collector.Result{Devices: []model.DeviceInput{dev(keyA.MAC, keyA.IP)}})
	if d := byMAC(t, s, keyA.MAC); d.Source != "snmp" || d.Hostname != "sw1" {
		t.Errorf("device = %+v, ARP must not take the facts back", d)
	}
}

func TestSourceRank(t *testing.T) {
	cases := []struct{ cur, in, want string }{
		{"arp", "snmp", "snmp"}, {"snmp", "arp", "snmp"}, {"snmp", "ssh", "ssh"},
		{"ssh", "snmp", "ssh"}, {"manual", "ssh", "manual"}, {"arp", "arp", "arp"}, {"local", "arp", "arp"},
	}
	for _, c := range cases {
		if got := betterSource(c.cur, c.in); got != c.want {
			t.Errorf("betterSource(%q, %q) = %q, want %q", c.cur, c.in, got, c.want)
		}
	}
}

func TestApplyFDBLinksVanishOnlyWhenTheSwitchWasPolled(t *testing.T) {
	s := newTestStore(t)
	devs := append(twoDevices(), dev(keyC.MAC, keyC.IP))
	fdb := model.LinkInput{A: keyA, B: keyC, APort: "Gi5", Source: "fdb"}
	lldp := model.LinkInput{A: keyA, B: keyB, APort: "Gi1", BPort: "Gi24", Source: "lldp"}
	scanResult(t, s, collector.Result{Devices: devs, Links: []model.LinkInput{fdb, lldp}})
	if got := links(t, s); len(got) != 2 {
		t.Fatalf("links = %+v", got)
	}

	// Polled over LLDP only: the fdb link says nothing about what LLDP saw.
	scanResult(t, s, collector.Result{Devices: devs, LinksPolled: []model.DeviceKey{keyA, keyB}})
	got := links(t, s)
	if len(got) != 1 || got[0].Source != "fdb" {
		t.Fatalf("links = %+v, want the fdb link to stay and the lldp one to go", got)
	}

	// Polled over FDB and the host is gone from the table: the link goes.
	scanResult(t, s, collector.Result{Devices: devs, FDBPolled: []model.DeviceKey{keyA}})
	if got := links(t, s); len(got) != 0 {
		t.Errorf("links = %+v, want none", got)
	}
}

func TestApplyProbeSourceRanksWithARP(t *testing.T) {
	s := newTestStore(t)
	probed := model.DeviceInput{Key: model.DeviceKey{IP: "192.168.1.70"}, Source: "probe"}
	scanResult(t, s, collector.Result{Devices: []model.DeviceInput{probed}})
	scanResult(t, s, collector.Result{Devices: []model.DeviceInput{
		{Key: model.DeviceKey{IP: "192.168.1.70"}, Hostname: "found", Source: "snmp"},
	}})
	got, err := s.ListDevices(context.Background(), DeviceFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Source != "snmp" || got[0].Hostname != "found" {
		t.Errorf("devices = %+v, want one device upgraded to snmp", got)
	}
	if betterSource("snmp", "probe") != "snmp" || betterSource("probe", "arp") != "probe" {
		t.Error("probe and arp rank the same, a tie keeps the current source")
	}
}

func svc(key model.DeviceKey, port int, name string) model.ServiceInput {
	return model.ServiceInput{Device: key, Port: port, Proto: "tcp", Name: name}
}

func services(t *testing.T, s *Store, deviceID int64) []model.Service {
	t.Helper()
	out, err := s.ListServices(context.Background(), deviceID)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestApplyServicesAreStoredOnceAndRefreshed(t *testing.T) {
	s := newTestStore(t)
	in := collector.Result{
		Devices:        twoDevices(),
		Services:       []model.ServiceInput{svc(keyA, 22, "ssh"), svc(keyA, 22, "ssh"), svc(keyB, 80, "http")},
		ServicesPolled: []model.DeviceKey{keyA, keyB},
	}
	scanResult(t, s, in)
	first := services(t, s, 0)
	if len(first) != 2 || first[0].Port != 22 || first[0].Name != "ssh" || first[0].Proto != "tcp" {
		t.Fatalf("services = %+v", first)
	}

	in.Services = []model.ServiceInput{svc(keyA, 22, "secure-shell"), svc(keyB, 80, "http")}
	scanResult(t, s, in)
	second := services(t, s, 0)
	if len(second) != 2 || second[0].ID != first[0].ID || second[0].Name != "secure-shell" {
		t.Fatalf("services = %+v, want the same rows with the new name", second)
	}
	if !second[0].FirstSeenAt.Equal(first[0].FirstSeenAt) || !second[0].LastSeenAt.After(first[0].LastSeenAt) {
		t.Errorf("first seen %v -> %v, last seen %v -> %v", first[0].FirstSeenAt, second[0].FirstSeenAt, first[0].LastSeenAt, second[0].LastSeenAt)
	}
}

func TestApplyServiceThatClosedIsRemovedAfterTheNextScan(t *testing.T) {
	s := newTestStore(t)
	scanResult(t, s, collector.Result{
		Devices:        twoDevices(),
		Services:       []model.ServiceInput{svc(keyA, 22, "ssh"), svc(keyA, 80, "http"), svc(keyB, 443, "https")},
		ServicesPolled: []model.DeviceKey{keyA, keyB},
	})

	// Port 80 of A closed. B was not scanned this time, so its service stays.
	scanResult(t, s, collector.Result{
		Devices:        twoDevices(),
		Services:       []model.ServiceInput{svc(keyA, 22, "ssh")},
		ServicesPolled: []model.DeviceKey{keyA},
	})
	a, b := byMAC(t, s, keyA.MAC), byMAC(t, s, keyB.MAC)
	if got := services(t, s, a.ID); len(got) != 1 || got[0].Port != 22 {
		t.Errorf("A services = %+v, want only ssh", got)
	}
	if got := services(t, s, b.ID); len(got) != 1 || got[0].Port != 443 {
		t.Errorf("B services = %+v, an unscanned device keeps its services", got)
	}

	// A scan with the stage off (nobody polled) removes nothing.
	scanResult(t, s, collector.Result{Devices: twoDevices()})
	if got := services(t, s, a.ID); len(got) != 1 {
		t.Errorf("A services = %+v", got)
	}

	// A scanned device with nothing open loses its last service.
	scanResult(t, s, collector.Result{Devices: twoDevices(), ServicesPolled: []model.DeviceKey{keyA}})
	if got := services(t, s, a.ID); len(got) != 0 {
		t.Errorf("A services = %+v, want none", got)
	}
}

func TestApplyServicesSkipsBadInput(t *testing.T) {
	s := newTestStore(t)
	scanResult(t, s, collector.Result{
		Devices: twoDevices(),
		Services: []model.ServiceInput{
			svc(keyA, 0, "zero"), svc(keyA, 70000, "big"), svc(keyC, 22, "ghost"), // C was never stored
			{Device: keyA, Port: 53, Proto: "icmp"},
			{Device: keyA, Port: 53, Proto: " UDP ", Name: "domain\n"},
		},
	})
	got := services(t, s, 0)
	if len(got) != 1 || got[0].Port != 53 || got[0].Proto != "udp" || got[0].Name != "domain" {
		t.Errorf("services = %+v, want the one valid udp service with a clean name", got)
	}
}

func TestServicesGoWithTheirDevice(t *testing.T) {
	s := newTestStore(t)
	scanResult(t, s, collector.Result{Devices: twoDevices(), Services: []model.ServiceInput{svc(keyA, 22, "ssh"), svc(keyB, 80, "http")}})
	if err := s.DeleteDevice(context.Background(), byMAC(t, s, keyA.MAC).ID); err != nil {
		t.Fatal(err)
	}
	if got := services(t, s, 0); len(got) != 1 || got[0].Port != 80 {
		t.Errorf("services = %+v, want only the other device's", got)
	}
}
