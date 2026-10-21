package collector

import (
	"bufio"
	"bytes"
	"context"
	"log/slog"
	"net/netip"
	"net/textproto"
	"sort"
	"strings"
	"sync"
)

var ssdpGroup = netip.MustParseAddrPort("239.255.255.250:1900")

// ssdpSearch is the one request: ask every UPnP device in the segment to answer.
var ssdpSearch = []byte("M-SEARCH * HTTP/1.1\r\n" +
	"HOST: 239.255.255.250:1900\r\n" +
	"MAN: \"ssdp:discover\"\r\n" +
	"MX: 2\r\n" +
	"ST: ssdp:all\r\n\r\n")

// SSDP asks the local segments which UPnP devices are there. It sends one M-SEARCH with
// ST: ssdp:all, reads the answers for three seconds and keeps the SERVER and ST headers.
// The LOCATION header is never read, the address in it is never requested.
type SSDP struct {
	log *slog.Logger
	net multicastTransport

	mu    sync.Mutex
	found map[string][]string // ip -> lower case SERVER and ST values of the last Collect
}

func NewSSDP(log *slog.Logger) *SSDP {
	return &SSDP{log: log, net: udpMulticast{log: log}, found: map[string][]string{}}
}

func (c *SSDP) Name() string { return "ssdp" }

// announced returns the SERVER and ST values ip sent in the last Collect, in lower case.
func (c *SSDP) announced(ip string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.found[ip]
}

// Collect sends the search. discovery.ssdp off sends nothing. Nothing is added to the
// result: what the devices say only helps to classify them.
func (c *SSDP) Collect(ctx context.Context, in Input) (Result, error) {
	if !in.Config.Discovery.SSDP {
		return Result{}, nil
	}
	known := map[string]bool{}
	for _, d := range in.Devices {
		if d.IP != "" {
			known[d.IP] = true
		}
	}
	found := map[string][]string{}
	for _, dg := range c.net.Exchange(ctx, ssdpGroup, ssdpSearch, multicastWindow) {
		ip := dg.from.String()
		if !known[ip] {
			continue
		}
		for _, v := range parseSSDP(dg.data) {
			if len(found[ip]) < maxNetworkFields && !contains(found[ip], v) {
				found[ip] = append(found[ip], v)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	for _, list := range found {
		sort.Strings(list)
	}
	c.mu.Lock()
	c.found = found
	c.mu.Unlock()
	return Result{}, nil
}

// parseSSDP returns the SERVER and ST values of an answer, cleaned and in lower case.
// A packet that is not an HTTP/1.1 200 answer, or whose headers do not parse, gives
// nothing. A value that is not UTF-8 is skipped. LOCATION is not read.
func parseSSDP(b []byte) []string {
	r := textproto.NewReader(bufio.NewReader(bytes.NewReader(b)))
	status, err := r.ReadLine()
	if err != nil || !strings.HasPrefix(status, "HTTP/1.1 200") {
		return nil
	}
	hdr, err := r.ReadMIMEHeader()
	if err != nil && len(hdr) == 0 {
		return nil
	}
	var out []string
	for _, key := range []string{"Server", "St"} {
		if v := cutRunes(networkText(hdr.Get(key)), maxBannerRunes); v != "" {
			out = append(out, strings.ToLower(v))
		}
	}
	return out
}
