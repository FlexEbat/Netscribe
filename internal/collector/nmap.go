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

	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
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
	// ports returns the open TCP ports of a device. OS detection needs an open and a
	// closed port, so a device with no open port is not asked. Nil asks every device.
	ports func(ip string) []int

	mu    sync.Mutex
	facts map[string][]string // ip -> "type=router", "os=linux 5.x" of the last Collect
}

// NmapState tells the panel whether nmap can be used.
type NmapState struct {
	// Available is true when the program was found.
	Available bool `json:"available"`
	// Enabled is true when discovery.nmap is on, so every scan runs nmap.
	Enabled bool `json:"enabled"`
	// Reason says why nmap is not available, empty otherwise.
	Reason string `json:"reason"`
}

var (
	// ErrNmapUnavailable is returned when nmap cannot be found.
	ErrNmapUnavailable = errors.New("nmap is not available")
	// ErrNoAddress is returned for a device that has no IPv4 address nmap could be asked about.
	ErrNoAddress = errors.New("the device has no IPv4 address")
	// ErrOutOfScope means the address lies outside the scan targets.
	ErrOutOfScope = errors.New("the device is outside the scan targets")
)

// resolve returns the program to run: discovery.nmapPath, or nmap from PATH.
func (c *Nmap) resolve(cfg config.DiscoveryConfig) (string, error) {
	if cfg.NmapPath != "" {
		return cfg.NmapPath, nil
	}
	found, err := c.lookPath("nmap")
	if err != nil {
		return "", ErrNmapUnavailable
	}
	return found, nil
}

// NmapIdentity is what nmap said about one device.
type NmapIdentity struct {
	OS    string   // the system name, empty when nmap had no guess
	Facts []string // for the classifier
}

// Identify asks nmap about one device. It runs whatever discovery.nmap says, because the
// person asked for this device by hand.
func (c *Nmap) Identify(ctx context.Context, cfg config.DiscoveryConfig, ip string) (NmapIdentity, error) {
	addr, err := netip.ParseAddr(ip)
	if err != nil || !addr.Is4() {
		return NmapIdentity{}, ErrNoAddress
	}
	path, err := c.resolve(cfg)
	if err != nil {
		return NmapIdentity{}, err
	}
	g, err := c.ask(ctx, path, addr.String())
	if err != nil {
		return NmapIdentity{}, err
	}
	return NmapIdentity{OS: g.os, Facts: g.facts}, nil
}

// State reports whether nmap is on and can be found. It starts no program.
func (c *Nmap) State(cfg config.DiscoveryConfig) NmapState {
	st := NmapState{Enabled: cfg.Nmap}
	file := cfg.NmapPath
	if file == "" {
		file = "nmap"
	}
	if _, err := c.lookPath(file); err != nil {
		st.Reason = "nmap was not found"
		if cfg.NmapPath != "" {
			st.Reason = "nmap was not found at discovery.nmapPath"
		}
		return st
	}
	st.Available = true
	return st
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
// The result carries the operating system name of each device nmap had a guess for. The
// rest of the guess only helps to classify the devices.
func (c *Nmap) Collect(ctx context.Context, in Input) (Result, error) {
	if !in.Config.Discovery.Nmap {
		return Result{}, nil
	}
	path, err := c.resolve(in.Config.Discovery)
	if err != nil {
		c.log.Warn("discovery.nmap is on, but nmap was not found, set discovery.nmapPath")
		return Result{}, nil
	}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		failures int
		done     bool
		found    = map[string][]string{}
		guesses  = map[string]nmapGuess{}
		sem      = make(chan struct{}, nmapConcurrency)
	)
	stopped := func() bool { mu.Lock(); defer mu.Unlock(); return done }
loop:
	for _, d := range in.Devices {
		addr, err := netip.ParseAddr(d.IP)
		if err != nil || !addr.Is4() {
			continue // only an IPv4 address that parses is ever passed to nmap
		}
		if c.ports != nil && len(c.ports(d.IP)) == 0 {
			continue // no open port, nmap would wait for a timeout and learn nothing
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
			guess, err := c.ask(ctx, path, ip)
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
			if len(guess.facts) > 0 {
				found[ip] = guess.facts
				guesses[ip] = guess
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

	// The operating system name is stored with the device. The kind comes from the
	// classifier, which reads the facts.
	var res Result
	for _, d := range in.Devices {
		if g, ok := guesses[d.IP]; ok && g.os != "" {
			res.Devices = append(res.Devices, model.DeviceInput{
				Key: model.DeviceKey{MAC: d.MAC, IP: d.IP}, OSGuess: g.os, Source: "nmap",
			})
		}
	}
	return res, nil
}

// ask runs nmap on one address and returns what it guessed.
func (c *Nmap) ask(ctx context.Context, path, ip string) (nmapGuess, error) {
	rctx, cancel := context.WithTimeout(ctx, nmapHostTimeout)
	defer cancel()
	// -O: OS detection. -n: no DNS lookups. -Pn: the device is known to be up, skip the
	// host discovery. --privileged: use the capabilities the binary has, without asking
	// for root. --top-ports 100 keeps the port scan that OS detection needs short.
	args := []string{"-O", "-n", "-Pn", "--privileged", "--top-ports", "100", "--host-timeout", "60s", "-oX", "-", ip}
	out, err := c.run(rctx, path, args)
	if err != nil {
		return nmapGuess{}, err
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
					Vendor   string `xml:"vendor,attr"`
					Family   string `xml:"osfamily,attr"`
					Accuracy int    `xml:"accuracy,attr"`
				} `xml:"osclass"`
			} `xml:"osmatch"`
		} `xml:"os"`
	} `xml:"host"`
}

// nmapGuess is the best OS guess of one device: the facts for the classifier and the
// name of the system as nmap wrote it.
type nmapGuess struct {
	facts []string
	os    string
}

// osFamily names the family of an OS class. Nmap calls both Apple iOS and Cisco IOS
// "IOS", which would make a Cisco switch look like a phone, so the Apple one is told
// apart by its vendor and becomes "apple ios". A Cisco class stays "ios".
func osFamily(family, vendor string) string {
	if strings.EqualFold(strings.TrimSpace(family), "ios") && strings.Contains(strings.ToLower(vendor), "apple") {
		return "apple ios"
	}
	return family
}

// parseNmap returns the best OS guess: its name and the device types of its classes, each
// with an accuracy of at least nmapMinAccuracy. The text comes from a program that talked
// to the network, so it is cleaned and cut like any other.
func parseNmap(b []byte) (nmapGuess, error) {
	var run nmapRun
	if err := xml.NewDecoder(bytes.NewReader(b)).Decode(&run); err != nil {
		return nmapGuess{}, fmt.Errorf("read nmap output: %w", err)
	}
	var out []string
	var osName string
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
		osName = cutRunes(networkText(m.Name), 100)
		add("os=", m.Name)
		for _, cl := range m.Classes {
			if cl.Accuracy >= nmapMinAccuracy {
				add("type=", cl.Type)
				add("family=", osFamily(cl.Family, cl.Vendor))
			}
		}
	}
	sort.Strings(out)
	return nmapGuess{facts: out, os: osName}, nil
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
