package api

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestClientIP(t *testing.T) {
	s := &server{trusted: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("fd00::/8")}}
	tests := []struct {
		name   string
		remote string
		xff    []string
		want   string
	}{
		{"direct client", "198.51.100.7:4000", nil, "198.51.100.7"},
		{"direct client ignores the header", "198.51.100.7:4000", []string{"1.2.3.4"}, "198.51.100.7"},
		{"trusted proxy, one hop", "10.0.0.1:4000", []string{"198.51.100.7"}, "198.51.100.7"},
		{"trusted proxy, the client forged the first hop", "10.0.0.1:4000", []string{"6.6.6.6, 198.51.100.7"}, "198.51.100.7"},
		{"two trusted proxies", "10.0.0.1:4000", []string{"198.51.100.7, 10.0.0.2"}, "198.51.100.7"},
		{"several header lines", "10.0.0.1:4000", []string{"6.6.6.6", "198.51.100.7"}, "198.51.100.7"},
		{"trusted proxy without the header", "10.0.0.1:4000", nil, "10.0.0.1"},
		{"malformed hop ends the chain", "10.0.0.1:4000", []string{"198.51.100.7, garbage"}, "10.0.0.1"},
		{"only proxies in the chain", "10.0.0.1:4000", []string{"10.0.0.2, 10.0.0.3"}, "10.0.0.1"},
		{"IPv6 client", "10.0.0.1:4000", []string{"2001:db8::1"}, "2001:db8::1"},
		{"IPv6 remote", "[2001:db8::5]:4000", []string{"1.2.3.4"}, "2001:db8::5"},
		{"IPv4-mapped remote", "[::ffff:198.51.100.7]:4000", nil, "198.51.100.7"},
		{"remote without a port", "198.51.100.7", nil, "198.51.100.7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tt.remote
			for _, v := range tt.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if got := s.clientIP(r); got != tt.want {
				t.Errorf("clientIP = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOneLine(t *testing.T) {
	for in, want := range map[string]string{
		"plain":       "plain",
		"two\nlines":  "two?lines",
		"esc\x1b[0m":  "esc?[0m",
		"tab\there":   "tab?here",
		"del\x7fchar": "del?char",
		"Кириллица":   "Кириллица",
		"":            "",
	} {
		if got := oneLine(in, 100); got != want {
			t.Errorf("oneLine(%q) = %q, want %q", in, got, want)
		}
	}
	if got := oneLine("абвгд", 3); got != "абв" {
		t.Errorf("oneLine truncates by characters: %q", got)
	}
	if got := oneLine("abcdef", 3); got != "abc" {
		t.Errorf("oneLine = %q", got)
	}
}
