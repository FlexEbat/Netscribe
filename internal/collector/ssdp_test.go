package collector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
)

func ssdpAnswer(location, server, st string) []byte {
	return []byte("HTTP/1.1 200 OK\r\n" +
		"CACHE-CONTROL: max-age=1800\r\n" +
		"LOCATION: " + location + "\r\n" +
		"SERVER: " + server + "\r\n" +
		"ST: " + st + "\r\n" +
		"USN: uuid:1234::" + st + "\r\n\r\n")
}

func ssdpInput(on bool, ips ...string) Input {
	cfg := testConfig()
	cfg.Discovery.SSDP = on
	return Input{Devices: liveDevices(ips...), Config: cfg}
}

func TestParseSSDPTakesServerAndTargetAndNotTheLocation(t *testing.T) {
	got := parseSSDP(ssdpAnswer("http://192.168.1.1:5000/rootDesc.xml", "Linux/5.4 UPnP/1.1 MiniUPnPd/2.2",
		"urn:schemas-upnp-org:device:InternetGatewayDevice:1"))
	want := "linux/5.4 upnp/1.1 miniupnpd/2.2,urn:schemas-upnp-org:device:internetgatewaydevice:1"
	if strings.Join(got, ",") != want {
		t.Errorf("parseSSDP = %v, want %q", got, want)
	}
	for _, v := range got {
		if strings.Contains(v, "5000") || strings.Contains(v, "rootdesc") {
			t.Errorf("%q holds the LOCATION address", v)
		}
	}
}

func TestParseSSDPSkipsWhatIsNotAnAnswer(t *testing.T) {
	cases := map[string][]byte{
		"empty":            nil,
		"binary garbage":   {0xff, 0xfe, 0x00, 0x01, 0x02},
		"the request":      ssdpSearch,
		"a failure status": []byte("HTTP/1.1 500 Oops\r\nSERVER: x\r\n\r\n"),
		"no headers":       []byte("HTTP/1.1 200 OK\r\n"),
		"not UTF-8":        []byte("HTTP/1.1 200 OK\r\nSERVER: \xff\xfe\r\nST: \xc3\x28\r\n\r\n"),
	}
	for name, packet := range cases {
		if got := parseSSDP(packet); len(got) != 0 {
			t.Errorf("%s: parseSSDP = %v, want nothing", name, got)
		}
	}
}

func TestParseSSDPCutsLongValuesAndDropsControlCharacters(t *testing.T) {
	got := parseSSDP(ssdpAnswer("http://x/", "A\tB"+strings.Repeat("c", 500), "ssdp:all"))
	if len(got) != 2 || len([]rune(got[0])) > 200 || strings.ContainsAny(got[0], "\t\r\n") || !strings.HasPrefix(got[0], "abccc") {
		t.Errorf("parseSSDP = %q, want a cut value without control characters", got)
	}
}

func TestSSDPSendsOneSearchAndKeepsWhatKnownDevicesSay(t *testing.T) {
	fake := &fakeMulticast{reply: []datagram{
		{from: netip.MustParseAddr("192.168.1.1"), data: ssdpAnswer("http://192.168.1.1:5000/d.xml", "MiniUPnPd/2.2", "upnp:rootdevice")},
		{from: netip.MustParseAddr("192.168.1.77"), data: ssdpAnswer("http://192.168.1.77/d.xml", "Stranger/1.0", "upnp:rootdevice")},
	}}
	c := &SSDP{log: quietLogger(), net: fake, found: map[string][]string{}}

	res, err := c.Collect(context.Background(), ssdpInput(true, "192.168.1.1"))
	if err != nil || len(res.Devices) != 0 {
		t.Fatalf("result = %+v, err = %v", res, err)
	}
	if fake.sends() != 1 || fake.groups[0] != ssdpGroup {
		t.Fatalf("sends = %d to %v, want one search to %v", fake.sends(), fake.groups, ssdpGroup)
	}
	for _, want := range []string{"M-SEARCH * HTTP/1.1", "ST: ssdp:all", `MAN: "ssdp:discover"`} {
		if !strings.Contains(string(fake.payloads[0]), want) {
			t.Errorf("the search lacks %q: %q", want, fake.payloads[0])
		}
	}
	if got := c.announced("192.168.1.1"); strings.Join(got, ",") != "miniupnpd/2.2,upnp:rootdevice" {
		t.Errorf("announced = %v", got)
	}
	if got := c.announced("192.168.1.77"); len(got) != 0 {
		t.Errorf("announced by an unknown host = %v", got)
	}
}

func TestSSDPNeverRequestsTheLocation(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()

	fake := &fakeMulticast{reply: []datagram{
		{from: netip.MustParseAddr("192.168.1.1"), data: ssdpAnswer(srv.URL+"/description.xml", "Server/1", "upnp:rootdevice")},
	}}
	c := &SSDP{log: quietLogger(), net: fake, found: map[string][]string{}}
	if _, err := c.Collect(context.Background(), ssdpInput(true, "192.168.1.1")); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 0 {
		t.Errorf("the LOCATION address was requested %d times", hits.Load())
	}
}

func TestSSDPSendsNothingWhenTheSourceIsOff(t *testing.T) {
	fake := &fakeMulticast{reply: []datagram{{from: netip.MustParseAddr("192.168.1.1"), data: ssdpAnswer("http://x/", "S", "ssdp:all")}}}
	c := &SSDP{log: quietLogger(), net: fake, found: map[string][]string{}}
	if _, err := c.Collect(context.Background(), ssdpInput(false, "192.168.1.1")); err != nil || fake.sends() != 0 {
		t.Errorf("err = %v, sends = %d, want nothing sent", err, fake.sends())
	}
	if got := c.announced("192.168.1.1"); len(got) != 0 {
		t.Errorf("announced = %v", got)
	}
}
