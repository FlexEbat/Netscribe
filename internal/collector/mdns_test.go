package collector

import (
	"context"
	"encoding/binary"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
)

// fakeMulticast replaces the network: it records every send and hands back canned answers.
type fakeMulticast struct {
	mu       sync.Mutex
	groups   []netip.AddrPort
	payloads [][]byte
	reply    []datagram
}

func (f *fakeMulticast) Exchange(_ context.Context, group netip.AddrPort, payload []byte, _ time.Duration) []datagram {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.groups = append(f.groups, group)
	f.payloads = append(f.payloads, payload)
	return f.reply
}

func (f *fakeMulticast) sends() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.groups)
}

// dnsName encodes a name as labels.
func dnsName(name string) []byte {
	var b []byte
	for _, l := range strings.Split(name, ".") {
		b = append(b, byte(len(l))) //nolint:gosec // test labels are short
		b = append(b, l...)
	}
	return append(b, 0)
}

// rr is one resource record, with the name and the data already encoded.
func rr(name []byte, typ uint16, data []byte) []byte {
	b := append([]byte(nil), name...)
	b = binary.BigEndian.AppendUint16(b, typ)
	b = binary.BigEndian.AppendUint16(b, 1)
	b = binary.BigEndian.AppendUint32(b, 120)
	b = binary.BigEndian.AppendUint16(b, uint16(len(data))) //nolint:gosec // test records are short
	return append(b, data...)
}

// response builds an mDNS answer packet that holds the given records.
func response(records ...[]byte) []byte {
	b := make([]byte, 12)
	b[2] = 0x84                                             // a response, authoritative
	binary.BigEndian.PutUint16(b[6:], uint16(len(records))) //nolint:gosec // a test packet holds a few records
	for _, r := range records {
		b = append(b, r...)
	}
	return b
}

func ptr(service, target string) []byte {
	return rr(dnsName(service), dnsTypePTR, dnsName(target))
}

func aRecord(host string, ip string) []byte {
	a := netip.MustParseAddr(ip).As4()
	return rr(dnsName(host), dnsTypeA, a[:])
}

func mdnsInput(on bool, hostname string, ips ...string) Input {
	cfg := testConfig()
	cfg.Discovery.MDNS = on
	devs := liveDevices(ips...)
	for i := range devs {
		devs[i].Hostname = hostname
	}
	return Input{Devices: devs, Config: cfg}
}

func printerAnswer(ip string) datagram {
	return datagram{from: netip.MustParseAddr(ip), data: response(
		ptr(mdnsQuestion, "_ipp._tcp.local"),
		ptr(mdnsQuestion, "_printer._tcp.local"),
		aRecord("office-printer.local", ip),
	)}
}

func TestMDNSQueryAsksForAllServiceTypes(t *testing.T) {
	q := mdnsQuery()
	if len(q) < 12 || binary.BigEndian.Uint16(q[4:]) != 1 {
		t.Fatalf("query = %x, want a header with one question", q)
	}
	name, next, ok := readDNSName(q, 12)
	if !ok || name != mdnsQuestion || binary.BigEndian.Uint16(q[next:]) != dnsTypePTR {
		t.Errorf("question = %q ok=%v, want a PTR for %s", name, ok, mdnsQuestion)
	}
}

func TestMDNSTakesTheTypesAndTheNameOfADevice(t *testing.T) {
	fake := &fakeMulticast{reply: []datagram{printerAnswer("192.168.1.20")}}
	c := &MDNS{log: quietLogger(), net: fake, types: map[string][]string{}}

	res, err := c.Collect(context.Background(), mdnsInput(true, "", "192.168.1.20"))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.serviceTypes("192.168.1.20"); strings.Join(got, ",") != "_ipp._tcp,_printer._tcp" {
		t.Errorf("types = %v", got)
	}
	if len(res.Devices) != 1 || res.Devices[0].Hostname != "office-printer.local" || res.Devices[0].Source != "mdns" {
		t.Errorf("devices = %+v, want the .local name of the printer", res.Devices)
	}
	if fake.sends() != 1 || fake.groups[0] != mdnsGroup {
		t.Errorf("sends = %d to %v, want one query to %v", fake.sends(), fake.groups, mdnsGroup)
	}
}

