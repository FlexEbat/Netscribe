package setup

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/netip"

	"github.com/FlexEbat/Netscribe/internal/config"
)

const defaultListen = "127.0.0.1:8080"

// listenChoice is where the panel listens and how the traffic to it is protected.
type listenChoice struct {
	Listen   string
	TLSCert  string
	TLSKey   string
	Proxies  []string
	PlainTCP bool // the person accepted plain HTTP beyond loopback
}

func portOf(addr string) string {
	if _, port, err := net.SplitHostPort(addr); err == nil && port != "" {
		return port
	}
	return "8080"
}

// normalizeProxy turns the address or the network of a reverse proxy into the CIDR form
// that the config requires. An address alone means that one host.
func normalizeProxy(v string) (string, error) {
	if pfx, err := netip.ParsePrefix(v); err == nil {
		return pfx.String(), nil
	}
	if addr, err := netip.ParseAddr(v); err == nil {
		return netip.PrefixFrom(addr, addr.BitLen()).String(), nil
	}
	return "", errors.New("use an address such as 10.0.0.2 or a network such as 10.0.0.0/24")
}

// normalizeProxies applies normalizeProxy to a list.
func normalizeProxies(list []string) ([]string, error) {
	out := make([]string, 0, len(list))
	for _, v := range list {
		n, err := normalizeProxy(v)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", v, err)
		}
		out = append(out, n)
	}
	return out, nil
}

// pickListen asks where the panel should listen. holdsPort says that the service already
// holds current, so a bind test of it would only find the service itself.
func (s *session) pickListen(current string, holdsPort bool) (listenChoice, error) {
	port := portOf(current)
	if s.o.Yes {
		return s.listenFromFlags(current)
	}

	type option struct{ label, addr string }
	opts := []option{{"127.0.0.1:" + port + "   this host only (recommended): reach it with ssh -L or a TLS reverse proxy", "127.0.0.1:" + port}}
	if ifaces, err := s.env.Interfaces(); err == nil {
		for _, ifc := range ifaces {
			if ifc.Loopback || !ifc.Up || isContainerIface(ifc.Name) {
				continue
			}
			for _, a := range ifc.Addrs {
				if a.Addr().Is4() {
					addr := net.JoinHostPort(a.Addr().String(), port)
					opts = append(opts, option{addr + "   on " + ifc.Name + ", open to that network", addr})
				}
			}
		}
	}
	opts = append(opts, option{"0.0.0.0:" + port + "   every interface, open to every network of this host", "0.0.0.0:" + port},
		option{"another address", ""})

	def := 0
	for i, o := range opts {
		if o.addr == current {
			def = i
		}
	}
	labels := make([]string, len(opts))
	for i, o := range opts {
		labels[i] = o.label
	}
	i, err := s.p.pick("Where should the panel listen?", labels, def)
	if err != nil {
		return listenChoice{}, err
	}
	addr := opts[i].addr
	check := func(v string) error {
		if err := config.ValidateListen(v); err != nil {
			return err
		}
		if holdsPort && v == current {
			return nil
		}
		if err := s.env.CanListen(v); err != nil {
			return fmt.Errorf("cannot bind %s: %w", v, err)
		}
		return nil
	}
	if addr == "" {
		if addr, err = s.p.ask("Listen address, host:port", current, check); err != nil {
			return listenChoice{}, err
		}
	} else if err := check(addr); err != nil {
		s.ui.printf("  %s %v\n", s.ui.paint("33", "!"), err)
		if addr, err = s.p.ask("Another listen address, host:port", addr, check); err != nil {
			return listenChoice{}, err
		}
	}
	host, _, _ := net.SplitHostPort(addr)
	if isLoopback(host) {
		return listenChoice{Listen: addr}, nil
	}
	return s.protect(addr)
}

