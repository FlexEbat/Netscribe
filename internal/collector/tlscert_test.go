package collector

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
)

func selfSigned(t *testing.T, tmpl *x509.Certificate) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.SerialNumber == nil {
		tmpl.SerialNumber = big.NewInt(1)
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// tlsServer answers handshakes and records every byte a client sends after them.
type tlsServer struct {
	ln       net.Listener
	appBytes atomic.Int64
	accepted atomic.Int64
	wg       sync.WaitGroup
}

func startTLSServer(t *testing.T, cert tls.Certificate) *tlsServer {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	s := &tlsServer{ln: ln}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			s.accepted.Add(1)
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				defer func() { _ = conn.Close() }()
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				n, _ := io.Copy(io.Discard, conn) // the handshake runs inside the first read
				s.appBytes.Add(n)
			}()
		}
	}()
	t.Cleanup(func() { _ = ln.Close(); s.wg.Wait() })
	return s
}

func (s *tlsServer) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func certCollector(port int) *TLSCerts {
	c := NewTLSCerts(slog.New(slog.NewTextHandler(io.Discard, nil)), func(string) []int { return []int{port} })
	c.isTLS = func(p int) bool { return p == port }
	return c
}

func certInput() Input {
	return Input{Devices: []model.Device{{MAC: "aa:bb:cc:00:00:01", IP: "127.0.0.1"}}}
}

func TestTLSCertsReadsAnExpiredSelfSignedCertificateAndSendsNothing(t *testing.T) {
	cert := selfSigned(t, &x509.Certificate{
		Subject:     pkix.Name{CommonName: "printer.lan"},
		DNSNames:    []string{"printer.lan", "printer"},
		NotBefore:   time.Now().Add(-48 * time.Hour),
		NotAfter:    time.Now().Add(-24 * time.Hour), // expired
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	srv := startTLSServer(t, cert)
	c := certCollector(srv.port())

	res, err := c.Collect(context.Background(), certInput())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Services) != 1 {
		t.Fatalf("services = %+v, want the one TLS service", res.Services)
	}
	got := res.Services[0]
	if got.Port != srv.port() || got.TLS == nil || got.TLS.Subject != "printer.lan" || got.TLS.Issuer != "printer.lan" {
		t.Errorf("service = %+v, want the certificate of printer.lan", got)
	}
	if !got.TLS.NotAfter.Before(time.Now()) {
		t.Errorf("not after = %v, want the expired date to be kept as it is", got.TLS.NotAfter)
	}
	if len(got.TLS.SANs) != 2 || len(got.TLS.Fingerprint) != 64 {
		t.Errorf("certificate = %+v, want 2 names and a 64 character fingerprint", got.TLS)
	}
	if facts := c.certFacts("127.0.0.1"); !strings.Contains(strings.Join(facts, " "), "printer.lan") {
		t.Errorf("facts = %v, want the subject for the classifier", facts)
	}
}

func TestTLSCertsSendsNoApplicationData(t *testing.T) {
	cert := selfSigned(t, &x509.Certificate{Subject: pkix.Name{CommonName: "x"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)})
	srv := startTLSServer(t, cert)
	c := certCollector(srv.port())
	if _, err := c.Collect(context.Background(), certInput()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for srv.accepted.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	// The read ends when the client closes; wait for that, then count.
	_ = srv.ln.Close()
	srv.wg.Wait()
	if n := srv.appBytes.Load(); n != 0 {
		t.Errorf("the server received %d bytes of application data, want none", n)
	}
}

func TestCertFromX509NormalizesAndCaps(t *testing.T) {
	var dns []string
	for i := 0; i < 100; i++ {
		dns = append(dns, fmt.Sprintf("host%03d.example.org", i))
	}
	cert := selfSigned(t, &x509.Certificate{
		Subject:   pkix.Name{CommonName: "evil\x07\x1bname\u202e" + strings.Repeat("a", 10<<10)},
		DNSNames:  dns,
		NotBefore: time.Now().Add(-time.Hour),
		NotAfter:  time.Now().Add(time.Hour),
	})
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	got := certFromX509(parsed)
	if strings.ContainsAny(got.Subject, "\x07\x1b\u202e") {
		t.Errorf("subject %q still holds control characters", got.Subject[:20])
	}
	if utf8.RuneCountInString(got.Subject) != maxCertName {
		t.Errorf("subject has %d characters, want it cut to %d", utf8.RuneCountInString(got.Subject), maxCertName)
	}
	if len(got.SANs) != maxCertSANs || got.SANs[0] != "host000.example.org" {
		t.Errorf("names = %v, want the first %d", got.SANs, maxCertSANs)
	}
	if got.NotBefore.Location() != time.UTC || got.NotAfter.Location() != time.UTC {
		t.Errorf("dates are not in UTC: %v %v", got.NotBefore.Location(), got.NotAfter.Location())
	}
}

func TestTLSCertsIgnoresAPortThatIsNotTLS(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_, _ = conn.Write([]byte("SSH-2.0-not-tls\r\n"))
			_ = conn.Close()
		}
	}()
	c := certCollector(ln.Addr().(*net.TCPAddr).Port)
	res, err := c.Collect(context.Background(), certInput())
	if err != nil || len(res.Services) != 0 {
		t.Errorf("res = %+v, err = %v, want nothing from a port that does not speak TLS", res, err)
	}
}

func TestTLSCertsIsOffWhenTheConfigSaysSo(t *testing.T) {
	cert := selfSigned(t, &x509.Certificate{Subject: pkix.Name{CommonName: "x"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)})
	srv := startTLSServer(t, cert)
	c := certCollector(srv.port())
	in := certInput()
	off := false
	in.Config = config.Config{Discovery: config.DiscoveryConfig{TLSCerts: &off}}
	res, err := c.Collect(context.Background(), in)
	if err != nil || len(res.Services) != 0 || srv.accepted.Load() != 0 {
		t.Errorf("res = %+v, err = %v, connections = %d, want no handshake at all", res, err, srv.accepted.Load())
	}
}

func TestTLSPorts(t *testing.T) {
	for _, p := range []int{443, 465, 636, 993, 995, 8443} {
		if !isTLSPort(p) {
			t.Errorf("port %d is not read, want it read", p)
		}
	}
	for _, p := range []int{22, 80, 8080, 3389} {
		if isTLSPort(p) {
			t.Errorf("port %d is read, want it left alone", p)
		}
	}
}

func TestMergeServicesKeepsTheCertificateAndTheAddress(t *testing.T) {
	dev := model.DeviceKey{MAC: "aa:bb:cc:00:00:01", IP: "10.0.0.1"}
	cert := &model.TLSCert{Subject: "x", Fingerprint: strings.Repeat("ab", 32)}
	base := []model.ServiceInput{{Device: dev, Port: 443, Proto: "tcp", Name: "https", Banner: "nginx"}}
	later := []model.ServiceInput{
		{Device: dev, Port: 443, Proto: "tcp", Name: "https", TLS: cert},
		{Device: dev, IP: "fd00::1", Port: 443, Proto: "tcp", Name: "https"},
	}
	got := mergeServices(base, later)
	if len(got) != 2 || got[0].Banner != "nginx" || got[0].TLS != cert || got[1].IP != "fd00::1" {
		t.Errorf("merged = %+v, want one entry with banner and certificate, and the IPv6 one apart", got)
	}
}
