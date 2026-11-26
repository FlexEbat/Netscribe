// Package certgen makes the self-signed certificate of slice 47. The certificate is a leaf
// signed by its own key: no certificate authority is made and no root is installed.
package certgen

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"
)

const (
	// DefaultDays is the validity when Options.Days is 0.
	DefaultDays = 365
	// MaxDays is the longest validity. Browsers refuse a longer one for a public certificate,
	// and a long-lived key on a LAN host is not worth the risk.
	MaxDays = 825
	// MaxNames bounds the subject alternative names.
	MaxNames = 20

	maxNameLen = 253
	backdate   = 5 * time.Minute
)

var (
	ErrBadName  = errors.New("name is not a host name or an address")
	ErrTooMany  = fmt.Errorf("at most %d names", MaxNames)
	ErrNoNames  = errors.New("no names")
	ErrBadDays  = fmt.Errorf("days must be from 1 to %d", MaxDays)
	errNameLong = errors.New("name is longer than 253 characters")
)

// Options describe the certificate to make.
type Options struct {
	Names []string  // the first one is the subject common name
	Days  int       // 0 means DefaultDays
	Now   time.Time // zero means time.Now()
}

// Generate returns a PEM certificate and a PEM private key (ECDSA P-256, PKCS 8).
func Generate(o Options) (certPEM, keyPEM []byte, err error) {
	names, err := NormalizeNames(o.Names)
	if err != nil {
		return nil, nil, err
	}
	if len(names) == 0 {
		return nil, nil, ErrNoNames
	}
	days := o.Days
	if days == 0 {
		days = DefaultDays
	}
	if days < 1 || days > MaxDays {
		return nil, nil, ErrBadDays
	}
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("make key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("make serial: %w", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: names[0]},
		NotBefore:             now.Add(-backdate),
		NotAfter:              now.Add(time.Duration(days) * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
		SignatureAlgorithm:    x509.ECDSAWithSHA256,
	}
	for _, n := range names {
		if ip := net.ParseIP(n); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, n)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("sign certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("encode key: %w", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}

// NormalizeNames trims, lower-cases and de-duplicates names and checks each one. It keeps the
// order, so the first name stays the common name.
func NormalizeNames(in []string) ([]string, error) {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		n := strings.ToLower(strings.TrimSpace(raw))
		if n == "" {
			continue
		}
		if ip := net.ParseIP(n); ip != nil {
			n = ip.String()
		} else if err := checkDNSName(n); err != nil {
			return nil, fmt.Errorf("%q: %w", raw, err)
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	if len(out) > MaxNames {
		return nil, ErrTooMany
	}
	return out, nil
}

func checkDNSName(n string) error {
	if len(n) > maxNameLen {
		return errNameLong
	}
	rest := strings.TrimPrefix(n, "*.")
	if rest == "" {
		return ErrBadName
	}
	for _, label := range strings.Split(rest, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return ErrBadName
		}
		for _, r := range label {
			ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-'
			if !ok {
				return ErrBadName
			}
		}
	}
	return nil
}

// Info is what a person compares against the browser warning.
type Info struct {
	Subject     string
	CommonName  string
	Names       []string
	NotBefore   time.Time
	NotAfter    time.Time
	Fingerprint string // SHA-256 of the DER certificate, upper case hex pairs joined by colons
	SelfSigned  bool
}

// Describe reads the first certificate of a PEM block.
func Describe(certPEM []byte) (Info, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return Info{}, errors.New("no certificate in the file")
	}
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return Info{}, fmt.Errorf("parse certificate: %w", err)
	}
	var names []string
	names = append(names, c.DNSNames...)
	for _, ip := range c.IPAddresses {
		names = append(names, ip.String())
	}
	return Info{
		Subject:     c.Subject.String(),
		CommonName:  c.Subject.CommonName,
		Names:       names,
		NotBefore:   c.NotBefore,
		NotAfter:    c.NotAfter,
		Fingerprint: fingerprint(block.Bytes),
		SelfSigned:  selfSigned(c),
	}, nil
}

func fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	h := strings.ToUpper(hex.EncodeToString(sum[:]))
	parts := make([]string, 0, len(sum))
	for i := 0; i < len(h); i += 2 {
		parts = append(parts, h[i:i+2])
	}
	return strings.Join(parts, ":")
}

// selfSigned reports whether the issuer is the subject and the key of the certificate made the
// signature. CheckSignatureFrom would refuse a leaf as its own parent, so the signature is
// checked directly.
func selfSigned(c *x509.Certificate) bool {
	if !bytes.Equal(c.RawIssuer, c.RawSubject) {
		return false
	}
	return c.CheckSignature(c.SignatureAlgorithm, c.RawTBSCertificate, c.Signature) == nil
}
