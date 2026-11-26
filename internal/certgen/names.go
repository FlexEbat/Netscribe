package certgen

import (
	"net"
	"os"
)

// DefaultNames are the names a certificate gets when the person names none: the host name, the
// loopback names and the addresses the panel listens on. listen is the listen address of the
// config; an empty or unspecified host stands for every interface of the host.
func DefaultNames(listen string) []string {
	var raw []string
	if h, err := os.Hostname(); err == nil {
		raw = append(raw, h)
	}
	raw = append(raw, "localhost", "127.0.0.1", "::1")
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		host = ""
	}
	ip := net.ParseIP(host)
	switch {
	case ip != nil && !ip.IsUnspecified():
		raw = append(raw, ip.String())
	case host == "" || ip != nil:
		// No interface is also fine: the loopback names stay.
		addrs, _ := net.InterfaceAddrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && !n.IP.IsLinkLocalUnicast() {
				raw = append(raw, n.IP.String())
			}
		}
	default:
		raw = append(raw, host)
	}
	seen := make(map[string]bool, len(raw))
	var out []string
	for _, r := range raw {
		norm, err := NormalizeNames([]string{r})
		if err != nil || len(norm) == 0 || seen[norm[0]] {
			continue // a host name the certificate cannot carry, or a repeat
		}
		seen[norm[0]] = true
		out = append(out, norm[0])
		if len(out) == MaxNames {
			break
		}
	}
	return out
}
