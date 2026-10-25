package setup

import (
	_ "embed"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/FlexEbat/Netscribe/internal/config"
)

// unitTemplate is a copy of deploy/netscribe.service. A test keeps the two equal: the
// binary cannot embed a file outside its packages.
//
//go:embed netscribe.service
var unitTemplate string

// Answers are the values that the setup writes into a new config. Every other key keeps
// its default, which the config package defines.
type Answers struct {
	Listen       string
	Database     string
	Targets      []string
	Interval     string
	Docker       bool
	DockerSocket string
	Nmap         bool
	TLSCert      string
	TLSKey       string
	Proxies      []string
}

// RenderConfig returns the text of a new config file.
func RenderConfig(a Answers) []byte {
	var b strings.Builder
	b.WriteString(`# Netscribe configuration, written by "netscribe setup".
# The file holds secrets (SNMP communities, SSH key paths): keep it at mode 0600.
# A key that is not listed here has its default. netscribe.example.yaml in the source
# tree lists every key with its default and a comment.
`)
	fmt.Fprintf(&b, "listen: %s\n", a.Listen)
	fmt.Fprintf(&b, "database: %s\n\n", strconv.Quote(a.Database))
	b.WriteString("scan:\n")
	fmt.Fprintf(&b, "  targets: [%s]\n", strings.Join(a.Targets, ", "))
	fmt.Fprintf(&b, "  interval: %s\n\n", a.Interval)
	if a.TLSCert != "" {
		b.WriteString("tls:\n")
		fmt.Fprintf(&b, "  certFile: %s\n", strconv.Quote(a.TLSCert))
		fmt.Fprintf(&b, "  keyFile: %s\n\n", strconv.Quote(a.TLSKey))
	}
	if len(a.Proxies) > 0 {
		fmt.Fprintf(&b, "trustedProxies: [%s]\n\n", strings.Join(a.Proxies, ", "))
	}
	b.WriteString("docker:\n")
	fmt.Fprintf(&b, "  enabled: %t\n", a.Docker)
	fmt.Fprintf(&b, "  socket: %s\n\n", strconv.Quote(a.DockerSocket))
	b.WriteString("discovery:\n")
	fmt.Fprintf(&b, "  nmap: %t\n", a.Nmap)
	return []byte(b.String())
}

// the three strings of the unit that depend on where the install lives.
const (
	unitExec  = "ExecStart=/usr/local/bin/netscribe serve --config /etc/netscribe/netscribe.yaml"
	unitState = "ReadWritePaths=/var/lib/netscribe"
	unitUser  = "User=netscribe\nGroup=netscribe"
)

// RenderUnit returns the systemd unit for this install.
func RenderUnit(o Options) (string, error) {
	for _, want := range []string{unitExec, unitState, unitUser} {
		if !strings.Contains(unitTemplate, want) {
			return "", fmt.Errorf("the unit template has no line %q", strings.SplitN(want, "\n", 2)[0])
		}
	}
	u := strings.Replace(unitTemplate, unitExec, fmt.Sprintf("ExecStart=%s serve --config %s", o.BinPath, o.ConfigPath), 1)
	u = strings.Replace(u, unitState, "ReadWritePaths="+o.StateDir, 1)
	u = strings.Replace(u, unitUser, fmt.Sprintf("User=%s\nGroup=%s", o.ServiceUser, o.ServiceUser), 1)
	return u, nil
}

// RenderDropIn returns a systemd drop-in with the rights the chosen features need, or
// an empty string when none is needed.
func RenderDropIn(group string, nmap bool) string {
	var lines []string
	if group != "" {
		lines = append(lines, "# Read the container data from the Docker socket.", "SupplementaryGroups="+group)
	}
	if nmap {
		lines = append(lines,
			"# Nmap needs raw sockets for the operating system detection.",
			"AmbientCapabilities=CAP_NET_RAW CAP_NET_ADMIN",
			"CapabilityBoundingSet=CAP_NET_RAW CAP_NET_ADMIN")
	}
	if len(lines) == 0 {
		return ""
	}
	return "# Written by \"netscribe setup\".\n[Service]\n" + strings.Join(lines, "\n") + "\n"
}

// Candidate is a network the setup proposes as a scan target.
type Candidate struct {
	Prefix netip.Prefix
	Iface  string
	Note   string
}

// containerIfaces are the name prefixes of virtual interfaces that belong to containers
// and virtual machines. Their networks are not the LAN to document.
var containerIfaces = []string{"docker", "br-", "veth", "virbr", "cni", "flannel", "cali", "kube", "lxc", "podman"}

func isContainerIface(name string) bool {
	for _, p := range containerIfaces {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// Candidates lists the private IPv4 networks that the host is connected to. A network
// wider than a /24 is narrowed to the /24 of the address, because a scan of 65 534
// addresses is a decision to make on purpose.
func Candidates(ifaces []NetIface) []Candidate {
	var out []Candidate
	seen := map[netip.Prefix]bool{}
	for _, ifc := range ifaces {
		if ifc.Loopback || !ifc.Up || isContainerIface(ifc.Name) {
			continue
		}
		for _, a := range ifc.Addrs {
			if !a.Addr().Is4() || a.Bits() > 30 {
				continue
			}
			p, note := a.Masked(), ""
			if a.Bits() < 24 {
				p = netip.PrefixFrom(a.Addr(), 24).Masked()
				note = "the network is " + a.Masked().String() + ", narrowed to a /24"
			}
			if _, err := config.ParseTarget(p.String(), false); err != nil || seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, Candidate{Prefix: p, Iface: ifc.Name, Note: note})
		}
	}
	return out
}

// overlaps reports whether two prefixes share addresses.
func overlaps(a, b netip.Prefix) bool { return a.Contains(b.Addr()) || b.Contains(a.Addr()) }
