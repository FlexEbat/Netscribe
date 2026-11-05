package collector

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/model"
)

const testRules = `# a rule file of its own, to show the rules are data
descr,Gadget OS,iot,80
services,bridge,switch,60
services,router,router,60
port,9100,printer,50
port,554,camera,50
vendor,Acme,server,40
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
		{"bridge and router bits weigh the same, so neither wins", "", "", Facts{Services: 6}, model.KindUnknown},
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
		"too many":        "port,9100,printer,50,extra\n",
		"bad weight":      "port,9100,printer,heavy\n",
		"weight zero":     "port,9100,printer,0\n",
		"weight too big":  "port,9100,printer,101\n",
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
http,Hikvision,camera,60
http,cups,printer,60
mdns,_ipp._tcp,printer,50
mdns,_airplay._tcp,iot,50
ssdp,InternetGatewayDevice,router,50
descr,Gadget OS,iot,80
port,22,server,40
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

// A table of fixtures: which kind wins, with which score, and why (section 5.2).
func TestClassifyScoresAndExplains(t *testing.T) {
	c, err := DefaultClassifier()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		descr  string
		vendor string
		facts  Facts
		kind   model.DeviceKind
		reason string // a phrase that must be among the reasons
	}{
		{"a printer by port 9100 and an HTTP banner", "", "", Facts{Services: -1, Ports: []int{80, 9100}, HTTP: "server: cups 2.4"}, model.KindPrinter, "port 9100"},
		{"a printer by its certificate", "", "", Facts{Services: -1, TLS: []string{"cn=laserjet 4000"}}, model.KindPrinter, "TLS subject: laserjet"},
		{"a router by SNMP services and its certificate", "", "", Facts{Services: 4, TLS: []string{"cn=mikrotik"}}, model.KindRouter, "SNMP services: router"},
		{"a camera by port 554 and its vendor", "", "Hikvision Digital", Facts{Services: -1, Ports: []int{554}}, model.KindCamera, "vendor: hikvision"},
		{"an access point by sysDescr alone", "Cisco Access Point 1815", "", Facts{Services: -1}, model.KindAP, "sysDescr: access point"},
		{"a device by its DHCP vendor class", "", "", Facts{Services: -1, DHCP: []string{"android-dhcp-13"}}, model.KindHost, "DHCP: android-dhcp"},
	}
	for _, tc := range cases {
		v := c.Classify(tc.descr, tc.vendor, tc.facts)
		if v.Kind != tc.kind {
			t.Errorf("%s: kind = %q (%v), want %q", tc.name, v.Kind, v.Reasons, tc.kind)
			continue
		}
		if v.Score < minScore || v.Score > maxScore {
			t.Errorf("%s: score = %d, want 40 to 100", tc.name, v.Score)
		}
		if !slices.Contains(v.Reasons, tc.reason) {
			t.Errorf("%s: reasons = %v, want %q among them", tc.name, v.Reasons, tc.reason)
		}
		if len(v.Reasons) > maxWhy {
			t.Errorf("%s: %d reasons, want at most %d", tc.name, len(v.Reasons), maxWhy)
		}
	}
}

func TestClassifyStaysUnknownWhenTheEvidenceIsWeakOrSplit(t *testing.T) {
	c := mustClassifier(t, "port,9100,printer,30\nport,554,camera,45\nport,22,server,45\nport,25,camera,46\nvendor,Acme,printer,55\n")
	cases := []struct {
		name   string
		vendor string
		ports  []int
		kind   model.DeviceKind
		why    []string
	}{
		{"a winner below 40", "", []int{9100}, model.KindUnknown, []string{"candidate printer (30)"}},
		{"two kinds within 10 points", "", []int{554, 22}, model.KindUnknown, []string{"candidate camera (45)", "candidate server (45)"}},
		{"a gap of exactly 10 is enough", "Acme", []int{554}, model.KindPrinter, []string{"vendor: acme"}},
		{"a gap of 9 is not", "Acme", []int{25}, model.KindUnknown, []string{"candidate printer (55)", "candidate camera (46)"}},
	}
	for _, tc := range cases {
		v := c.Classify("", tc.vendor, Facts{Services: -1, Ports: tc.ports})
		if v.Kind != tc.kind || !slices.Equal(v.Reasons, tc.why) {
			t.Errorf("%s: %q %v, want %q %v", tc.name, v.Kind, v.Reasons, tc.kind, tc.why)
		}
		if tc.kind == model.KindUnknown && v.Score != 0 {
			t.Errorf("%s: score = %d, want 0 for an unknown kind", tc.name, v.Score)
		}
	}
}

func TestOldRulesWithoutWeightsStillLoadAndWeighFifty(t *testing.T) {
	c := mustClassifier(t, "port,9100,printer\nport,554,camera\n")
	v := c.Classify("", "", Facts{Services: -1, Ports: []int{9100}})
	if v.Kind != model.KindPrinter || v.Score != 50 {
		t.Errorf("verdict = %+v, want a printer with the default weight 50", v)
	}
}

func TestClassifyCapsTheScoreAndTheReasons(t *testing.T) {
	var rules strings.Builder
	for i := 0; i < 8; i++ {
		fmt.Fprintf(&rules, "http,token%d,printer,60\n", i)
	}
	c := mustClassifier(t, rules.String())
	v := c.Classify("", "", Facts{Services: -1, HTTP: "token0 token1 token2 token3 token4 token5 token6 token7"})
	if v.Kind != model.KindPrinter || v.Score != maxScore || len(v.Reasons) != maxWhy {
		t.Errorf("verdict = %+v, want a printer, score 100 and 5 reasons", v)
	}
}

func TestApplyWritesTheScoreAndTheCandidates(t *testing.T) {
	c := mustClassifier(t, "port,9100,printer,50\nport,554,camera,45\nport,22,server,45\n")
	a := model.DeviceKey{MAC: "aa:aa:aa:aa:aa:01", IP: "10.0.0.1"}
	b := model.DeviceKey{MAC: "aa:aa:aa:aa:aa:02", IP: "10.0.0.2"}
	devs := []model.DeviceInput{{Key: a}, {Key: b}}
	c.Apply(devs, func(k model.DeviceKey) Facts {
		if k == a {
			return Facts{Services: -1, Ports: []int{9100}}
		}
		return Facts{Services: -1, Ports: []int{554, 22}}
	})
	if devs[0].Kind != model.KindPrinter || devs[0].KindScore != 50 || !slices.Equal(devs[0].KindReasons, []string{"port 9100"}) {
		t.Errorf("a = %+v", devs[0])
	}
	if devs[1].Kind != "" || devs[1].KindScore != 0 || len(devs[1].KindReasons) != 2 {
		t.Errorf("b = %+v, want no kind and the two candidates as reasons", devs[1])
	}
}

func TestDefaultRulesAllCarryAWeight(t *testing.T) {
	for _, line := range strings.Split(defaultRules, "\n") {
		t2 := strings.TrimSpace(line)
		if t2 == "" || strings.HasPrefix(t2, "#") {
			continue
		}
		if strings.Count(t2, ",") != 3 {
			t.Errorf("rule %q has no weight column", t2)
		}
	}
}
