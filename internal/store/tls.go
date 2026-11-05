package store

import (
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/FlexEbat/Netscribe/internal/model"
	dbgen "github.com/FlexEbat/Netscribe/internal/store/db"
)

// The limits of what a certificate may put into the database. The text comes from the
// network, so every name is cleaned and cut here whatever the collector did.
const (
	maxTLSName  = 200
	maxTLSSANs  = 5
	maxWhyRunes = 80
	maxWhy      = 5
)

func cutRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// cleanTLS returns the columns of a certificate, normalized and capped, and false when the
// certificate has nothing to keep (no fingerprint).
func cleanTLS(c *model.TLSCert) (dbgen.SetServiceTLSParams, bool) {
	if c == nil {
		return dbgen.SetServiceTLSParams{}, false
	}
	fp := strings.ToLower(strings.TrimSpace(c.Fingerprint))
	if b, err := hex.DecodeString(fp); err != nil || len(b) != 32 {
		return dbgen.SetServiceTLSParams{}, false
	}
	sans := make([]string, 0, maxTLSSANs)
	for _, s := range c.SANs {
		if s = cutRunes(cleanText(s), maxTLSName); s != "" && len(sans) < maxTLSSANs {
			sans = append(sans, s)
		}
	}
	enc, _ := json.Marshal(sans) // a slice of strings always encodes
	return dbgen.SetServiceTLSParams{
		TlsSubject:     cutRunes(cleanText(c.Subject), maxTLSName),
		TlsSans:        string(enc),
		TlsIssuer:      cutRunes(cleanText(c.Issuer), maxTLSName),
		TlsNotBefore:   formatTime(c.NotBefore.UTC()),
		TlsNotAfter:    formatTime(c.NotAfter.UTC()),
		TlsFingerprint: fp,
	}, true
}

// decodeTLS is the certificate of a service row, nil when none was read.
func decodeTLS(r dbgen.Service) *model.TLSCert {
	if r.TlsFingerprint == "" {
		return nil
	}
	c := &model.TLSCert{Subject: r.TlsSubject, Issuer: r.TlsIssuer, Fingerprint: r.TlsFingerprint, SANs: []string{}}
	if err := json.Unmarshal([]byte(r.TlsSans), &c.SANs); err != nil || c.SANs == nil {
		c.SANs = []string{}
	}
	if t, err := parseTime(r.TlsNotBefore); err == nil {
		c.NotBefore = t
	}
	if t, err := parseTime(r.TlsNotAfter); err == nil {
		c.NotAfter = t
	}
	return c
}

// cleanReasons caps the phrases that explain a kind: at most 5, each cleaned and 80 runes.
func cleanReasons(in []string) []string {
	out := make([]string, 0, maxWhy)
	for _, s := range in {
		if s = cutRunes(cleanText(s), maxWhyRunes); s != "" && len(out) < maxWhy {
			out = append(out, s)
		}
	}
	return out
}

func encodeReasons(in []string) string {
	b, _ := json.Marshal(cleanReasons(in)) // a slice of strings always encodes
	return string(b)
}

func decodeReasons(s string) []string {
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil || out == nil {
		return []string{}
	}
	return out
}

// expiringSoon is how close to its end a certificate gets a change entry.
const expiringSoon = 14 * 24 * time.Hour
