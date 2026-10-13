package collector

import (
	"context"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/model"
)

// twoSwitches is sw1 (192.168.1.2) and sw2 (192.168.1.3) cabled together: sw1 Gi1 is
// connected to sw2 Gi1... as each side reports it. sw1 also sees an access point it has
// never met.
func twoSwitches(t *testing.T) (fakeNetwork, Input) {
	t.Helper()
	net := fakeNetwork{
		"192.168.1.2": loadAgent(t, "snmp_sw1.txt", "public"),
		"192.168.1.3": loadAgent(t, "snmp_sw2.txt", "public"),
	}
	return net, Input{Devices: liveDevices("192.168.1.2", "192.168.1.3"), Config: testConfig("public")}
}

func runLLDP(t *testing.T, net fakeNetwork, in Input) Result {
	t.Helper()
	snmp := newSNMP(net.dial, quietLogger())
	if _, err := snmp.Collect(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	res, err := NewLLDP(snmp, quietLogger()).Collect(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestLLDPBuildsLinksBetweenKnownDevices(t *testing.T) {
	net, in := twoSwitches(t)
	res := runLLDP(t, net, in)

	// The neighbor of sw1 on Gi1 is sw2, known by the MAC of its interface. sw2 reports
	// sw1 the same way: two reports of one cable.
	var toSw2, toSw1 *model.LinkInput
	for i, l := range res.Links {
		switch {
		case l.A.IP == "192.168.1.2" && l.B.IP == "192.168.1.3":
			toSw2 = &res.Links[i]
		case l.A.IP == "192.168.1.3" && l.B.IP == "192.168.1.2":
			toSw1 = &res.Links[i]
		}
	}
	if toSw2 == nil || toSw1 == nil {
		t.Fatalf("links = %+v", res.Links)
	}
	if toSw2.APort != "Gi1" || toSw2.BPort != "Gi24" || toSw2.Source != "lldp" {
		t.Errorf("sw1 -> sw2 = %+v", *toSw2)
	}
	if toSw1.APort != "Gi24" || toSw1.BPort != "Gi1" {
		t.Errorf("sw2 -> sw1 = %+v: the ports must mirror the other report", *toSw1)
	}
	if len(res.LinksPolled) != 2 {
		t.Errorf("polled = %+v, want both switches", res.LinksPolled)
	}
}

func TestLLDPCreatesUnknownNeighbor(t *testing.T) {
	net, in := twoSwitches(t)
	res := runLLDP(t, net, in)

	var ap *model.DeviceInput
	for i, d := range res.Devices {
		if d.Hostname == "ap-lobby" {
			ap = &res.Devices[i]
		}
	}
	if ap == nil {
		t.Fatalf("devices = %+v", res.Devices)
	}
	if ap.Key.MAC != "02:11:22:33:66:01" || ap.Key.IP != "10.0.0.50" || ap.Kind != model.KindAP || ap.Source != "snmp" {
		t.Errorf("neighbor = %+v", *ap)
	}
	found := false
	for _, l := range res.Links {
		if l.A.IP == "192.168.1.2" && l.B == ap.Key && l.APort == "Gi2" && l.BPort == "ap0" {
			found = true
		}
	}
	if !found {
		t.Errorf("no link from sw1 Gi2 to the access point: %+v", res.Links)
	}
}

func TestLLDPKnownNeighborGetsKindOnly(t *testing.T) {
	net, in := twoSwitches(t)
	res := runLLDP(t, net, in)
	for _, d := range res.Devices {
		if d.Key.IP == "192.168.1.3" && d.Hostname != "" {
			t.Errorf("a known neighbor must not get a name from LLDP: %+v", d)
		}
	}
	kinds := 0
	for _, d := range res.Devices {
		if d.Key.IP == "192.168.1.3" && d.Kind == model.KindSwitch {
			kinds++
		}
	}
	if kinds == 0 {
		t.Errorf("sw2 has the bridge capability, devices = %+v", res.Devices)
	}
}

func TestLLDPSkipsDevicesWithoutSNMP(t *testing.T) {
	net, in := twoSwitches(t)
	in.Devices = append(in.Devices, model.Device{IP: "192.168.1.77", MAC: "aa:aa:aa:aa:aa:77"})
	res := runLLDP(t, net, in)
	for _, p := range res.LinksPolled {
		if p.IP == "192.168.1.77" {
			t.Errorf("a device that never answered SNMP counted as polled")
		}
	}
}

func TestKindFromCaps(t *testing.T) {
	cases := []struct {
		caps []byte
		want model.DeviceKind
	}{
		{[]byte{0x28, 0x00}, model.KindSwitch}, // bridge and router: still a switch
		{[]byte{0x10, 0x00}, model.KindAP},
		{[]byte{0x38, 0x00}, model.KindAP}, // an access point that bridges and routes
		{[]byte{0x08, 0x00}, model.KindRouter},
		{[]byte{0x01, 0x00}, ""}, // station only says nothing
		{nil, ""},
	}
	for _, c := range cases {
		if got := kindFromCaps(c.caps); got != c.want {
			t.Errorf("kindFromCaps(% x) = %q, want %q", c.caps, got, c.want)
		}
	}
}

func TestPortName(t *testing.T) {
	cases := []struct {
		name    string
		subtype int
		id      []byte
		desc    string
		want    string
	}{
		{"interface name", 5, []byte("Gi1/0/1"), "GigabitEthernet1/0/1", "Gi1/0/1"},
		{"locally assigned", 7, []byte("port 3"), "", "port 3"},
		{"mac address", 3, []byte{0x02, 0x11, 0x22, 0x33, 0x44, 0x55}, "", "02:11:22:33:44:55"},
		{"unknown subtype uses the description", 2, []byte{1, 2}, "uplink", "uplink"},
		{"binary text falls back to the description", 5, []byte{0xff, 0xfe}, "eth0", "eth0"},
		{"nothing readable falls back to hex", 6, []byte{0xde, 0xad}, "", "dead"},
		{"control characters are not text", 5, []byte("a\x00b"), "", "610062"},
	}
	for _, c := range cases {
		if got := portName(c.subtype, c.id, []byte(c.desc)); got != c.want {
			t.Errorf("%s: portName = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDecodeChassis(t *testing.T) {
	if mac, ip := decodeChassis(4, []byte{2, 17, 34, 51, 68, 85}); mac != "02:11:22:33:44:55" || ip != "" {
		t.Errorf("mac subtype = %q, %q", mac, ip)
	}
	if mac, ip := decodeChassis(5, []byte{1, 10, 0, 0, 9}); mac != "" || ip != "10.0.0.9" {
		t.Errorf("address subtype = %q, %q", mac, ip)
	}
	if mac, ip := decodeChassis(5, []byte{2, 1, 2, 3, 4}); mac != "" || ip != "" {
		t.Errorf("IPv6 family must give nothing, got %q, %q", mac, ip)
	}
	if mac, ip := decodeChassis(7, []byte("name")); mac != "" || ip != "" {
		t.Errorf("a local name identifies nothing, got %q, %q", mac, ip)
	}
}

func TestParseNeighborsIgnoresMalformedRows(t *testing.T) {
	rem := []snmpPDU{
		{OID: oidLLDPRemTable + ".5.0.1", Value: []byte{1}},             // too short
		{OID: oidLLDPRemTable + ".x.0.1.1", Value: []byte{1}},           // not a number
		{OID: "1.2.3", Value: []byte{1}},                                // not in the table
		{OID: oidLLDPRemTable + ".9.0.1.1", Value: []byte("only-name")}, // a row without a chassis
	}
	got := parseNeighbors(nil, rem, []snmpPDU{{OID: oidLLDPRemMan + ".3.0.1.1.1.4.999.1.1.1", Value: int64(2)}})
	if len(got) != 1 || got[0].sysName != "only-name" || got[0].mgmtIP != "" || got[0].chassisMAC != "" {
		t.Errorf("got %+v", got)
	}
}

func TestLLDPNeighborWithoutIdentityIsSkipped(t *testing.T) {
	l := NewLLDP(newSNMP(fakeNetwork{}.dial, quietLogger()), quietLogger())
	if key, isNew := l.resolve(neighbor{sysName: "ghost"}, newKnown(nil)); key != (model.DeviceKey{}) || isNew {
		t.Errorf("key = %+v, isNew = %v", key, isNew)
	}
}
