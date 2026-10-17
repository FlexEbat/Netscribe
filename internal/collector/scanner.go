package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
)

const (
	dnsTimeout     = time.Second
	dnsConcurrency = 256
	finishTimeout  = 5 * time.Second
	maxHostname    = 253
)

// ScanStore is the part of store.Repo the scanner needs.
type ScanStore interface {
	StartScan(ctx context.Context, target string) (model.Scan, error)
	FinishScan(ctx context.Context, id int64, status, errMsg string) error
	GetScan(ctx context.Context, id int64) (model.Scan, error)
	ApplyResult(ctx context.Context, scanID int64, r Result) error
}

// Resolver does reverse DNS lookups. *net.Resolver satisfies it.
type Resolver interface {
	LookupAddr(ctx context.Context, addr string) ([]string, error)
}

// Scanner runs the collectors in order and records the outcome.
type Scanner struct {
	store     ScanStore
	discovery Collector
	resolver  Resolver
	log       *slog.Logger
	dnsWait   time.Duration
	events    Publisher
	busy      atomic.Bool

	// cfg is the config in force, SetConfig replaces it while the server runs. A scan
	// copies it into scanCfg when it takes the scan slot and keeps that copy to the end,
	// so a reload never changes a running scan.
	cfgMu   sync.RWMutex
	cfg     config.Config
	scanCfg config.Config

	// wake tells Schedule that scan.interval changed. timer is replaced in tests.
	wake  chan struct{}
	timer func(time.Duration) (<-chan time.Time, func() bool)

	// enrichers run after the discovery stage, in order: SNMP first, because LLDP uses
	// what it learned. An error of an enricher is logged, the scan goes on.
	enrichers []Collector

	// late run after classify, in order: the order of section 5.2 puts ssh behind it.
	// An error of a late collector is logged, the scan goes on.
	late []Collector

	// docker is also the last of late. It keeps its own state and watches the daemon
	// between scans.
	docker *Docker

	// classifier sets the kind of devices nothing else has typed, facts gives it
	// sysServices and open ports. Both are optional.
	classifier *Classifier
	facts      func(model.DeviceKey) Facts
}

// NewScanner returns a scanner that discovers hosts with ARP and resolves names with system DNS.
func NewScanner(store ScanStore, cfg config.Config, log *slog.Logger) *Scanner {
	probe, snmp, services := NewProbe(log), NewSNMP(log), NewServices(log)
	docker := NewDocker(cfg.Docker, log)
	lldp := NewLLDP(snmp, log)
	classifier, err := DefaultClassifier()
	if err != nil {
		log.Error("the built-in classify rules are broken, kinds are not set", "err", err)
		classifier = nil
	}
	return &Scanner{
		store: store, discovery: NewARP(), resolver: net.DefaultResolver, cfg: cfg, log: log, dnsWait: dnsTimeout,
		wake: make(chan struct{}, 1),
		// The order matters: the probe finds hosts the SNMP collector then asks, LLDP
		// tells which ports are trunks, and the router and switch tables use both.
		enrichers:  []Collector{probe, snmp, lldp, NewSNMPARP(snmp, lldp, log), services},
		late:       []Collector{NewSSH(log), docker},
		docker:     docker,
		classifier: classifier,
		facts: func(k model.DeviceKey) Facts {
			f := Facts{Services: -1, Ports: mergePorts(probe.openPorts(k.IP), services.openPorts(k.IP))}
			if n, ok := snmp.servicesOf(k.IP); ok {
				f.Services = n
			}
			return f
		},
	}
}

// ErrBusy means another scan is running: one scan at a time (section 9.6 of the spec).
var ErrBusy = errors.New("a scan is already running")

// ErrNoTargets means there is nothing to scan: no targets are configured.
var ErrNoTargets = errors.New("no scan targets")

// Publisher receives scan events. *events.Hub satisfies it. A nil Publisher drops them.
type Publisher interface {
	Publish(name string, data any)
}

