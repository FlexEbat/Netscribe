package collector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
)

// fakeAgent answers like a device whose snmpwalk output was recorded in a fixture.
type fakeAgent struct {
	community string
	rows      []snmpPDU // in OID order
}

// loadAgent reads a fixture in the format of "snmpwalk -On": one "OID = TYPE: value"
// per line, a line without " = " continues the previous STRING.
func loadAgent(t *testing.T, name, community string) *fakeAgent {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name)) //nolint:gosec // a fixture name written in the test
	if err != nil {
		t.Fatal(err)
	}
	var rows []snmpPDU
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		oid, rest, ok := strings.Cut(line, " = ")
		if !ok {
			last := &rows[len(rows)-1]
			last.Value = append(last.Value.([]byte), '\n')
			last.Value = append(last.Value.([]byte), line...)
			continue
		}
		typ, val, _ := strings.Cut(rest, ": ")
		row := snmpPDU{OID: strings.TrimPrefix(oid, ".")}
		switch typ {
		case "STRING":
			row.Value = []byte(val)
		case "Hex-STRING":
			var b []byte
			for _, h := range strings.Fields(val) {
				n, err := strconv.ParseUint(h, 16, 8)
				if err != nil {
					t.Fatalf("%s: bad hex %q", name, h)
				}
				b = append(b, byte(n))
			}
			row.Value = b
		case "INTEGER":
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				t.Fatalf("%s: bad integer %q", name, val)
			}
			row.Value = n
		default:
			t.Fatalf("%s: unsupported type %q", name, typ)
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool { return oidLess(rows[i].OID, rows[j].OID) })
	return &fakeAgent{community: community, rows: rows}
}

func oidLess(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		x, _ := strconv.Atoi(as[i])
		y, _ := strconv.Atoi(bs[i])
		if x != y {
			return x < y
		}
	}
	return len(as) < len(bs)
}

func (a *fakeAgent) Get(_ context.Context, oids []string) ([]snmpPDU, error) {
	var out []snmpPDU
	for _, want := range oids {
		found := snmpPDU{OID: want}
		for _, r := range a.rows {
			if r.OID == want {
				found = r
			}
		}
		out = append(out, found)
	}
	return out, nil
}

func (a *fakeAgent) Walk(_ context.Context, root string) ([]snmpPDU, error) {
	var out []snmpPDU
	for _, r := range a.rows {
		if strings.HasPrefix(r.OID, root+".") {
			out = append(out, r)
		}
	}
	return out, nil
}

func (a *fakeAgent) Close() error { return nil }

// fakeNetwork maps addresses to agents. A wrong community or an unknown address times out.
type fakeNetwork map[string]*fakeAgent

func (n fakeNetwork) dial(_ context.Context, ip, community string, _ time.Duration) (snmpClient, error) {
	a, ok := n[ip]
	if !ok || a.community != community {
		return timeoutClient{}, nil
	}
	return a, nil
}

type timeoutClient struct{}

func (timeoutClient) Get(context.Context, []string) ([]snmpPDU, error) { return nil, errSNMPTimeout }
func (timeoutClient) Walk(context.Context, string) ([]snmpPDU, error)  { return nil, errSNMPTimeout }
func (timeoutClient) Close() error                                     { return nil }

func testConfig(communities ...string) config.Config {
	var c config.Config
	for _, s := range communities {
		c.SNMP.Communities = append(c.SNMP.Communities, config.Secret(s))
	}
	c.SNMP.Timeout = config.Duration(time.Second)
	return c
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)) }

func liveDevices(ips ...string) []model.Device {
	var out []model.Device
	for i, ip := range ips {
		out = append(out, model.Device{ID: int64(i + 1), IP: ip, MAC: fmt.Sprintf("aa:aa:aa:aa:aa:%02x", i+1)})
	}
	return out
}

