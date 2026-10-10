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
	cfg       config.Config
	log       *slog.Logger
	dnsWait   time.Duration
}

// NewScanner returns a scanner that discovers hosts with ARP and resolves names with system DNS.
func NewScanner(store ScanStore, cfg config.Config, log *slog.Logger) *Scanner {
	return &Scanner{store: store, discovery: NewARP(), resolver: net.DefaultResolver, cfg: cfg, log: log, dnsWait: dnsTimeout}
}

// Run scans the targets. The discovery stage decides the outcome: when it fails,
// or the scan is canceled before the result is stored, the scan ends as failed
// and the device state stays as it was.
func (s *Scanner) Run(ctx context.Context, targets []netip.Prefix) (model.Scan, error) {
	if len(targets) == 0 {
		return model.Scan{}, errors.New("no scan targets")
	}
	scan, err := s.store.StartScan(ctx, joinTargets(targets))
	if err != nil {
		return model.Scan{}, fmt.Errorf("start scan: %w", err)
	}
	s.log.Info("scan started", "scan", scan.ID, "target", scan.Target)

	res, err := s.discovery.Collect(ctx, Input{Targets: targets, Config: s.cfg})
	if err != nil {
		return s.fail(ctx, scan.ID, fmt.Errorf("%s: %w", s.discovery.Name(), err))
	}
	s.resolveHostnames(ctx, res.Devices)
	if err := ctx.Err(); err != nil {
		return s.fail(ctx, scan.ID, err)
	}
	if err := s.store.ApplyResult(ctx, scan.ID, res); err != nil {
		return s.fail(ctx, scan.ID, fmt.Errorf("store result: %w", err))
	}

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
	return final, nil
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