// Progress is the data of a scan.progress event.
type Progress struct {
	ScanID int64  `json:"scanId"`
	Stage  string `json:"stage"`
	Done   int    `json:"done"`
	Total  int    `json:"total"`
}

// stages is the number of progress steps of a scan: discovery, dns, store, and snmp
// (with lldp) when enrichers are set.
func (s *Scanner) stages() int {
	if len(s.enrichers) > 0 {
		return 4
	}
	return 3
}

// SetEvents sets the receiver of scan events.
func (s *Scanner) SetEvents(p Publisher) { s.events = p }

// Docker returns the Docker collector, which the server also runs between scans.
func (s *Scanner) Docker() *Docker { return s.docker }

func (s *Scanner) config() config.Config {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg
}

// SetConfig replaces the config for the next scan. The scan that is running ends with
// the config it started with. The Docker collector reconnects when its settings change.
func (s *Scanner) SetConfig(cfg config.Config) {
	s.cfgMu.Lock()
	intervalChanged := s.cfg.Scan.Interval != cfg.Scan.Interval
	s.cfg = cfg
	s.cfgMu.Unlock()
	if s.docker != nil {
		s.docker.Reconfigure(cfg.Docker)
	}
	if intervalChanged {
		select {
		case s.wake <- struct{}{}:
		default: // a wake-up is already waiting
		}
	}
}

func (s *Scanner) publish(name string, data any) {
	if s.events != nil {
		s.events.Publish(name, data)
	}
}

func (s *Scanner) progress(scanID int64, stage string, done int) {
	s.publish("scan.progress", Progress{ScanID: scanID, Stage: stage, Done: done, Total: s.stages()})
}

// begin takes the single scan slot and records the new scan. The caller releases the
// slot with s.busy.Store(false) once the scan has ended.
func (s *Scanner) begin(ctx context.Context, targets []netip.Prefix) (model.Scan, error) {
	if len(targets) == 0 {
		return model.Scan{}, ErrNoTargets
	}
	if !s.busy.CompareAndSwap(false, true) {
		return model.Scan{}, ErrBusy
	}
	scan, err := s.store.StartScan(ctx, joinTargets(targets))
	if err != nil {
		s.busy.Store(false)
		return model.Scan{}, fmt.Errorf("start scan: %w", err)
	}
	s.scanCfg = s.config()
	s.log.Info("scan started", "scan", scan.ID, "target", scan.Target)
	s.publish("scan.started", scan)
	return scan, nil
}

// Run scans the targets and waits for the result. The discovery stage decides the
// outcome: when it fails, or the scan is canceled before the result is stored, the
// scan ends as failed and the device state stays as it was.
func (s *Scanner) Run(ctx context.Context, targets []netip.Prefix) (model.Scan, error) {
	scan, err := s.begin(ctx, targets)
	if err != nil {
		return model.Scan{}, err
	}
	defer s.busy.Store(false)
	return s.execute(ctx, scan, targets)
}

// Start records the scan and returns it while the scan continues in the background
// until it ends or ctx is canceled. ErrBusy means a scan is already running.
func (s *Scanner) Start(ctx context.Context, targets []netip.Prefix) (model.Scan, error) {
	scan, err := s.begin(ctx, targets)
	if err != nil {
		return model.Scan{}, err
	}
	go func() {
		defer s.busy.Store(false)
		// execute records its own failure in the log and in the scan row.
		_, _ = s.execute(ctx, scan, targets)
	}()
	return scan, nil
}

// Schedule starts a scan every scan.interval until ctx ends. An interval of 0 turns the
// schedule off. A new interval takes effect at once: the wait starts over with it. A tick
// that finds a scan still running is skipped. targets turns the config in force into the
// scan targets, so a change of scan.targets applies to the next scheduled scan.
func (s *Scanner) Schedule(ctx context.Context, targets func(config.Config) ([]netip.Prefix, error)) {
	newTimer := s.timer
	if newTimer == nil {
		newTimer = func(d time.Duration) (<-chan time.Time, func() bool) {
			t := time.NewTimer(d)
			return t.C, t.Stop
		}
	}
	for {
		var tick <-chan time.Time
		stop := func() bool { return false }
		if iv := s.config().Scan.Interval.Std(); iv > 0 {
			tick, stop = newTimer(iv)
		}
		select {
		case <-ctx.Done():
			stop()
			return
		case <-s.wake:
			stop()
		case <-tick:
			s.scheduled(ctx, targets)
		}
	}
}

