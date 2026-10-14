package collector

import (
	"context"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
)

// tableNetwork is sw2 (a bridge) and sw3 (a bridge and router). sw3 has an LLDP neighbor
// on Gi1, so Gi1 is a trunk.
func runTables(t *testing.T, mutate func(*config.Config)) (Result, *SNMPARP) {
	t.Helper()
	net := fakeNetwork{
		"192.168.1.3": loadAgent(t, "snmp_sw2.txt", "public"),
		"192.168.1.4": loadAgent(t, "snmp_sw3.txt", "public"),
	}
	cfg := testConfig("public")
	cfg.Discovery.RouterTables = true
	if mutate != nil {
		mutate(&cfg)
	}
	in := Input{Devices: liveDevices("192.168.1.3", "192.168.1.4"), Config: cfg}
	snmp := newSNMP(net.dial, quietLogger())
	if _, err := snmp.Collect(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	lldp := NewLLDP(snmp, quietLogger())
	if _, err := lldp.Collect(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	c := NewSNMPARP(snmp, lldp, quietLogger())
	res, err := c.Collect(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return res, c
}

func deviceByMAC(res Result, mac string) *model.DeviceInput {
	for i, d := range res.Devices {
		if d.Key.MAC == mac {
			return &res.Devices[i]
		}
	}
	return nil
}

func TestSNMPARPFindsDevicesOfOtherSegments(t *testing.T) {
	res, _ := runTables(t, nil)
	d := deviceByMAC(res, "02:aa:00:00:00:20")
	if d == nil || d.Key.IP != "10.1.0.20" || d.Source != "snmp" {
		t.Errorf("device from the router's ARP table = %+v", d)
	}
	for _, mac := range []string{"02:aa:00:00:00:21", "02:aa:00:00:00:22", "01:00:5e:00:00:05"} {
		if deviceByMAC(res, mac) != nil {
			t.Errorf("%s is invalid, unspecified or multicast and must not become a device", mac)
		}
	}
}

func TestSNMPARPFindsHostsOnlyTheForwardingTableKnows(t *testing.T) {
	res, _ := runTables(t, nil)
	host := deviceByMAC(res, "02:aa:00:00:00:30")
	if host == nil || host.Key.IP != "192.168.1.60" || host.Source != "snmp" {
		t.Fatalf("host from the FDB = %+v", host)
	}
	if q := deviceByMAC(res, "02:aa:00:00:00:40"); q == nil || q.Key.IP != "" {
		t.Errorf("a host from the 802.1Q table has no IP to give, got %+v", q)
	}
}

func TestSNMPARPLinksHostsToSwitchPorts(t *testing.T) {
	res, _ := runTables(t, nil)
	want := map[string]string{"02:aa:00:00:00:30": "Gi2", "02:aa:00:00:00:40": "Gi3"}
	got := map[string]string{}
	for _, l := range res.Links {
		if l.Source != "fdb" || l.A.IP != "192.168.1.4" || l.BPort != "" {
			t.Errorf("unexpected link %+v", l)
		}
		got[l.B.MAC] = l.APort
	}
	for mac, port := range want {
		if got[mac] != port {
			t.Errorf("host %s on port %q, want %q (links %+v)", mac, got[mac], port, res.Links)
		}
	}
	if len(got) != len(want) {
		t.Errorf("links = %+v, want exactly the hosts on access ports", res.Links)
	}
}

func TestSNMPARPSkipsTrunkPortsOwnMACsAndSelfRows(t *testing.T) {
	res, _ := runTables(t, nil)
	for _, mac := range []string{
		"02:aa:00:00:00:31", // behind the trunk port Gi1
		"02:aa:00:00:00:32", // status "self"
		"02:11:22:33:77:02", // an interface of sw3 itself
		"02:11:22:33:55:01", // an interface of sw2
	} {
		if d := deviceByMAC(res, mac); d != nil {
			t.Errorf("%s must not become a device: %+v", mac, d)
		}
		for _, l := range res.Links {
			if l.B.MAC == mac {
				t.Errorf("%s must not get a link: %+v", mac, l)
			}
		}
	}
}

func TestSNMPARPReportsWhichSwitchesWerePolled(t *testing.T) {
	res, _ := runTables(t, nil)
	seen := map[string]bool{}
	for _, k := range res.FDBPolled {
		seen[k.IP] = true
	}
	if !seen["192.168.1.4"] || !seen["192.168.1.3"] {
		t.Errorf("FDBPolled = %+v, want both switches: an empty table is still an answer", res.FDBPolled)
	}
}

func TestSNMPARPCanBeSwitchedOff(t *testing.T) {
	res, _ := runTables(t, func(c *config.Config) { c.Discovery.RouterTables = false })
	if len(res.Devices) != 0 || len(res.Links) != 0 || len(res.FDBPolled) != 0 {
		t.Errorf("res = %+v, want nothing with discovery.routerTables false", res)
	}
}

func TestParseARPAndFDBIgnoreMalformedRows(t *testing.T) {
	if got := parseARP([]snmpPDU{
		{OID: oidIPNetToMediaPhys + ".nodot", Value: []byte{2, 0, 0, 0, 0, 1}},
		{OID: oidIPNetToMediaPhys + ".1.300.1.1.1", Value: []byte{2, 0, 0, 0, 0, 1}},
		{OID: oidIPNetToMediaPhys + ".1.10.0.0.1", Value: []byte{1, 2}},
	}, nil); len(got) != 0 {
		t.Errorf("parseARP = %v", got)
	}
	if got := parseFDB([]snmpPDU{
		{OID: oidFdbPort + ".1.2.3", Value: int64(1)},
		{OID: oidFdbPort + ".2.0.0.0.0.300", Value: int64(1)},
		{OID: oidFdbPort + ".2.0.0.0.0.1", Value: int64(-1)},
	}, nil, oidFdbPort, oidFdbStatus, 0); len(got) != 0 {
		t.Errorf("parseFDB = %v", got)
	}
}