func TestMDNSDoesNotReplaceAKnownName(t *testing.T) {
	fake := &fakeMulticast{reply: []datagram{printerAnswer("192.168.1.20")}}
	c := &MDNS{log: quietLogger(), net: fake, types: map[string][]string{}}

	res, err := c.Collect(context.Background(), mdnsInput(true, "printer-from-dns", "192.168.1.20"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Devices) != 0 {
		t.Errorf("devices = %+v, a device that has a name gets no new one", res.Devices)
	}
	if len(c.serviceTypes("192.168.1.20")) != 2 {
		t.Errorf("the service types are still kept for the classifier")
	}
}

func TestMDNSIgnoresUnknownSendersAndForeignAddresses(t *testing.T) {
	fake := &fakeMulticast{reply: []datagram{
		printerAnswer("192.168.1.99"), // not a device of this scan
		{from: netip.MustParseAddr("192.168.1.20"), data: response(
			ptr(mdnsQuestion, "_ipp._tcp.local"),
			aRecord("someone-else.local", "192.168.1.50"), // a record for another host
		)},
	}}
	c := &MDNS{log: quietLogger(), net: fake, types: map[string][]string{}}

	res, err := c.Collect(context.Background(), mdnsInput(true, "", "192.168.1.20"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Devices) != 0 {
		t.Errorf("devices = %+v, a name that points at another address is not taken", res.Devices)
	}
	if got := c.serviceTypes("192.168.1.99"); len(got) != 0 {
		t.Errorf("types of an unknown host = %v", got)
	}
}

func TestMDNSSendsNothingWhenTheSourceIsOff(t *testing.T) {
	fake := &fakeMulticast{reply: []datagram{printerAnswer("192.168.1.20")}}
	c := &MDNS{log: quietLogger(), net: fake, types: map[string][]string{}}

	res, err := c.Collect(context.Background(), mdnsInput(false, "", "192.168.1.20"))
	if err != nil || len(res.Devices) != 0 || fake.sends() != 0 {
		t.Errorf("result = %+v, err = %v, sends = %d, want nothing", res, err, fake.sends())
	}
}

func TestMDNSSkipsBrokenPacketsWithoutFailing(t *testing.T) {
	good := response(ptr(mdnsQuestion, "_ipp._tcp.local"))
	loop := response(rr([]byte{0xC0, 12}, dnsTypePTR, dnsName("_ipp._tcp.local"))) // a pointer to itself
	cases := map[string][]byte{
		"empty":               nil,
		"short header":        {0, 0, 0x84},
		"not a response":      append([]byte{0, 0, 0, 0}, good[4:]...),
		"cut in a record":     good[:len(good)-6],
		"pointer loop":        loop,
		"too many records":    func() []byte { b := append([]byte(nil), good...); binary.BigEndian.PutUint16(b[6:], 5000); return b }(),
		"label over the size": append(make([]byte, 12), 0x80, 1, 2),
		"not UTF-8 type":      response(rr(dnsName(mdnsQuestion), dnsTypePTR, dnsName("_\xff\xfe._tcp.local"))),
	}
	for name, packet := range cases {
		fake := &fakeMulticast{reply: []datagram{{from: netip.MustParseAddr("192.168.1.20"), data: packet}}}
		c := &MDNS{log: quietLogger(), net: fake, types: map[string][]string{}}
		res, err := c.Collect(context.Background(), mdnsInput(true, "", "192.168.1.20"))
		if err != nil || len(res.Devices) != 0 || len(c.serviceTypes("192.168.1.20")) != 0 {
			t.Errorf("%s: result = %+v, err = %v, types = %v, want it skipped", name, res, err, c.serviceTypes("192.168.1.20"))
		}
	}
}

func TestMDNSReadsCompressedNames(t *testing.T) {
	// The second record names the first one by a pointer.
	first := rr(dnsName(mdnsQuestion), dnsTypePTR, dnsName("_ipp._tcp.local"))
	offset := 12 // the name of the first record starts right after the header
	second := rr([]byte{0xC0, byte(offset)}, dnsTypePTR, dnsName("_airplay._tcp.local"))
	msg, ok := parseMDNS(response(first, second))
	if !ok || strings.Join(msg.serviceTypes, ",") != "_ipp._tcp,_airplay._tcp" {
		t.Errorf("types = %v ok=%v, want both, the second found through the pointer", msg.serviceTypes, ok)
	}
}

func TestMDNSServiceTypeIsNotMistakenForAnotherKind(t *testing.T) {
	for _, bad := range []string{"printer.local", "_ipp._icmp.local", "_._tcp.local", "_a b._tcp.local", "_ipp._tcp.extra.local"} {
		if got, ok := serviceType(bad); ok {
			t.Errorf("serviceType(%q) = %q, want it refused", bad, got)
		}
	}
	if got, ok := serviceType("_IPP._tcp.local"); !ok || got != "_ipp._tcp" {
		t.Errorf("serviceType = %q ok=%v, want _ipp._tcp", got, ok)
	}
}

// The path of criterion 3: a device that says nothing over SNMP but offers _ipp._tcp.
func TestADeviceThatOffersIPPBecomesAPrinter(t *testing.T) {
	c := mustClassifier(t, "mdns,_ipp._tcp,printer\n")
	devs := []model.DeviceInput{{Key: model.DeviceKey{IP: "192.168.1.20"}, Source: "arp"}}
	c.Apply(devs, func(model.DeviceKey) Facts { return Facts{Services: -1, MDNS: []string{"_ipp._tcp"}} })
	if devs[0].Kind != model.KindPrinter {
		t.Errorf("kind = %q, want printer", devs[0].Kind)
	}

	// A kind that LLDP reported stays.
	devs = []model.DeviceInput{{Key: model.DeviceKey{IP: "192.168.1.20"}, Source: "lldp", Kind: model.KindSwitch}}
	c.Apply(devs, func(model.DeviceKey) Facts { return Facts{Services: -1, MDNS: []string{"_ipp._tcp"}} })
	if devs[0].Kind != model.KindSwitch {
		t.Errorf("kind = %q, an LLDP kind must not change", devs[0].Kind)
	}
}