// protect asks how the traffic to an address beyond loopback is encrypted.
func (s *session) protect(addr string) (listenChoice, error) {
	s.ui.note("%s is open to the network: passwords and session cookies need TLS on the way.", addr)
	i, err := s.p.pick("How is the traffic protected?", []string{
		"A TLS reverse proxy (nginx, Caddy, Traefik) on another host forwards to the panel",
		"The panel serves TLS itself, with a certificate and a key",
		"Not at all: plain HTTP (not recommended)",
	}, 1)
	if err != nil {
		return listenChoice{}, err
	}
	switch i {
	case 0:
		list, err := s.p.ask("Address or network of the proxy, separated by commas", "", func(v string) error {
			if len(splitList(v)) == 0 {
				return errors.New("name at least one proxy")
			}
			_, err := normalizeProxies(splitList(v))
			return err
		})
		if err != nil {
			return listenChoice{}, err
		}
		proxies, err := normalizeProxies(splitList(list))
		if err != nil {
			return listenChoice{}, err
		}
		return listenChoice{Listen: addr, Proxies: proxies}, nil
	case 1:
		cert, err := s.p.ask("Path of the certificate (PEM)", "", nil)
		if err != nil {
			return listenChoice{}, err
		}
		key, err := s.p.ask("Path of the private key (PEM)", "", func(v string) error { return s.checkKeyPair(cert, v) })
		if err != nil {
			return listenChoice{}, err
		}
		s.ui.note("the service account %s must be able to read both files", s.o.ServiceUser)
		return listenChoice{Listen: addr, TLSCert: cert, TLSKey: key}, nil
	}
	ok, err := s.p.confirm("Use plain HTTP on "+addr+"?", false)
	if err != nil {
		return listenChoice{}, err
	}
	if !ok {
		return s.protect(addr)
	}
	return listenChoice{Listen: addr, PlainTCP: true}, nil
}

func (s *session) checkKeyPair(certPath, keyPath string) error {
	cert, err := s.env.ReadFile(certPath)
	if err != nil {
		return fmt.Errorf("read the certificate: %w", err)
	}
	key, err := s.env.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("read the key: %w", err)
	}
	if _, err := tls.X509KeyPair(cert, key); err != nil {
		return fmt.Errorf("the certificate and the key do not belong together: %w", err)
	}
	return nil
}

// listenFromFlags takes the address and the protection from the options, without asking.
func (s *session) listenFromFlags(current string) (listenChoice, error) {
	addr := s.o.Listen
	if addr == "" {
		addr = current
	}
	if addr == "" {
		addr = defaultListen
	}
	if err := config.ValidateListen(addr); err != nil {
		return listenChoice{}, fmt.Errorf("--listen %s: %w", addr, err)
	}
	if err := s.env.CanListen(addr); err != nil {
		return listenChoice{}, fmt.Errorf("the address %s is not usable: %w", addr, err)
	}
	s.ui.printf("  %s Listen address %s\n", s.ui.paint("36", "?"), s.ui.dim("→ "+addr))
	host, _, _ := net.SplitHostPort(addr)
	if isLoopback(host) {
		return listenChoice{Listen: addr}, nil
	}
	switch {
	case s.o.TLSCert != "" || s.o.TLSKey != "":
		if err := s.checkKeyPair(s.o.TLSCert, s.o.TLSKey); err != nil {
			return listenChoice{}, err
		}
		return listenChoice{Listen: addr, TLSCert: s.o.TLSCert, TLSKey: s.o.TLSKey}, nil
	case len(s.o.TrustedProxies) > 0:
		proxies, err := normalizeProxies(s.o.TrustedProxies)
		if err != nil {
			return listenChoice{}, fmt.Errorf("--trusted-proxy %w", err)
		}
		return listenChoice{Listen: addr, Proxies: proxies}, nil
	case s.o.PlainHTTP:
		return listenChoice{Listen: addr, PlainTCP: true}, nil
	}
	return listenChoice{}, fmt.Errorf("%s is open to the network without TLS: choose a loopback address, or pass --tls-cert and --tls-key, --trusted-proxy, or --plain-http", addr)
}