func (s *Scanner) scheduled(ctx context.Context, targets func(config.Config) ([]netip.Prefix, error)) {
	list, err := targets(s.config())
	if err != nil {
		s.log.Warn("scheduled scan not started", "err", err)
		return
	}
	_, err = s.Start(ctx, list)
	switch {
	case err == nil:
	case errors.Is(err, ErrBusy):
		s.log.Info("scheduled scan skipped, the previous scan is still running")
	case errors.Is(err, ErrNoTargets):
		s.log.Warn("scheduled scan skipped, no scan targets are configured")
	default:
		s.log.Error("scheduled scan not started", "err", err)
	}
}

func (s *Scanner) execute(ctx context.Context, scan model.Scan, targets []netip.Prefix) (model.Scan, error) {
	res, err := s.discovery.Collect(ctx, Input{Targets: targets, Config: s.scanCfg})
	if err != nil {
		return s.fail(ctx, scan.ID, fmt.Errorf("%s: %w", s.discovery.Name(), err))
	}
	s.progress(scan.ID, "discovery", 1)
	s.resolveHostnames(ctx, res.Devices)
	if err := ctx.Err(); err != nil {
		return s.fail(ctx, scan.ID, err)
	}
	s.progress(scan.ID, "dns", 2)
	if len(s.enrichers) > 0 {
		res = s.enrich(ctx, targets, res)
		if err := ctx.Err(); err != nil {
			return s.fail(ctx, scan.ID, err)
		}
		s.progress(scan.ID, "snmp", 3)
	}
	if err := s.store.ApplyResult(ctx, scan.ID, res); err != nil {
		return s.fail(ctx, scan.ID, fmt.Errorf("store result: %w", err))
	}
	s.progress(scan.ID, "store", s.stages())

	fctx, cancel := detached(ctx)
	defer cancel()
	if err := s.store.FinishScan(fctx, scan.ID, "done", ""); err != nil {
		return model.Scan{}, fmt.Errorf("finish scan: %w", err)
	}
	final, err := s.store.GetScan(fctx, scan.ID)
	if err != nil {
		return model.Scan{}, fmt.Errorf("read scan: %w", err)
	}
	s.log.Info("scan finished", "scan", final.ID, "devices", final.DeviceCount)
	s.publish("scan.finished", final)
	s.publish("topology.changed", struct{}{})
	return final, nil
}

// enrich runs the enrichers on the devices the discovery stage found and appends what
// they learn. The store applies the inputs in order and a later non-empty value wins, so
// a name read over SNMP replaces the name from reverse DNS. A failing enricher costs only
// its own facts.
func (s *Scanner) enrich(ctx context.Context, targets []netip.Prefix, base Result) Result {
	var devices []model.Device
	known := map[model.DeviceKey]struct{}{}
	remember := func(inputs []model.DeviceInput) {
		for _, d := range inputs {
			if _, ok := known[d.Key]; ok || d.Key.IP == "" {
				continue
			}
			known[d.Key] = struct{}{}
			devices = append(devices, model.Device{MAC: d.Key.MAC, IP: d.Key.IP, Hostname: d.Hostname})
		}
	}
	remember(base.Devices)

	res := base
	// collect runs cs on what is known so far and merges what they return. It reports
	// false when the scan was canceled.
	collect := func(cs []Collector) bool {
		for _, c := range cs {
			if ctx.Err() != nil {
				return false
			}
			// Devices found by an earlier collector are asked by the later ones.
			out, err := c.Collect(ctx, Input{Targets: targets, Devices: devices, Config: s.scanCfg})
			if err != nil {
				if ctx.Err() == nil {
					s.log.Warn("collector failed", "collector", c.Name(), "err", err)
				}
				continue
			}
			if s.resolver != nil {
				s.resolveHostnames(ctx, out.Devices) // names for the hosts the discovery stage did not see
			}
			remember(out.Devices)
			res.Devices = append(res.Devices, out.Devices...)
			res.Interfaces = append(res.Interfaces, out.Interfaces...)
			res.Links = append(res.Links, out.Links...)
			res.LinksPolled = append(res.LinksPolled, out.LinksPolled...)
			res.FDBPolled = append(res.FDBPolled, out.FDBPolled...)
			res.Services = append(res.Services, out.Services...)
			res.ServicesPolled = append(res.ServicesPolled, out.ServicesPolled...)
			res.Containers = append(res.Containers, out.Containers...)
			res.ContainersPolled = append(res.ContainersPolled, out.ContainersPolled...)
		}
		return true
	}
	if !collect(s.enrichers) {
		return res
	}
	fillVendors(res.Devices)
	if s.classifier != nil {
		s.classifier.Apply(res.Devices, s.facts)
	}
	collect(s.late)
	return res
}

