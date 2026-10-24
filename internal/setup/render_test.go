package setup

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/config"
)

func TestRenderedConfigLoadsWithTheChosenValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "netscribe.yaml")
	text := RenderConfig(Answers{
		Listen: "127.0.0.1:9090", Database: "/var/lib/netscribe/netscribe.db",
		Targets: []string{"192.168.1.0/24", "10.0.5.0/24"}, Interval: "30m",
		Docker: true, DockerSocket: "/var/run/docker.sock", Nmap: true,
	})
	if err := os.WriteFile(path, text, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path, false)
	if err != nil {
		t.Fatalf("the rendered config does not load: %v\n%s", err, text)
	}
	if cfg.Listen != "127.0.0.1:9090" || cfg.Database != "/var/lib/netscribe/netscribe.db" {
		t.Errorf("listen %q, database %q", cfg.Listen, cfg.Database)
	}
	if got := strings.Join(cfg.Scan.Targets, ","); got != "192.168.1.0/24,10.0.5.0/24" {
		t.Errorf("targets %q", got)
	}
	if cfg.Scan.Interval.Std().Minutes() != 30 || !cfg.Docker.Enabled || !cfg.Discovery.Nmap {
		t.Errorf("interval %v, docker %v, nmap %v", cfg.Scan.Interval.Std(), cfg.Docker.Enabled, cfg.Discovery.Nmap)
	}
}

func TestRenderedConfigNamesNoSecretAndNoCommunity(t *testing.T) {
	var keys []string
	for _, line := range strings.Split(string(RenderConfig(Answers{Listen: "127.0.0.1:8080", Database: "/x/db", Targets: []string{"192.168.1.0/24"}, Interval: "15m", DockerSocket: "/s"})), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			keys = append(keys, line)
		}
	}
	text := strings.Join(keys, "\n")
	for _, word := range []string{"communities", "keyFile", "password", "public"} {
		if strings.Contains(text, word) {
			t.Errorf("the generated config sets %q", word)
		}
	}
}

func TestEmbeddedUnitEqualsTheShippedUnit(t *testing.T) {
	shipped, err := os.ReadFile("../../deploy/netscribe.service")
	if err != nil {
		t.Fatal(err)
	}
	if string(shipped) != unitTemplate {
		t.Fatal("internal/setup/netscribe.service differs from deploy/netscribe.service: copy the second over the first")
	}
}

func TestRenderUnitPointsAtTheInstall(t *testing.T) {
	o := DefaultOptions()
	o.BinPath, o.ConfigPath, o.StateDir, o.ServiceUser = "/opt/ns/bin/netscribe", "/srv/ns/ns.yaml", "/srv/ns/data", "ns"
	got, err := RenderUnit(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"ExecStart=/opt/ns/bin/netscribe serve --config /srv/ns/ns.yaml",
		"ReadWritePaths=/srv/ns/data",
		"User=ns\nGroup=ns",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("unit lacks %q", want)
		}
	}
	if strings.Contains(got, "/usr/local/bin") || strings.Contains(got, "/etc/netscribe") {
		t.Error("the unit still names a default path")
	}
}

func TestRenderUnitOfTheDefaultInstallIsTheShippedUnit(t *testing.T) {
	got, err := RenderUnit(DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if got != unitTemplate {
		t.Error("the default install must produce the shipped unit unchanged")
	}
}

func TestDropIn(t *testing.T) {
	if RenderDropIn("", false) != "" {
		t.Error("no feature, no drop-in")
	}
	d := RenderDropIn("docker", true)
	for _, want := range []string{"[Service]", "SupplementaryGroups=docker", "AmbientCapabilities=CAP_NET_RAW CAP_NET_ADMIN", "CapabilityBoundingSet=CAP_NET_RAW CAP_NET_ADMIN"} {
		if !strings.Contains(d, want) {
			t.Errorf("drop-in lacks %q:\n%s", want, d)
		}
	}
	if strings.Contains(RenderDropIn("", true), "SupplementaryGroups") {
		t.Error("a group without Docker")
	}
}

func TestCandidates(t *testing.T) {
	pfx := netip.MustParsePrefix
	got := Candidates([]NetIface{
		{Name: "lo", Up: true, Loopback: true, Addrs: []netip.Prefix{pfx("127.0.0.1/8")}},
		{Name: "eth0", Up: true, Addrs: []netip.Prefix{pfx("192.168.1.20/24"), pfx("fe80::1/64"), pfx("192.168.1.21/24")}},
		{Name: "eth1", Up: true, Addrs: []netip.Prefix{pfx("10.1.2.3/16")}},
		{Name: "eth2", Up: false, Addrs: []netip.Prefix{pfx("192.168.50.2/24")}},
		{Name: "docker0", Up: true, Addrs: []netip.Prefix{pfx("172.17.0.1/16")}},
		{Name: "br-1a2b", Up: true, Addrs: []netip.Prefix{pfx("172.20.0.1/16")}},
		{Name: "wan", Up: true, Addrs: []netip.Prefix{pfx("203.0.113.9/24")}},
		{Name: "ppp0", Up: true, Addrs: []netip.Prefix{pfx("10.9.9.1/32")}},
	})
	var list []string
	for _, c := range got {
		list = append(list, c.Prefix.String())
	}
	if want := "192.168.1.0/24 10.1.2.0/24"; strings.Join(list, " ") != want {
		t.Fatalf("candidates %v, want %s", list, want)
	}
	if got[1].Note == "" {
		t.Error("a /16 network narrowed to a /24 must say so")
	}
}

func TestOverlaps(t *testing.T) {
	a, b, c := netip.MustParsePrefix("10.0.0.0/16"), netip.MustParsePrefix("10.0.5.0/24"), netip.MustParsePrefix("10.1.0.0/24")
	if !overlaps(a, b) || !overlaps(b, a) || overlaps(a, c) {
		t.Error("overlap of nested and separate prefixes")
	}
}