func TestSNMPReadsSystemAndInterfaces(t *testing.T) {
	net := fakeNetwork{"192.168.1.2": loadAgent(t, "snmp_sw1.txt", "private")}
	c := newSNMP(net.dial, quietLogger())

	res, err := c.Collect(context.Background(), Input{
		Devices: liveDevices("192.168.1.2", "192.168.1.9"),
		Config:  testConfig("public", "private"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Devices) != 1 {
		t.Fatalf("%d devices, want 1: the silent device must stay as it is", len(res.Devices))
	}
	d := res.Devices[0]
	if d.Key.IP != "192.168.1.2" || d.Hostname != "sw1" || d.Source != "snmp" {
		t.Errorf("device = %+v", d)
	}
	if d.Description != "Acme Switch 24G fw 3.1" {
		t.Errorf("description = %q, want the lines joined", d.Description)
	}

	// mgmt0 has two addresses, Gi1 has a zero MAC and none, Gi2 is down.
	want := []model.InterfaceInput{
		{Name: "Gi1", Up: true},
		{Name: "Gi2", MAC: "02:11:22:33:44:03"},
		{Name: "mgmt0", MAC: "02:11:22:33:44:01", IP: "10.0.0.2", Up: true},
		{Name: "mgmt0", MAC: "02:11:22:33:44:01", IP: "192.168.1.2", Up: true},
	}
	if len(res.Interfaces) != len(want) {
		t.Fatalf("interfaces = %+v", res.Interfaces)
	}
	for i, w := range want {
		g := res.Interfaces[i]
		if g.Name != w.Name || g.MAC != w.MAC || g.IP != w.IP || g.Up != w.Up || g.Device.IP != "192.168.1.2" {
			t.Errorf("interface %d = %+v, want %+v", i, g, w)
		}
	}
}

func TestSNMPSecondRunGivesTheSameResult(t *testing.T) {
	net := fakeNetwork{"192.168.1.2": loadAgent(t, "snmp_sw1.txt", "public")}
	c := newSNMP(net.dial, quietLogger())
	in := Input{Devices: liveDevices("192.168.1.2"), Config: testConfig("public")}
	first, _ := c.Collect(context.Background(), in)
	second, _ := c.Collect(context.Background(), in)
	if fmt.Sprint(first) != fmt.Sprint(second) {
		t.Errorf("second run differs:\n%v\n%v", first, second)
	}
}

func TestSNMPNoAnswerIsNotAnError(t *testing.T) {
	c := newSNMP(fakeNetwork{}.dial, quietLogger())
	res, err := c.Collect(context.Background(), Input{Devices: liveDevices("192.168.1.9"), Config: testConfig("public")})
	if err != nil || len(res.Devices) != 0 || len(res.Interfaces) != 0 {
		t.Errorf("res = %+v, err = %v", res, err)
	}
}

func TestSNMPWithoutCommunitiesDoesNothing(t *testing.T) {
	dialed := false
	c := newSNMP(func(context.Context, string, string, time.Duration) (snmpClient, error) {
		dialed = true
		return nil, errors.New("must not be called")
	}, quietLogger())
	res, err := c.Collect(context.Background(), Input{Devices: liveDevices("192.168.1.2"), Config: testConfig()})
	if err != nil || dialed || len(res.Devices) != 0 {
		t.Errorf("dialed = %v, res = %+v, err = %v", dialed, res, err)
	}
}

func TestSNMPStopsWhenCanceled(t *testing.T) {
	c := newSNMP(fakeNetwork{}.dial, quietLogger())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Collect(ctx, Input{Devices: liveDevices("192.168.1.2"), Config: testConfig("public")}); !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestSNMPCommunityStaysOutOfLogsAndResults(t *testing.T) {
	const secret = "s3cr3t-community"
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	net := fakeNetwork{"192.168.1.2": loadAgent(t, "snmp_sw1.txt", secret)}
	c := newSNMP(net.dial, log)
	cfg := testConfig("wrong", secret)

	res, err := c.Collect(context.Background(), Input{Devices: liveDevices("192.168.1.2", "192.168.1.9"), Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"log": logs.String(), "result": fmt.Sprintf("%+v", res), "config": fmt.Sprintf("%v %+v", cfg, cfg.SNMP),
	} {
		if strings.Contains(text, secret) || strings.Contains(text, "wrong") {
			t.Errorf("the %s contains a community: %s", name, text)
		}
	}
}

func TestBuildInterfacesNamesAndFallbacks(t *testing.T) {
	rows := map[string][]snmpPDU{
		oidIfDescr: {{OID: oidIfDescr + ".7", Value: []byte("Loopback")}},
		oidIfName:  {{OID: oidIfName + ".8", Value: []byte("lo1")}},
	}
	got := buildInterfaces(model.DeviceKey{IP: "10.0.0.1"}, rows)
	if len(got) != 2 || got[0].Name != "Loopback" || got[1].Name != "lo1" {
		t.Errorf("got %+v", got)
	}
	if n := buildInterfaces(model.DeviceKey{}, map[string][]snmpPDU{oidIfOperStatus: {{OID: oidIfOperStatus + ".4", Value: int64(1)}}}); len(n) != 1 || n[0].Name != "if4" || !n[0].Up {
		t.Errorf("an interface without a name gets ifN: %+v", n)
	}
}

func TestBuildInterfacesIgnoresMalformedRows(t *testing.T) {
	rows := map[string][]snmpPDU{
		oidIfDescr:   {{OID: oidIfDescr + ".x", Value: []byte("bad")}, {OID: "9.9.9", Value: []byte("alien")}},
		oidIPAdEntIf: {{OID: oidIPAdEntIf + ".300.1.1.1", Value: int64(1)}, {OID: oidIPAdEntIf + ".1.1.1.1", Value: "no"}},
	}
	if got := buildInterfaces(model.DeviceKey{}, rows); len(got) != 0 {
		t.Errorf("got %+v, want nothing", got)
	}
}

func TestFormatMAC(t *testing.T) {
	cases := []struct {
		in   []byte
		want string
	}{
		{[]byte{0x02, 0x11, 0x22, 0x33, 0x44, 0xab}, "02:11:22:33:44:ab"},
		{[]byte{0, 0, 0, 0, 0, 0}, ""},
		{[]byte{1, 2, 3}, ""},
		{nil, ""},
	}
	for _, c := range cases {
		if got := formatMAC(c.in); got != c.want {
			t.Errorf("formatMAC(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
