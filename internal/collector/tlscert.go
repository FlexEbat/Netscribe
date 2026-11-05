package collector

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/FlexEbat/Netscribe/internal/model"
)

const (
	tlsTimeout     = 3 * time.Second // the connect and the handshake together
	tlsConcurrency = 32
	maxCertName    = 200 // characters of one name that are kept
	maxCertSANs    = 5   // alternative names that are kept
)

// tlsPorts are the ports that speak TLS from the first byte. A service named https is the
// other way in.
var tlsPorts = map[int]bool{443: true, 465: true, 636: true, 993: true, 995: true, 8443: true}

// isTLSPort says whether a port speaks TLS from its first byte.
func isTLSPort(port int) bool { return tlsPorts[port] || portNameOf(port) == "https" }

// TLSCerts reads the certificate of every open TLS port of a live device: it connects,
// completes the handshake, keeps the certificate the device presented and closes. It sends
// no application data and does not verify the certificate. The point is to read it, and the
// certificate of a device is often expired, self signed or issued for another name; those
// are the findings, not reasons to stay silent.
type TLSCerts struct {
	log   *slog.Logger
	ports func(ip string) []int // open TCP ports of an address
	dial  func(ctx context.Context, network, addr string) (net.Conn, error)
	isTLS func(port int) bool // the ports that get a handshake, replaced in tests

	mu    sync.Mutex
	facts map[string][]string // ip -> subject, names and issuer of its certificates, lower case
}

// NewTLSCerts returns a collector that asks the ports that ports reports as open.
func NewTLSCerts(log *slog.Logger, ports func(ip string) []int) *TLSCerts {
	d := &net.Dialer{Timeout: tlsTimeout}
	return &TLSCerts{log: log, ports: ports, dial: d.DialContext, isTLS: isTLSPort, facts: map[string][]string{}}
}

func (c *TLSCerts) Name() string { return "tlscerts" }

// certFacts returns what the certificates of ip said about the device, for the classifier.
func (c *TLSCerts) certFacts(ip string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.facts[ip]
}

type certJob struct {
	dev      model.Device
	ip, own  string
	port     int
	deviceIP string
}

// Collect reads the certificates. It does nothing when discovery.tlsCerts is off.
func (c *TLSCerts) Collect(ctx context.Context, in Input) (Result, error) {
	if !in.Config.Discovery.TLSCertsEnabled() {
		return Result{}, nil
	}
	var jobs []certJob
	for _, d := range in.Devices {
		for _, a := range []struct{ ip, own string }{{d.IP, ""}, {d.IP6, d.IP6}} {
			if a.ip == "" {
				continue
			}
			for _, port := range c.ports(a.ip) {
				if c.isTLS(port) {
					jobs = append(jobs, certJob{dev: d, ip: a.ip, own: a.own, port: port})
				}
			}
		}
	}
	var (
		res   Result
		mu    sync.Mutex
		wg    sync.WaitGroup
		facts = map[string][]string{}
		sem   = make(chan struct{}, tlsConcurrency)
	)
loop:
	for _, j := range jobs {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break loop
		}
		wg.Add(1)
		go func(j certJob) {
			defer wg.Done()
			defer func() { <-sem }()
			cert, err := c.read(ctx, j.ip, j.port)
			if err != nil {
				if ctx.Err() == nil {
					c.log.Debug("certificate not read", "addr", net.JoinHostPort(j.ip, strconv.Itoa(j.port)), "err", err)
				}
				return // a stored certificate stays until a handshake gives another
			}
			mu.Lock()
			defer mu.Unlock()
			res.Services = append(res.Services, model.ServiceInput{
				Device: model.DeviceKey{MAC: j.dev.MAC, IP: j.dev.IP}, IP: j.own, Port: j.port, Proto: "tcp",
				Name: portNameOf(j.port), TLS: cert,
			})
			facts[j.ip] = append(facts[j.ip], strings.ToLower(cert.Subject), strings.ToLower(strings.Join(cert.SANs, " ")), strings.ToLower(cert.Issuer))
		}(j)
	}
	wg.Wait()
	sort.Slice(res.Services, func(i, j int) bool {
		a, b := res.Services[i], res.Services[j]
		if a.Device != b.Device {
			return a.Device.MAC+a.Device.IP < b.Device.MAC+b.Device.IP
		}
		if a.Port != b.Port {
			return a.Port < b.Port
		}
		return a.IP < b.IP
	})
	c.mu.Lock()
	c.facts = facts
	c.mu.Unlock()
	return res, ctx.Err()
}

// read connects to ip:port, completes a TLS handshake, returns the first certificate the
// peer sent and closes the connection.
func (c *TLSCerts) read(ctx context.Context, ip string, port int) (*model.TLSCert, error) {
	ctx, cancel := context.WithTimeout(ctx, tlsTimeout)
	defer cancel()
	conn, err := c.dial(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }() // nothing was written but the handshake
	// The certificate is read and never trusted: devices present expired, self signed and
	// misnamed ones, and that is exactly what this reads. Nothing is sent after the
	// handshake. Old protocol versions stay allowed, because old devices speak nothing else.
	cfg := &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS10} //nolint:gosec // see above
	tc := tls.Client(conn, cfg)
	if err := tc.HandshakeContext(ctx); err != nil {
		return nil, err
	}
	peers := tc.ConnectionState().PeerCertificates
	if len(peers) == 0 {
		return nil, errors.New("the peer sent no certificate")
	}
	return certFromX509(peers[0]), nil
}

// certFromX509 turns a certificate into the stored form: names without control characters and
// at most 200 characters, the first 5 alternative names, the dates in UTC and the SHA-256
// fingerprint of the DER bytes.
func certFromX509(x *x509.Certificate) *model.TLSCert {
	sum := sha256.Sum256(x.Raw)
	names := make([]string, 0, maxCertSANs)
	add := func(s string) {
		if s = cleanCertName(s); s != "" && len(names) < maxCertSANs {
			names = append(names, s)
		}
	}
	for _, s := range x.DNSNames {
		add(s)
	}
	for _, ip := range x.IPAddresses {
		add(ip.String())
	}
	return &model.TLSCert{
		Subject:     cleanCertName(x.Subject.CommonName),
		SANs:        names,
		Issuer:      cleanCertName(x.Issuer.CommonName),
		NotBefore:   x.NotBefore.UTC(),
		NotAfter:    x.NotAfter.UTC(),
		Fingerprint: hex.EncodeToString(sum[:]),
	}
}

// cleanCertName drops control and format characters and cuts the name to 200 characters on a
// character boundary. The text comes from the network.
func cleanCertName(s string) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if model.UnsafeRune(r) {
			return -1
		}
		return r
	}, s))
	if utf8.RuneCountInString(s) > maxCertName {
		s = string([]rune(s)[:maxCertName])
	}
	return s
}
