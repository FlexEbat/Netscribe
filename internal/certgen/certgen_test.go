package certgen

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"strings"
	"testing"
	"time"
)

var fixedNow = time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)

func parse(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()
	b, _ := pem.Decode(certPEM)
	if b == nil {
		t.Fatal("no PEM block")
	}
	c, err := x509.ParseCertificate(b.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestGenerateVerifiesForEveryName(t *testing.T) {
	names := []string{"netscribe.lan", "*.lab.example", "192.168.1.10", "::1"}
	certPEM, keyPEM, err := Generate(Options{Names: names, Days: 30, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	c := parse(t, certPEM)
	pool := x509.NewCertPool()
	pool.AddCert(c)
	for _, host := range []string{"netscribe.lan", "x.lab.example", "192.168.1.10", "::1"} {
		_, err := c.Verify(x509.VerifyOptions{
			DNSName:     host,
			Roots:       pool,
			CurrentTime: fixedNow,
			KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		})
		if err != nil {
			t.Errorf("verify %s: %v", host, err)
		}
	}
	if c.IsCA || !c.BasicConstraintsValid {
		t.Error("certificate must be a leaf with CA:false")
	}
	if c.KeyUsage != x509.KeyUsageDigitalSignature {
		t.Errorf("key usage = %v", c.KeyUsage)
	}
	if len(c.ExtKeyUsage) != 1 || c.ExtKeyUsage[0] != x509.ExtKeyUsageServerAuth {
		t.Errorf("ext key usage = %v", c.ExtKeyUsage)
	}
	if c.Subject.CommonName != "netscribe.lan" {
		t.Errorf("common name = %q", c.Subject.CommonName)
	}
	if want := fixedNow.Add(-5 * time.Minute); !c.NotBefore.Equal(want) {
		t.Errorf("NotBefore = %v, want %v", c.NotBefore, want)
	}
	if want := fixedNow.Add(30 * 24 * time.Hour); !c.NotAfter.Equal(want) {
		t.Errorf("NotAfter = %v, want %v", c.NotAfter, want)
	}
	if c.SignatureAlgorithm != x509.ECDSAWithSHA256 {
		t.Errorf("signature = %v", c.SignatureAlgorithm)
	}
	if !strings.HasPrefix(string(keyPEM), "-----BEGIN PRIVATE KEY-----") {
		t.Error("key is not PKCS 8 PEM")
	}
}

func TestVerifyFailsForAnOtherName(t *testing.T) {
	certPEM, _, err := Generate(Options{Names: []string{"a.lan"}, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	c := parse(t, certPEM)
	pool := x509.NewCertPool()
	pool.AddCert(c)
	if _, err := c.Verify(x509.VerifyOptions{DNSName: "b.lan", Roots: pool, CurrentTime: fixedNow}); err == nil {
		t.Error("a certificate for a.lan must not verify b.lan")
	}
}

func TestGenerateRefusals(t *testing.T) {
	tooMany := make([]string, MaxNames+1)
	for i := range tooMany {
		tooMany[i] = "h" + string(rune('a'+i)) + ".lan"
	}
	cases := []struct {
		name string
		opts Options
		want error
	}{
		{"826 days", Options{Names: []string{"a.lan"}, Days: 826}, ErrBadDays},
		{"negative days", Options{Names: []string{"a.lan"}, Days: -1}, ErrBadDays},
		{"21 names", Options{Names: tooMany}, ErrTooMany},
		{"bad name", Options{Names: []string{"bad_name!"}}, ErrBadName},
		{"no names", Options{}, ErrNoNames},
		{"only blanks", Options{Names: []string{" ", ""}}, ErrNoNames},
		{"bare wildcard", Options{Names: []string{"*."}}, ErrBadName},
		{"leading hyphen", Options{Names: []string{"-a.lan"}}, ErrBadName},
		{"empty label", Options{Names: []string{"a..lan"}}, ErrBadName},
		{"too long", Options{Names: []string{strings.Repeat("a", 254)}}, errNameLong},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cert, key, err := Generate(tc.opts)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if cert != nil || key != nil {
				t.Error("a refusal must return no material")
			}
		})
	}
}

func TestGenerateAcceptsTheLimits(t *testing.T) {
	if _, _, err := Generate(Options{Names: []string{"a.lan"}, Days: MaxDays, Now: fixedNow}); err != nil {
		t.Errorf("825 days: %v", err)
	}
	many := make([]string, MaxNames)
	for i := range many {
		many[i] = "h" + strings.Repeat("x", i) + ".lan"
	}
	if _, _, err := Generate(Options{Names: many, Now: fixedNow}); err != nil {
		t.Errorf("20 names: %v", err)
	}
}

func TestDefaultDays(t *testing.T) {
	certPEM, _, err := Generate(Options{Names: []string{"a.lan"}, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	c := parse(t, certPEM)
	if want := fixedNow.Add(DefaultDays * 24 * time.Hour); !c.NotAfter.Equal(want) {
		t.Errorf("NotAfter = %v, want %v", c.NotAfter, want)
	}
}

func TestNormalizeNames(t *testing.T) {
	got, err := NormalizeNames([]string{" Host.LAN ", "host.lan", "10.0.0.1", "10.0.0.1", "::1", "0:0:0:0:0:0:0:1"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"host.lan", "10.0.0.1", "::1"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSerialsDiffer(t *testing.T) {
	a, _, _ := Generate(Options{Names: []string{"a.lan"}, Now: fixedNow})
	b, _, _ := Generate(Options{Names: []string{"a.lan"}, Now: fixedNow})
	if parse(t, a).SerialNumber.Cmp(parse(t, b).SerialNumber) == 0 {
		t.Error("two certificates share a serial")
	}
}

func TestDescribe(t *testing.T) {
	certPEM, _, err := Generate(Options{Names: []string{"a.lan", "10.0.0.5"}, Days: 10, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	info, err := Describe(certPEM)
	if err != nil {
		t.Fatal(err)
	}
	if info.CommonName != "a.lan" {
		t.Errorf("common name = %q", info.CommonName)
	}
	if !info.SelfSigned {
		t.Error("a generated certificate is self-signed")
	}
	if strings.Join(info.Names, ",") != "a.lan,10.0.0.5" {
		t.Errorf("names = %v", info.Names)
	}
	if len(info.Fingerprint) != 32*3-1 || strings.ToUpper(info.Fingerprint) != info.Fingerprint {
		t.Errorf("fingerprint = %q", info.Fingerprint)
	}
	if _, err := Describe([]byte("not pem")); err == nil {
		t.Error("garbage must be refused")
	}
}
