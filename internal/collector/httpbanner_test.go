package collector

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// webInput is an Input with the HTTP source on or off.
func webInput(on bool, ips ...string) Input {
	cfg := testConfig()
	cfg.Discovery.HTTPHeaders = on
	return Input{Devices: liveDevices(ips...), Config: cfg}
}

// bannerFor asks a test server through a collector that treats its port as a web port.
func bannerFor(t *testing.T, srv *httptest.Server, scheme string) (*HTTPBanner, Result) {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	c := NewHTTPBanner(quietLogger(), func(string) []int { return []int{port} })
	c.web = map[int]string{port: scheme}
	res, err := c.Collect(context.Background(), webInput(true, u.Hostname()))
	if err != nil {
		t.Fatal(err)
	}
	return c, res
}

func TestHTTPBannerIsTheServerHeader(t *testing.T) {
	var seen http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = *r.Clone(context.Background())
		w.Header().Set("Server", "nginx/1.25.3")
		_, _ = w.Write([]byte("<html>never read</html>"))
	}))
	defer srv.Close()

	c, res := bannerFor(t, srv, "http")
	if len(res.Services) != 1 || res.Services[0].Banner != "nginx/1.25.3" || res.Services[0].Proto != "tcp" {
		t.Fatalf("services = %+v, want one with the Server header as banner", res.Services)
	}
	if seen.Method != http.MethodHead || seen.URL.Path != "/" {
		t.Errorf("request = %s %s, want HEAD /", seen.Method, seen.URL.Path)
	}
	for _, h := range []string{"Authorization", "Cookie"} {
		if seen.Header.Get(h) != "" {
			t.Errorf("the request carries %s", h)
		}
	}
	if got := c.banner("127.0.0.1"); got != "nginx/1.25.3" {
		t.Errorf("banner for the classifier = %q, want it lower case", got)
	}
}

func TestHTTPBannerAddsPoweredByAndRealm(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "lighttpd")
		w.Header().Set("X-Powered-By", "PHP/8.2")
		w.Header().Set("WWW-Authenticate", `Basic realm="RouterOS v7"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, res := bannerFor(t, srv, "http")
	want := "lighttpd; X-Powered-By: PHP/8.2; realm: RouterOS v7"
	if len(res.Services) != 1 || res.Services[0].Banner != want {
		t.Fatalf("services = %+v, want banner %q", res.Services, want)
	}
}

func TestHTTPBannerIsCutAndHasNoControlCharacters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "tab\there "+strings.Repeat("x", 400))
	}))
	defer srv.Close()

	_, res := bannerFor(t, srv, "http")
	if len(res.Services) != 1 {
		t.Fatalf("services = %+v", res.Services)
	}
	b := res.Services[0].Banner
	if len([]rune(b)) > 200 {
		t.Errorf("banner has %d characters, want at most 200", len([]rune(b)))
	}
	if strings.ContainsAny(b, "\t\r\n\x00") {
		t.Errorf("banner %q has control characters", b)
	}
	if !strings.HasPrefix(b, "tabhere xxx") {
		t.Errorf("banner = %q, want the text with the tab removed", b)
	}
}

func TestHTTPBannerDoesNotFollowRedirects(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			hits.Add(1)
			return
		}
		w.Header().Set("Server", "redirector")
		w.Header().Set("Location", "/elsewhere")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	_, res := bannerFor(t, srv, "http")
	if hits.Load() != 0 {
		t.Errorf("the redirect target was requested %d times", hits.Load())
	}
	if len(res.Services) != 1 || res.Services[0].Banner != "redirector" {
		t.Errorf("services = %+v, want the banner of the first answer", res.Services)
	}
}

func TestHTTPBannerRefusesHeadersOverTheLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "big")
		w.Header().Set("X-Filler", strings.Repeat("a", 64<<10))
	}))
	defer srv.Close()

	_, res := bannerFor(t, srv, "http")
	if len(res.Services) != 0 {
		t.Errorf("services = %+v, an answer with more than 8 KiB of headers must be skipped", res.Services)
	}
}

func TestHTTPBannerGivesUpOnASlowServer(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	start := time.Now()
	_, res := bannerFor(t, srv, "http")
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("took %v, want a request to end within about 2s", took)
	}
	if len(res.Services) != 0 {
		t.Errorf("services = %+v, want none from a server that never answers", res.Services)
	}
}

func TestHTTPBannerSkipsAGarbageAnswer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_, _ = conn.Write([]byte("\xff\xfe not http at all\r\n\r\n"))
			_ = conn.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	c := NewHTTPBanner(quietLogger(), func(string) []int { return []int{port} })
	c.web = map[int]string{port: "http"}
	res, err := c.Collect(context.Background(), webInput(true, "127.0.0.1"))
	if err != nil || len(res.Services) != 0 {
		t.Errorf("result = %+v, err = %v, want no service and no error", res, err)
	}
}

func TestHTTPBannerOverHTTPSAddsTheCertificateName(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "router.lan"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "mini_httpd")
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
	srv.StartTLS()
	defer srv.Close()

	// The certificate is self-signed, and the request must still go through.
	_, res := bannerFor(t, srv, "https")
	if len(res.Services) != 1 || res.Services[0].Banner != "mini_httpd; CN: router.lan" {
		t.Fatalf("services = %+v, want the Server header and the certificate name", res.Services)
	}
}

func TestHTTPBannerSendsNothingWhenTheSourceIsOff(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())

	c := NewHTTPBanner(quietLogger(), func(string) []int { return []int{port} })
	c.web = map[int]string{port: "http"}
	res, err := c.Collect(context.Background(), webInput(false, u.Hostname()))
	if err != nil || len(res.Services) != 0 || hits.Load() != 0 {
		t.Errorf("result = %+v, err = %v, requests = %d, want none", res, err, hits.Load())
	}
}

func TestHTTPBannerAsksOnlyWebPorts(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())

	// The port is open, but it is not 80, 443 or 8080.
	c := NewHTTPBanner(quietLogger(), func(string) []int { return []int{port, 22} })
	res, err := c.Collect(context.Background(), webInput(true, u.Hostname()))
	if err != nil || len(res.Services) != 0 || hits.Load() != 0 {
		t.Errorf("result = %+v, err = %v, requests = %d, want none", res, err, hits.Load())
	}
}

func TestNetworkText(t *testing.T) {
	cases := map[string]string{
		"plain":            "plain",
		"  padded \t":      "padded",
		"a\x00b\x1bc":      "abc",
		"\xff\xfe invalid": "",
		"日本語":              "日本語",
	}
	for in, want := range cases {
		if got := networkText(in); got != want {
			t.Errorf("networkText(%q) = %q, want %q", in, got, want)
		}
	}
}
