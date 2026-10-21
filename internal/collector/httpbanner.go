package collector

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/FlexEbat/Netscribe/internal/model"
)

const (
	httpTimeout      = 2 * time.Second // for one request, the connect included
	httpMaxHeader    = 8 << 10         // the most header bytes read from one response
	httpConcurrency  = 32
	maxBannerRunes   = 200 // the most characters of a banner that are kept
	maxNetworkFields = 32  // the most mDNS types or SSDP targets kept for one device
)

// webPorts are the ports that get one request, with the scheme that speaks on them.
var webPorts = map[int]string{80: "http", 443: "https", 8080: "http"}

var realmPattern = regexp.MustCompile(`(?i)realm\s*=\s*"?([^",]*)`)

// HTTPBanner sends one HEAD / to every open web port of a live device and keeps what the
// answer says about the server: Server, X-Powered-By, the realm of WWW-Authenticate and,
// over HTTPS, the common name of the certificate.
//
// It reads the status line and the headers and nothing else: no body, no redirects, no
// credentials, no cookies. The certificate is not verified, because only the headers are
// read and a device often has a certificate nobody signed.
type HTTPBanner struct {
	log   *slog.Logger
	ports func(ip string) []int // open TCP ports of a device
	web   map[int]string        // the ports that get a request, replaced in tests

	client *http.Client

	mu      sync.Mutex
	banners map[string]string // ip -> all banners of the last Collect, lower case
}

// NewHTTPBanner returns a collector that asks the ports ports reports as open.
func NewHTTPBanner(log *slog.Logger, ports func(ip string) []int) *HTTPBanner {
	dialer := &net.Dialer{Timeout: httpTimeout}
	return &HTTPBanner{
		log:   log,
		ports: ports,
		web:   webPorts,
		client: &http.Client{
			Timeout: httpTimeout,
			// A redirect is an answer too: the first response is the one that is read.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
			Transport: &http.Transport{
				Proxy:                  nil, // the request goes to the device, not through a proxy
				DialContext:            dialer.DialContext,
				DisableKeepAlives:      true,
				MaxResponseHeaderBytes: httpMaxHeader,
				ResponseHeaderTimeout:  httpTimeout,
				TLSHandshakeTimeout:    httpTimeout,
				// Only headers are read, and devices rarely have a certificate a CA signed.
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS10}, //nolint:gosec // see above
			},
		},
		banners: map[string]string{},
	}
}

func (c *HTTPBanner) Name() string { return "http" }

// banner returns the banners found on ip in the last Collect, in lower case.
func (c *HTTPBanner) banner(ip string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.banners[ip]
}

// Collect asks the web ports of every live device. discovery.httpHeaders off sends nothing.
func (c *HTTPBanner) Collect(ctx context.Context, in Input) (Result, error) {
	if !in.Config.Discovery.HTTPHeaders {
		return Result{}, nil
	}
	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		res Result
		all = map[string][]string{}
		sem = make(chan struct{}, httpConcurrency)
	)
loop:
	for _, d := range in.Devices {
		if d.IP == "" || c.ports == nil {
			continue
		}
		for _, port := range c.ports(d.IP) {
			scheme, ok := c.web[port]
			if !ok {
				continue
			}
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				break loop
			}
			wg.Add(1)
			go func(d model.Device, port int, scheme string) {
				defer wg.Done()
				defer func() { <-sem }()
				text := c.ask(ctx, scheme, d.IP, port)
				if text == "" {
					return
				}
				mu.Lock()
				defer mu.Unlock()
				res.Services = append(res.Services, model.ServiceInput{
					Device: model.DeviceKey{MAC: d.MAC, IP: d.IP}, Port: port, Proto: "tcp", Name: portNameOf(port), Banner: text,
				})
				all[d.IP] = append(all[d.IP], strings.ToLower(text))
			}(d, port, scheme)
		}
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	sort.Slice(res.Services, func(i, j int) bool {
		a, b := res.Services[i], res.Services[j]
		if a.Device.IP != b.Device.IP {
			return a.Device.IP < b.Device.IP
		}
		return a.Port < b.Port
	})
	joined := make(map[string]string, len(all))
	for ip, list := range all {
		sort.Strings(list)
		joined[ip] = strings.Join(list, "\n")
	}
	c.mu.Lock()
	c.banners = joined
	c.mu.Unlock()
	return res, nil
}

// ask sends the request and returns the banner, or "" when there is none.
func (c *HTTPBanner) ask(ctx context.Context, scheme, ip string, port int) string {
	if addr, err := netip.ParseAddr(ip); err != nil || !addr.IsValid() {
		return ""
	}
	url := scheme + "://" + net.JoinHostPort(ip, strconv.Itoa(port)) + "/"
	rctx, cancel := context.WithTimeout(ctx, httpTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodHead, url, nil)
	if err != nil {
		return ""
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return "" // closed, slow, not HTTP or a header over the limit: no banner
	}
	defer func() { _ = resp.Body.Close() }() // a HEAD answer has no body, none is read

	var parts []string
	if v := networkText(resp.Header.Get("Server")); v != "" {
		parts = append(parts, v)
	}
	if v := networkText(resp.Header.Get("X-Powered-By")); v != "" {
		parts = append(parts, "X-Powered-By: "+v)
	}
	for _, h := range resp.Header.Values("WWW-Authenticate") {
		if m := realmPattern.FindStringSubmatch(h); m != nil {
			if v := networkText(m[1]); v != "" {
				parts = append(parts, "realm: "+v)
				break
			}
		}
	}
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		if v := networkText(resp.TLS.PeerCertificates[0].Subject.CommonName); v != "" {
			parts = append(parts, "CN: "+v)
		}
	}
	return cutRunes(strings.Join(parts, "; "), maxBannerRunes)
}

// networkText cleans a text that came from the network: text that is not valid UTF-8
// is dropped, control characters are removed and the ends are trimmed. The caller cuts
// the length.
func networkText(s string) string {
	if !utf8.ValidString(s) {
		return ""
	}
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if model.UnsafeRune(r) {
			return -1
		}
		return r
	}, s))
}

// cutRunes returns s cut to at most n characters.
func cutRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:n]))
}
