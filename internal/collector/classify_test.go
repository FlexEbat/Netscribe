package collector

import (
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/model"
)

const testRules = `# a rule file of its own, to show the rules are data
descr,Gadget OS,iot
services,bridge,switch
services,router,router
port,9100,printer
port,554,camera
vendor,Acme,server
`

func mustClassifier(t *testing.T, csv string) *Classifier {
	t.Helper()
	c, err := NewClassifier(strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestClassifierKind(t *testing.T) {
	c := mustClassifier(t, testRules)
	cases := []struct {
		name   string
		descr  string
		vendor string
		facts  Facts
		want   model.DeviceKind
	}{
		{"port 9100 is a printer", "", "", Facts{Services: -1, Ports: []int{22, 9100}}, model.KindPrinter},
		{"port 554 is a camera", "", "", Facts{Services: -1, Ports: []int{554}}, model.KindCamera},
		{"text of sysDescr", "Runs GADGET os 2.1", "", Facts{Services: -1}, model.KindIoT},
		{"bridge bit", "", "", Facts{Services: 2}, model.KindSwitch},
		{"router bit", "", "", Facts{Services: 4}, model.KindRouter},
		{"bridge and router stays a switch, the rule is first", "", "", Facts{Services: 6}, model.KindSwitch},
		{"vendor", "", "Acme Corp", Facts{Services: -1}, model.KindServer},
		{"sysDescr beats ports and vendor", "gadget os", "Acme", Facts{Services: -1, Ports: []int{9100}}, model.KindIoT},
		{"services beat ports", "", "", Facts{Services: 2, Ports: []int{9100}}, model.KindSwitch},
		{"ports beat vendor", "", "Acme", Facts{Services: -1, Ports: []int{554}}, model.KindCamera},
		{"a host layer only says nothing", "", "", Facts{Services: 72}, model.KindUnknown},
		{"nothing matches", "plain", "Other", Facts{Services: -1, Ports: []int{80}}, model.KindUnknown},
		{"unknown services do not match a bit rule", "", "", Facts{Services: -1}, model.KindUnknown},
	}
	for _, tc := range cases {
		if got := c.Kind(tc.descr, tc.vendor, tc.facts); got != tc.want {
			t.Errorf("%s: kind = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestClassifierApplyKeepsWhatIsKnown(t *testing.T) {
	c := mustClassifier(t, testRules)
	a := model.DeviceKey{MAC: "aa:aa:aa:aa:aa:01", IP: "10.0.0.1"}
	b := model.DeviceKey{MAC: "aa:aa:aa:aa:aa:02", IP: "10.0.0.2"}
	d := model.DeviceKey{MAC: "aa:aa:aa:aa:aa:03", IP: "10.0.0.3"}
	devs := []model.DeviceInput{
		{Key: a, Source: "arp"},                      // 0: nothing said, port 9100 -> printer
		{Key: b, Source: "arp"},                      // 1
		{Key: b, Kind: model.KindAP, Source: "snmp"}, // LLDP said ap, rules would say printer
		{Key: d, Source: "arp"},                      // 3: nothing matches
		{Key: a, Description: "x", Source: "snmp"},   // a second input of a
	}
	facts := func(k model.DeviceKey) Facts {
		if k == a || k == b {
			return Facts{Services: -1, Ports: []int{9100}}
		}
		return Facts{Services: -1}
	}
	c.Apply(devs, facts)
	if devs[0].Kind != model.KindPrinter {
		t.Errorf("a = %q, want printer written to its first input", devs[0].Kind)
	}
	if devs[1].Kind != "" || devs[2].Kind != model.KindAP {
		t.Errorf("b = %q / %q, a kind LLDP reported must stay", devs[1].Kind, devs[2].Kind)
	}
	if devs[3].Kind != "" {
		t.Errorf("d = %q, a device nothing matches stays without a kind (unknown in the store)", devs[3].Kind)
	}
}

func TestClassifierApplyWithoutFacts(t *testing.T) {
	c := mustClassifier(t, testRules)
	devs := []model.DeviceInput{{Key: model.DeviceKey{IP: "10.0.0.1"}, Vendor: "Acme"}}
	c.Apply(devs, nil)
	if devs[0].Kind != model.KindServer {
		t.Errorf("kind = %q", devs[0].Kind)
	}
}

func TestClassifierRejectsBadRules(t *testing.T) {
	for name, csv := range map[string]string{
		"too few columns": "port,9100\n",
		"too many":        "port,9100,printer,extra\n",
		"unknown kind":    "port,9100,toaster\n",
		"unknown field":   "mood,happy,host\n",
		"bad port":        "port,http,printer\n",
		"port too big":    "port,70000,printer\n",
		"bad services":    "services,hub,switch\n",
		"empty match":     "descr,,printer\n",
		"unknown as kind": "port,9100,unknown\n",
	} {
		if _, err := NewClassifier(strings.NewReader(csv)); err == nil {
			t.Errorf("%s: accepted %q", name, csv)
		}
	}
}

// The embedded file is the data the product ships with: it must parse, and it must give
// the kinds the spec promises.
func TestDefaultRules(t *testing.T) {
	c, err := DefaultClassifier()
	if err != nil {
		t.Fatalf("classify_rules.csv: %v", err)
	}
	if got := c.Kind("", "", Facts{Services: -1, Ports: []int{9100}}); got != model.KindPrinter {
		t.Errorf("port 9100 = %q, want printer", got)
	}
	for _, p := range []int{515, 631} {
		if got := c.Kind("", "", Facts{Services: -1, Ports: []int{p}}); got != model.KindPrinter {
			t.Errorf("port %d = %q, want printer", p, got)
		}
	}
	if got := c.Kind("", "", Facts{Services: -1, Ports: []int{554}}); got != model.KindCamera {
		t.Errorf("port 554 = %q, want camera", got)
	}
	if got := c.Kind("", "", Facts{Services: -1, Ports: []int{22, 80}}); got != model.KindUnknown {
		t.Errorf("ssh and http prove nothing, got %q", got)
	}
}

const sourceRules = `# rules for what a device says over HTTP, mDNS and SSDP
http,Hikvision,camera
http,cups,printer
mdns,_ipp._tcp,printer
mdns,_airplay._tcp,iot
ssdp,InternetGatewayDevice,router
descr,Gadget OS,iot
port,22,server
`

func TestClassifierRulesForHTTPMDNSAndSSDP(t *testing.T) {
	c := mustClassifier(t, sourceRules)
	cases := []struct {
		name  string
		descr string
		facts Facts
		want  model.DeviceKind
	}{
		{"http text, any case", "", Facts{Services: -1, HTTP: "Server: HIKVISION-Webs"}, model.KindCamera},
		{"mdns type", "", Facts{Services: -1, MDNS: []string{"_ssh._tcp", "_ipp._tcp"}}, model.KindPrinter},
		{"mdns type must match whole", "", Facts{Services: -1, MDNS: []string{"_ipp._tcp.extra"}}, model.KindUnknown},
		{"ssdp text", "", Facts{Services: -1, SSDP: []string{"urn:schemas-upnp-org:device:internetgatewaydevice:1"}}, model.KindRouter},
		{"sysDescr beats http", "Gadget OS", Facts{Services: -1, HTTP: "cups"}, model.KindIoT},
		{"http beats mdns", "", Facts{Services: -1, HTTP: "cups", MDNS: []string{"_airplay._tcp"}}, model.KindPrinter},
		{"mdns beats a port", "", Facts{Services: -1, Ports: []int{22}, MDNS: []string{"_airplay._tcp"}}, model.KindIoT},
		{"nothing matches", "", Facts{Services: -1, HTTP: "nginx"}, model.KindUnknown},
	}
	for _, tc := range cases {
		if got := c.Kind(tc.descr, "", tc.facts); got != tc.want {
			t.Errorf("%s: kind = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestClassifierRefusesABadMDNSRule(t *testing.T) {
	for _, line := range []string{"mdns,ipp,printer", "mdns,_ipp _tcp,printer", "http,,printer", "ssdp,,router"} {
		if _, err := NewClassifier(strings.NewReader(line + "\n")); err == nil {
			t.Errorf("%q was accepted", line)
		}
	}
}

func TestDefaultRulesKnowThePrinterServices(t *testing.T) {
	c, err := DefaultClassifier()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Kind("", "", Facts{Services: -1, MDNS: []string{"_ipp._tcp"}}); got != model.KindPrinter {
		t.Errorf("kind = %q, want printer from _ipp._tcp", got)
	}
}
