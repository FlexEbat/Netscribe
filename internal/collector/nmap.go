package collector

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	nmapHostTimeout = 90 * time.Second // for one device
	nmapConcurrency = 2                // devices asked at one moment
	nmapMaxOutput   = 1 << 20          // the most XML read from one run
	nmapMinAccuracy = 85               // a guess below this percent is not used
	nmapGiveUpAfter = 3                // failures in a row before the stage stops
)

// nmapRunner starts nmap and returns what it printed. Tests replace it.
type nmapRunner func(ctx context.Context, path string, args []string) ([]byte, error)

// Nmap asks an installed nmap what operating system and device type a device looks like.
// It is off by default (discovery.nmap) and the scanner only asks about devices that
// nothing else could type. Nmap is a separate program the person installs: Netscribe runs
// it with fixed arguments and an address it has checked, and it reads only the XML result.
//
// OS detection needs raw sockets. Without them nmap fails, the stage logs the reason and
// the scan goes on. See README.md for the capabilities nmap needs.
type Nmap struct {
	log      *slog.Logger
	run      nmapRunner
	lookPath func(file string) (string, error)

	mu    sync.Mutex
	facts map[string][]string // ip -> "type=router", "os=linux 5.x" of the last Collect
}

func NewNmap(log *slog.Logger) *Nmap {
	return &Nmap{log: log, run: runNmap, lookPath: exec.LookPath, facts: map[string][]string{}}
}

func (c *Nmap) Name() string { return "nmap" }

// guess returns what nmap said about ip in the last Collect, in lower case.
func (c *Nmap) guess(ip string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.facts[ip]
}

// Collect asks nmap about every device in in.Devices. discovery.nmap off runs nothing.
// The result is empty: what nmap guessed only helps to classify the devices.
func (c *Nmap) Collect(ctx context.Context, in Input) (Result, error) {
	if !in.Config.Discovery.Nmap {
		return Result{}, nil
	}
	path := in.Config.Discovery.NmapPath
	if path == "" {
		found, err := c.lookPath("nmap")
		if err != nil {
			c.log.Warn("discovery.nmap is on, but nmap was not found in PATH, set discovery.nmapPath")
			return Result{}, nil
		}
		path = found
	}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		failures int
		done     bool
		found    = map[string][]string{}
		sem      = make(chan struct{}, nmapConcurrency)
	)
	stopped := func() bool { mu.Lock(); defer mu.Unlock(); return done }
loop:
	for _, d := range in.Devices {
		addr, err := netip.ParseAddr(d.IP)
		if err != nil || !addr.Is4() {
			continue // only an IPv4 address that parses is ever passed to nmap
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break loop
		}
		if stopped() {
			<-sem
			break
		}
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			defer func() { <-sem }()
			facts, err := c.ask(ctx, path, ip)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				failures++
				if failures == 1 {
					c.log.Warn("nmap failed", "ip", ip, "err", err)
				}
				if failures >= nmapGiveUpAfter && len(found) == 0 {
					done = true // it fails the same way every time, stop trying
					c.log.Warn("nmap keeps failing, the stage is skipped for this scan")
				}
				return
			}
			failures = 0
			if len(facts) > 0 {
				found[ip] = facts
			}
		}(addr.String())
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	c.mu.Lock()
	c.facts = found
	c.mu.Unlock()
	return Result{}, nil
}

// ask runs nmap on one address and returns what it guessed.
func (c *Nmap) ask(ctx context.Context, path, ip string) ([]string, error) {
	rctx, cancel := context.WithTimeout(ctx, nmapHostTimeout)
	defer cancel()
	// -O: OS detection. -n: no DNS lookups. -Pn: the device is known to be up, skip the
	// host discovery. --privileged: use the capabilities the binary has, without asking
	// for root. --top-ports 100 keeps the port scan that OS detection needs short.
	args := []string{"-O", "-n", "-Pn", "--privileged", "--top-ports", "100", "--host-timeout", "60s", "-oX", "-", ip}
	out, err := c.run(rctx, path, args)
	if err != nil {
		return nil, err
	}
	return parseNmap(out)
}

// nmapRun is the part of nmap's XML that is read.
type nmapRun struct {
	Hosts []struct {
		OS struct {
			Matches []struct {
				Name     string `xml:"name,attr"`
				Accuracy int    `xml:"accuracy,attr"`
				Classes  []struct {
					Type     string `xml:"type,attr"`
					Accuracy int    `xml:"accuracy,attr"`
				} `xml:"osclass"`
			} `xml:"osmatch"`
		} `xml:"os"`
	} `xml:"host"`
}

// parseNmap returns the facts of the best OS guess: its name and the device types of its
// classes, each with an accuracy of at least nmapMinAccuracy. The text comes from a
// program that talked to the network, so it is cleaned and cut like any other.
func parseNmap(b []byte) ([]string, error) {
	var run nmapRun
	if err := xml.NewDecoder(bytes.NewReader(b)).Decode(&run); err != nil {
		return nil, fmt.Errorf("read nmap output: %w", err)
	}
	var out []string
	add := func(prefix, v string) {
		v = strings.ToLower(cutRunes(networkText(v), 100))
		if v == "" {
			return
		}
		fact := prefix + v
		for _, have := range out {
			if have == fact {
				return
			}
		}
		out = append(out, fact)
	}
	for _, h := range run.Hosts {
		best := -1
		for i, m := range h.OS.Matches {
			if m.Accuracy >= nmapMinAccuracy && (best < 0 || m.Accuracy > h.OS.Matches[best].Accuracy) {
				best = i
			}
		}
		if best < 0 {
			continue
		}
		m := h.OS.Matches[best]
		add("os=", m.Name)
		for _, cl := range m.Classes {
			if cl.Accuracy >= nmapMinAccuracy {
				add("type=", cl.Type)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// capped keeps the first max bytes written to it and drops the rest, so a program that
// prints without end cannot fill the memory.
type capped struct {
	buf bytes.Buffer
	max int
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); room > 0 {
		c.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

// runNmap starts the program. The path comes from the config or from PATH and the
// arguments are fixed apart from an address that was parsed as IPv4, so no shell is
// involved and nothing from the network reaches the command line.
func runNmap(ctx context.Context, path string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, path, args...) //nolint:gosec // see the comment above
	var stdout, stderr capped
	stdout.max, stderr.max = nmapMaxOutput, 512
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	// A minimal environment: nothing of the server's variables is passed on.
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C"}
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		reason := firstLine(networkText(stderr.buf.String()))
		if reason == "" {
			return nil, err
		}
		return nil, errors.New(reason)
	}
	return stdout.buf.Bytes(), nil
}
