package setup

import (
	"strings"
	"testing"
)

const sample = `# Netscribe configuration
listen: 127.0.0.1:8080 # the panel

scan:
  # the networks to scan
  targets: [192.168.1.0/24]
  interval: 15m
snmp:
  communities: [public]
`

func TestSetKeepsTheCommentsAndTheOrder(t *testing.T) {
	doc, err := parseYAML([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.set(p("scan.interval"), strNode("30m")); err != nil {
		t.Fatal(err)
	}
	if err := doc.set(p("listen"), strNode("0.0.0.0:9090")); err != nil {
		t.Fatal(err)
	}
	out, err := doc.bytes()
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	for _, want := range []string{"# Netscribe configuration", "# the panel", "# the networks to scan", "interval: 30m", "listen: 0.0.0.0:9090", "targets: [192.168.1.0/24]"} {
		if !strings.Contains(text, want) {
			t.Errorf("the result lacks %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "listen:") > strings.Index(text, "scan:") || strings.Index(text, "scan:") > strings.Index(text, "snmp:") {
		t.Errorf("the order of the keys changed:\n%s", text)
	}
}

func TestSetCreatesSectionsAndRemoveDeletesKeys(t *testing.T) {
	doc, _ := parseYAML([]byte(sample))
	if err := doc.set(p("tls.certFile"), strNode("/etc/ssl/a.pem")); err != nil {
		t.Fatal(err)
	}
	if got := value(doc.find(p("tls.certFile"))); got != "/etc/ssl/a.pem" {
		t.Errorf("new key: %q", got)
	}
	doc.remove(p("tls"))
	doc.remove(p("scan.interval"))
	doc.remove(p("nothing.here"))
	out, _ := doc.bytes()
	if strings.Contains(string(out), "tls") || strings.Contains(string(out), "interval") {
		t.Errorf("keys stayed:\n%s", out)
	}
	if err := doc.set(p("listen.x"), strNode("y")); err == nil {
		t.Error("a value cannot become a section")
	}
}

func TestValueAndCount(t *testing.T) {
	doc, _ := parseYAML([]byte(sample))
	if got := value(doc.find(p("scan.targets"))); got != "192.168.1.0/24" {
		t.Errorf("list value %q", got)
	}
	if count(doc.find(p("snmp.communities"))) != 1 || count(doc.find(p("nothing"))) != 0 || value(nil) != "" {
		t.Error("count and value of missing nodes")
	}
}

func TestParseYAMLAcceptsAnEmptyFileAndRefusesAList(t *testing.T) {
	doc, err := parseYAML(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.set(p("listen"), strNode("127.0.0.1:1")); err != nil {
		t.Fatal(err)
	}
	if _, err := parseYAML([]byte("- a\n- b\n")); err == nil {
		t.Error("a list is not a config")
	}
	if _, err := parseYAML([]byte("a: [")); err == nil {
		t.Error("broken yaml")
	}
}

func TestListNodeKeepsNumbers(t *testing.T) {
	doc, _ := parseYAML(nil)
	_ = doc.set(p("scan.ports"), listNode([]string{"22", "443"}, true))
	_ = doc.set(p("scan.targets"), listNode([]string{"10.0.0.0/24"}, false))
	out, _ := doc.bytes()
	if !strings.Contains(string(out), "ports: [22, 443]") || !strings.Contains(string(out), "targets: [10.0.0.0/24]") {
		t.Errorf("flow lists:\n%s", out)
	}
}