// mergePorts joins two lists of open ports without repeats.
func mergePorts(a, b []int) []int {
	seen := map[int]struct{}{}
	var out []int
	for _, list := range [][]int{a, b} {
		for _, p := range list {
			if _, ok := seen[p]; !ok {
				seen[p] = struct{}{}
				out = append(out, p)
			}
		}
	}
	return out
}

// fail records the failure with a context that survives cancellation, because
// the usual reason to land here is that ctx was canceled.
func (s *Scanner) fail(ctx context.Context, id int64, cause error) (model.Scan, error) {
	msg := cause.Error()
	if errors.Is(cause, context.Canceled) {
		msg = "canceled"
	}
	fctx, cancel := detached(ctx)
	defer cancel()
	if err := s.store.FinishScan(fctx, id, "failed", msg); err != nil {
		s.log.Error("record failed scan", "scan", id, "err", err)
		return model.Scan{}, cause
	}
	s.log.Warn("scan failed", "scan", id, "reason", msg)
	final, err := s.store.GetScan(fctx, id)
	if err != nil {
		return model.Scan{}, cause
	}
	s.publish("scan.finished", final)
	return final, cause
}

func detached(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), finishTimeout)
}

func joinTargets(targets []netip.Prefix) string {
	parts := make([]string, len(targets))
	for i, t := range targets {
		parts[i] = t.String()
	}
	return strings.Join(parts, ",")
}

// resolveHostnames fills empty hostnames from reverse DNS. Every lookup has its own
// timeout and they run side by side, so a dead DNS server costs one timeout in total.
func (s *Scanner) resolveHostnames(ctx context.Context, devs []model.DeviceInput) {
	sem := make(chan struct{}, dnsConcurrency)
	var wg sync.WaitGroup
loop:
	for i := range devs {
		if devs[i].Hostname != "" || devs[i].Key.IP == "" {
			continue
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break loop
		}
		wg.Add(1)
		go func(d *model.DeviceInput) {
			defer wg.Done()
			defer func() { <-sem }()
			lctx, cancel := context.WithTimeout(ctx, s.dnsWait)
			defer cancel()
			names, err := s.resolver.LookupAddr(lctx, d.Key.IP)
			if err != nil || len(names) == 0 {
				return
			}
			d.Hostname = cleanHostname(names[0])
		}(&devs[i])
	}
	wg.Wait()
}

func cleanHostname(name string) string {
	name = strings.TrimSuffix(strings.TrimSpace(name), ".")
	if len(name) > maxHostname {
		name = name[:maxHostname]
		// Do not leave half of a multi-byte character at the end.
		for name != "" {
			if r, size := utf8.DecodeLastRuneInString(name); r != utf8.RuneError || size > 1 {
				break
			}
			name = name[:len(name)-1]
		}
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1 // names come from the network and end up in logs and terminals
		}
		return r
	}, name)
}
