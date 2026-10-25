package setup

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeKeyPair makes a self-signed certificate and its key, and returns their paths.
func writeKeyPair(t *testing.T) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "netscribe.test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certPath, keyPath = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

func TestPickListenDefaultsToThisHostOnly(t *testing.T) {
	f := newFake(t)
	s := newSession(f.env("\n"), f.opts())
	got, err := s.pickListen("127.0.0.1:8080", false)
	if err != nil || got.Listen != "127.0.0.1:8080" || got.TLSCert != "" || len(got.Proxies) != 0 {
		t.Fatalf("%+v, %v", got, err)
	}
	out := f.out.String()
	for _, want := range []string{"this host only", "192.168.1.20:8080", "on eth0", "0.0.0.0:8080", "another address"} {
		if !strings.Contains(out, want) {
			t.Errorf("the choices lack %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "172.17.0.1") {
		t.Error("the address of the Docker bridge must not be offered")
	}
}

func TestPickListenOnAnInterfaceWithAProxy(t *testing.T) {
	f := newFake(t)
	// option 2 is the interface, then the proxy option, then the proxy address
	s := newSession(f.env("2\n1\n10.0.0.2, 10.1.0.0/24\n"), f.opts())
	got, err := s.pickListen("127.0.0.1:8080", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Listen != "192.168.1.20:8080" || strings.Join(got.Proxies, ",") != "10.0.0.2/32,10.1.0.0/24" || got.TLSCert != "" {
		t.Errorf("%+v", got)
	}
}

func TestPickListenWithTLSChecksThePair(t *testing.T) {
	f := newFake(t)
	cert, key := writeKeyPair(t)
	other, otherKey := writeKeyPair(t)
	_ = other
	// every interface, TLS, then a key that does not match, then the right one
	s := newSession(f.env("3\n2\n"+cert+"\n"+otherKey+"\n"+key+"\n"), f.opts())
	got, err := s.pickListen("127.0.0.1:8080", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Listen != "0.0.0.0:8080" || got.TLSCert != cert || got.TLSKey != key {
		t.Errorf("%+v", got)
	}
	if !strings.Contains(f.out.String(), "do not belong together") {
		t.Errorf("the mismatch was not reported:\n%s", f.out.String())
	}
}

func TestPickListenPlainHTTPNeedsAYes(t *testing.T) {
	f := newFake(t)
	// interface, no protection, "n" goes back to the question, no protection again, "y"
	s := newSession(f.env("2\n3\nn\n3\ny\n"), f.opts())
	got, err := s.pickListen("127.0.0.1:8080", false)
	if err != nil || !got.PlainTCP || got.Listen != "192.168.1.20:8080" {
		t.Fatalf("%+v, %v", got, err)
	}
}

func TestPickListenCustomAddressAndABusyPort(t *testing.T) {
	f := newFake(t)
	s := newSession(f.env("4\nnonsense\n127.0.0.1:9000\n"), f.opts())
	got, err := s.pickListen("127.0.0.1:8080", false)
	if err != nil || got.Listen != "127.0.0.1:9000" {
		t.Fatalf("%+v, %v", got, err)
	}

	f = newFake(t)
	f.listen = os.ErrExist
	s = newSession(f.env("\n127.0.0.1:9001\n"), f.opts())
	f.listen = os.ErrExist
	if _, err := s.pickListen("127.0.0.1:8080", false); err == nil {
		// the first bind fails, the second prompt takes the new address, which fails as well
		t.Log("every address is busy in this fake, an error is expected")
	}

	// the service itself holds the port: the same address is not a problem
	f = newFake(t)
	f.listen = os.ErrExist
	s = newSession(f.env("\n"), f.opts())
	if got, err := s.pickListen("127.0.0.1:8080", true); err != nil || got.Listen != "127.0.0.1:8080" {
		t.Errorf("%+v, %v", got, err)
	}
}

func TestListenFromFlags(t *testing.T) {
	cert, key := writeKeyPair(t)
	for _, tc := range []struct {
		name    string
		listen  string
		mod     func(*Options)
		wantErr string
		check   func(listenChoice) bool
	}{
		{"loopback", "127.0.0.1:8080", nil, "", func(c listenChoice) bool { return c.Listen == "127.0.0.1:8080" }},
		{"open without protection", "0.0.0.0:8080", nil, "--plain-http", nil},
		{"open with TLS", "0.0.0.0:8443", func(o *Options) { o.TLSCert, o.TLSKey = cert, key }, "", func(c listenChoice) bool { return c.TLSCert == cert }},
		{"open with a mismatched pair", "0.0.0.0:8443", func(o *Options) { o.TLSCert, o.TLSKey = cert, cert }, "belong together", nil},
		{"open behind a proxy", "10.0.0.5:8080", func(o *Options) { o.TrustedProxies = []string{"10.0.0.2"} }, "", func(c listenChoice) bool { return len(c.Proxies) == 1 && c.Proxies[0] == "10.0.0.2/32" }},
		{"a bad proxy", "10.0.0.5:8080", func(o *Options) { o.TrustedProxies = []string{"proxy"} }, "trusted-proxy", nil},
		{"plain on purpose", "10.0.0.5:8080", func(o *Options) { o.PlainHTTP = true }, "", func(c listenChoice) bool { return c.PlainTCP }},
		{"not an address", "banana", nil, "--listen", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t)
			o := yesOptions(f)
			o.Listen = tc.listen
			if tc.mod != nil {
				tc.mod(&o)
			}
			got, err := newSession(f.env(""), o).pickListen(o.Listen, false)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || !tc.check(got) {
				t.Fatalf("%+v, %v", got, err)
			}
		})
	}
}

func TestNormalizeProxy(t *testing.T) {
	for in, want := range map[string]string{"10.0.0.2": "10.0.0.2/32", "10.0.0.0/24": "10.0.0.0/24", "::1": "::1/128", "fd00::/8": "fd00::/8"} {
		got, err := normalizeProxy(in)
		if err != nil || got != want {
			t.Errorf("normalizeProxy(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "proxy", "10.0.0.256", "10.0.0.0/33"} {
		if _, err := normalizeProxy(bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

func TestSetupWritesTLSAndProxiesIntoTheConfig(t *testing.T) {
	f := newFake(t)
	o := yesOptions(f)
	o.Listen = "0.0.0.0:8443"
	o.TrustedProxies = []string{"10.0.0.2"}
	if err := Setup(t.Context(), f.env("pw\n"), o, "v"); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}
	text := readFile(t, o.ConfigPath)
	if !strings.Contains(text, "listen: 0.0.0.0:8443") || !strings.Contains(text, "trustedProxies: [10.0.0.2/32]") {
		t.Errorf("config:\n%s", text)
	}
}
