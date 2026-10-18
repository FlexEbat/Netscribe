package collector

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

type fakeStore struct {
	mu          sync.Mutex
	scan        model.Scan
	applied     []Result
	applyErr    error
	finishCalls int
	starts      int
	// finishCtxErr records ctx.Err() seen by FinishScan: it must be nil even after cancellation.
	finishCtxErr error
}

func (s *fakeStore) StartScan(_ context.Context, target string) (model.Scan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.starts++
	s.scan = model.Scan{ID: 7, Target: target, Status: "running"}
	return s.scan, nil
}

func (s *fakeStore) FinishScan(ctx context.Context, _ int64, status, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finishCalls++
	s.finishCtxErr = ctx.Err()
	s.scan.Status, s.scan.Error = status, errMsg
	return nil
}

func (s *fakeStore) GetScan(context.Context, int64) (model.Scan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.scan, nil
}

func (s *fakeStore) ApplyResult(_ context.Context, _ int64, r Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.applyErr != nil {
		return s.applyErr
	}
	s.applied = append(s.applied, r)
	s.scan.DeviceCount = len(r.Devices)
	return nil
}

type fakeCollector struct {
	res Result
	err error
	run func(ctx context.Context) error // optional hook, e.g. block until canceled
}

func (*fakeCollector) Name() string { return "fake" }

func (c *fakeCollector) Collect(ctx context.Context, _ Input) (Result, error) {
	if c.run != nil {
		if err := c.run(ctx); err != nil {
			return Result{}, err
		}
	}
	return c.res, c.err
}

type fakeResolver struct {
	names map[string]string
	block bool
	calls int
	mu    sync.Mutex
}

func (r *fakeResolver) LookupAddr(ctx context.Context, addr string) ([]string, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	if r.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if n, ok := r.names[addr]; ok {
		return []string{n}, nil
	}
	return nil, errors.New("no such host")
}

func newTestScanner(st *fakeStore, c Collector, r Resolver) *Scanner {
	return &Scanner{store: st, discovery: c, resolver: r, cfg: config.Config{}, log: quiet, dnsWait: 50 * time.Millisecond}
}

func twoDevices() Result {
	return Result{Devices: []model.DeviceInput{
		{Key: model.DeviceKey{MAC: "aa:aa:aa:aa:aa:01", IP: "192.168.1.1"}, Source: "arp", Kind: model.KindUnknown},
		{Key: model.DeviceKey{MAC: "aa:aa:aa:aa:aa:02", IP: "192.168.1.2"}, Source: "arp", Kind: model.KindUnknown},
	}}
}

func TestScannerSuccess(t *testing.T) {
	st := &fakeStore{}
	res := &fakeResolver{names: map[string]string{"192.168.1.1": "router.lan."}}
	sc, err := newTestScanner(st, &fakeCollector{res: twoDevices()}, res).Run(context.Background(), prefixes(t, "192.168.1.0/24", "10.0.0.0/24"))
	if err != nil {
		t.Fatal(err)
	}
	if sc.Status != "done" || sc.Error != "" || sc.DeviceCount != 2 {
		t.Errorf("scan = %+v", sc)
	}
	if sc.Target != "192.168.1.0/24,10.0.0.0/24" {
		t.Errorf("Target = %q", sc.Target)
	}
	if len(st.applied) != 1 {
		t.Fatalf("ApplyResult called %d times", len(st.applied))
	}
	got := st.applied[0].Devices
	if got[0].Hostname != "router.lan" {
		t.Errorf("hostname = %q, want the name without the trailing dot", got[0].Hostname)
	}
	if got[1].Hostname != "" {
		t.Errorf("a failed lookup left hostname %q", got[1].Hostname)
	}
}

func TestScannerKeepsExistingHostname(t *testing.T) {
	st := &fakeStore{}
	r := twoDevices()
	r.Devices[0].Hostname = "from-snmp"
	res := &fakeResolver{names: map[string]string{"192.168.1.1": "from-dns", "192.168.1.2": "b.lan"}}
	if _, err := newTestScanner(st, &fakeCollector{res: r}, res).Run(context.Background(), prefixes(t, "192.168.1.0/24")); err != nil {
		t.Fatal(err)
	}
	if st.applied[0].Devices[0].Hostname != "from-snmp" {
		t.Errorf("DNS overwrote a known hostname: %q", st.applied[0].Devices[0].Hostname)
	}
	if res.calls != 1 {
		t.Errorf("%d lookups, want 1: devices with a hostname need none", res.calls)
	}
}

func TestScannerDiscoveryErrorFailsScanWithoutApplying(t *testing.T) {
	st := &fakeStore{}
	sc, err := newTestScanner(st, &fakeCollector{err: errors.New("no probe could be sent")}, &fakeResolver{}).
		Run(context.Background(), prefixes(t, "192.168.1.0/24"))
	if err == nil {
		t.Fatal("Run() succeeded although discovery failed")
	}
	if sc.Status != "failed" || !strings.Contains(sc.Error, "no probe could be sent") || !strings.HasPrefix(sc.Error, "fake: ") {
		t.Errorf("scan = %+v", sc)
	}
	if len(st.applied) != 0 {
		t.Error("a failed scan changed the stored state")
	}
}

func TestScannerCancelDuringDiscovery(t *testing.T) {
	st := &fakeStore{}
	started := make(chan struct{})
	c := &fakeCollector{run: func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}}
	ctx, cancel := context.WithCancel(context.Background())
	type out struct {
		sc  model.Scan
		err error
	}
	done := make(chan out, 1)
	go func() {
		sc, err := newTestScanner(st, c, &fakeResolver{}).Run(ctx, prefixes(t, "192.168.1.0/24"))
		done <- out{sc, err}
	}()
	<-started
	cancel()

	select {
	case o := <-done:
		if !errors.Is(o.err, context.Canceled) {
			t.Errorf("Run() error = %v, want context.Canceled", o.err)
		}
		if o.sc.Status != "failed" || o.sc.Error != "canceled" {
			t.Errorf("scan = %+v, want failed/canceled", o.sc)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop within 2s of cancellation")
	}
	if len(st.applied) != 0 {
		t.Error("a canceled scan changed the stored state")
	}
	if st.finishCtxErr != nil {
		t.Errorf("FinishScan got a canceled context (%v): the failure could not be recorded", st.finishCtxErr)
	}
}

func TestScannerCancelDuringDNS(t *testing.T) {
	st := &fakeStore{}
	ctx, cancel := context.WithCancel(context.Background())
	res := &fakeResolver{block: true}
	s := newTestScanner(st, &fakeCollector{res: twoDevices()}, res)
	s.dnsWait = time.Hour
	go func() { time.Sleep(30 * time.Millisecond); cancel() }()

	start := time.Now()
	sc, err := s.Run(ctx, prefixes(t, "192.168.1.0/24"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() = %v, want context.Canceled", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Error("cancellation during DNS took longer than 2s")
	}
	if sc.Status != "failed" || sc.Error != "canceled" || len(st.applied) != 0 {
		t.Errorf("scan = %+v, applied = %d", sc, len(st.applied))
	}
}

func TestScannerDeadDNSCostsOneTimeout(t *testing.T) {
	st := &fakeStore{}
	r := Result{}
	for i := 1; i <= 200; i++ {
		r.Devices = append(r.Devices, model.DeviceInput{
			Key:    model.DeviceKey{MAC: "aa:aa:aa:aa:" + hex2(i/256) + ":" + hex2(i%256), IP: "192.168.1." + strconv.Itoa(i)},
			Source: "arp",
		})
	}
	s := newTestScanner(st, &fakeCollector{res: r}, &fakeResolver{block: true})
	s.dnsWait = 100 * time.Millisecond

	start := time.Now()
	sc, err := s.Run(context.Background(), prefixes(t, "192.168.1.0/24"))
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("200 unresolvable hosts took %v with a 100ms timeout: lookups are not concurrent", elapsed)
	}
	if sc.Status != "done" {
		t.Errorf("status = %q: a dead DNS server must not fail the scan", sc.Status)
	}
}

func TestScannerApplyErrorFailsScan(t *testing.T) {
	st := &fakeStore{applyErr: errors.New("disk full")}
	sc, err := newTestScanner(st, &fakeCollector{res: twoDevices()}, &fakeResolver{}).Run(context.Background(), prefixes(t, "192.168.1.0/24"))
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("Run() = %v", err)
	}
	if sc.Status != "failed" || !strings.Contains(sc.Error, "disk full") {
		t.Errorf("scan = %+v", sc)
	}
}

func TestScannerWithoutTargets(t *testing.T) {
	st := &fakeStore{}
	if _, err := newTestScanner(st, &fakeCollector{}, &fakeResolver{}).Run(context.Background(), nil); err == nil {
		t.Fatal("Run() without targets succeeded")
	}
	if st.finishCalls != 0 || st.scan.ID != 0 {
		t.Error("a scan record was created for an empty target list")
	}
}

func TestCleanHostname(t *testing.T) {
	long := strings.Repeat("a", 300)
	for in, want := range map[string]string{
		"router.lan.": "router.lan",
		"  nas.lan  ": "nas.lan",
		"plain":       "plain",
		"":            "",
		long:          long[:253],
	} {
		if got := cleanHostname(in); got != want {
			t.Errorf("cleanHostname(%.20q) = %.20q, want %.20q", in, got, want)
		}
	}
}

func TestCleanHostnameKeepsValidUTF8AndDropsControlCharacters(t *testing.T) {
	// "é" is two bytes: a cut at byte 253 would split it.
	split := strings.Repeat("a", 252) + "é"
	got := cleanHostname(split)
	if !utf8.ValidString(got) || len(got) > maxHostname {
		t.Errorf("cleanHostname(long name) = %d bytes, valid UTF-8 = %v", len(got), utf8.ValidString(got))
	}
	if got := cleanHostname("host\x1b[31m.lan\n"); got != "host[31m.lan" {
		t.Errorf("cleanHostname(escape sequence) = %q, want control characters removed", got)
	}
}

func hex2(i int) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[i>>4&0xf], digits[i&0xf]})
}

// fakeClock replaces the timer of the schedule. Every timer it hands out is kept, so a
// test sees the waits the scheduler asked for and fires them one by one.
type fakeClock struct {
	mu    sync.Mutex
	waits []time.Duration
	chans []chan time.Time
}

func (c *fakeClock) timer(d time.Duration) (<-chan time.Time, func() bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.waits = append(c.waits, d)
	c.chans = append(c.chans, ch)
	return ch, func() bool { return true }
}

func (c *fakeClock) requested() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.waits...)
}

// fire ends the latest timer, as if its interval had passed.
func (c *fakeClock) fire() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.chans[len(c.chans)-1] <- time.Now()
}

// lockedBuffer is a log sink the scheduler goroutine and the test can share.
type lockedBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func scheduleConfig(interval time.Duration, targets ...string) config.Config {
	return config.Config{Scan: config.ScanConfig{Interval: config.Duration(interval), Targets: targets}}
}

// startSchedule runs the scheduler of a scanner on a fake clock until the test ends.
func startSchedule(t *testing.T, s *Scanner, targets func(config.Config) ([]netip.Prefix, error)) *fakeClock {
	t.Helper()
	clock := &fakeClock{}
	s.timer = clock.timer
	s.wake = make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Schedule(ctx, targets)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("Schedule did not stop when its context ended")
		}
	})
	return clock
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func startedScans(st *fakeStore) int {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.starts
}

var lanTargets = func(config.Config) ([]netip.Prefix, error) {
	return []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")}, nil
}

func TestScheduleStartsAScanEveryInterval(t *testing.T) {
	st := &fakeStore{}
	s := newTestScanner(st, &fakeCollector{}, &fakeResolver{})
	s.cfg = scheduleConfig(15 * time.Minute)
	clock := startSchedule(t, s, lanTargets)

	waitUntil(t, "the first timer", func() bool { return len(clock.requested()) == 1 })
	if got := clock.requested()[0]; got != 15*time.Minute {
		t.Errorf("first wait = %v, want 15m", got)
	}
	if n := startedScans(st); n != 0 {
		t.Errorf("%d scans before the first tick", n)
	}
	clock.fire()
	waitUntil(t, "the scheduled scan", func() bool { return startedScans(st) == 1 })
	waitUntil(t, "the next timer", func() bool { return len(clock.requested()) == 2 })
}

func TestScheduleTakesANewIntervalWithoutARestart(t *testing.T) {
	s := newTestScanner(&fakeStore{}, &fakeCollector{}, &fakeResolver{})
	s.cfg = scheduleConfig(15 * time.Minute)
	clock := startSchedule(t, s, lanTargets)
	waitUntil(t, "the first timer", func() bool { return len(clock.requested()) == 1 })

	s.SetConfig(scheduleConfig(30 * time.Minute))
	waitUntil(t, "the timer with the new interval", func() bool { return len(clock.requested()) == 2 })
	if got := clock.requested()[1]; got != 30*time.Minute {
		t.Errorf("wait after the change = %v, want 30m", got)
	}

	// A reload that leaves the interval alone does not restart the wait.
	s.SetConfig(scheduleConfig(30*time.Minute, "10.0.0.0/24"))
	time.Sleep(50 * time.Millisecond)
	if n := len(clock.requested()); n != 2 {
		t.Errorf("%d timers, a reload without a new interval restarted the wait", n)
	}
}

func TestScheduleIntervalZeroTurnsItOffAndBackOn(t *testing.T) {
	s := newTestScanner(&fakeStore{}, &fakeCollector{}, &fakeResolver{})
	s.cfg = scheduleConfig(0)
	clock := startSchedule(t, s, lanTargets)
	time.Sleep(50 * time.Millisecond)
	if n := len(clock.requested()); n != 0 {
		t.Fatalf("%d timers with interval 0", n)
	}
	s.SetConfig(scheduleConfig(time.Hour))
	waitUntil(t, "a timer after the schedule is turned on", func() bool { return len(clock.requested()) == 1 })
}

func TestScheduleAsksForTargetsOfTheConfigInForce(t *testing.T) {
	st := &fakeStore{}
	s := newTestScanner(st, &fakeCollector{}, &fakeResolver{})
	s.cfg = scheduleConfig(time.Hour, "192.168.1.0/24")
	var mu sync.Mutex
	var seen [][]string
	clock := startSchedule(t, s, func(c config.Config) ([]netip.Prefix, error) {
		mu.Lock()
		seen = append(seen, c.Scan.Targets)
		mu.Unlock()
		return lanTargets(c)
	})
	waitUntil(t, "the first timer", func() bool { return len(clock.requested()) == 1 })
	s.SetConfig(scheduleConfig(time.Hour, "10.0.0.0/24"))
	clock.fire()
	waitUntil(t, "the scheduled scan", func() bool { return startedScans(st) == 1 })
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 || len(seen[0]) != 1 || seen[0][0] != "10.0.0.0/24" {
		t.Errorf("targets asked for config %v, want the reloaded one", seen)
	}
}

func TestScheduleSkipsATickWhileAScanRuns(t *testing.T) {
	st := &fakeStore{}
	release := make(chan struct{})
	entered := make(chan struct{}, 2)
	c := &fakeCollector{run: func(ctx context.Context) error {
		entered <- struct{}{}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	var logs lockedBuffer
	s := newTestScanner(st, c, &fakeResolver{})
	s.log = slog.New(slog.NewTextHandler(&logs, nil))
	s.cfg = scheduleConfig(time.Hour)
	clock := startSchedule(t, s, lanTargets)
	t.Cleanup(func() { close(release) })

	waitUntil(t, "the first timer", func() bool { return len(clock.requested()) == 1 })
	clock.fire()
	<-entered // the first scan is running
	waitUntil(t, "the second timer", func() bool { return len(clock.requested()) == 2 })
	clock.fire()
	waitUntil(t, "the third timer", func() bool { return len(clock.requested()) == 3 })

	if n := startedScans(st); n != 1 {
		t.Errorf("%d scans started, want 1: the tick during a running scan must be skipped", n)
	}
	if !strings.Contains(logs.String(), "skipped") {
		t.Errorf("the skipped tick is not in the log: %q", logs.String())
	}
}

// configSpy is an enricher that records the config its scan runs with.
type configSpy struct {
	mu   sync.Mutex
	seen []string
}

func (*configSpy) Name() string { return "spy" }

func (c *configSpy) Collect(_ context.Context, in Input) (Result, error) {
	c.mu.Lock()
	c.seen = append(c.seen, strings.Join(in.Config.Scan.Targets, ","))
	c.mu.Unlock()
	return Result{}, nil
}

func TestARunningScanKeepsItsConfigAndTheNextOneTakesTheNew(t *testing.T) {
	st := &fakeStore{}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	discovery := &fakeCollector{run: func(ctx context.Context) error {
		first := false
		once.Do(func() { first = true; close(entered) })
		if !first {
			return nil // only the first scan is held back
		}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	spy := &configSpy{}
	s := newTestScanner(st, discovery, &fakeResolver{})
	s.enrichers = []Collector{spy}
	s.cfg = scheduleConfig(time.Hour, "192.168.1.0/24")
	targets := []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")}

	if _, err := s.Start(context.Background(), targets); err != nil {
		t.Fatal(err)
	}
	<-entered
	s.SetConfig(scheduleConfig(time.Hour, "10.0.0.0/24")) // the reload arrives mid-scan
	close(release)
	waitUntil(t, "the first scan to end", func() bool { return !s.busy.Load() })

	if _, err := s.Run(context.Background(), targets); err != nil {
		t.Fatal(err)
	}
	spy.mu.Lock()
	defer spy.mu.Unlock()
	if len(spy.seen) != 2 || spy.seen[0] != "192.168.1.0/24" || spy.seen[1] != "10.0.0.0/24" {
		t.Errorf("configs seen by the two scans = %v, want the old one then the new one", spy.seen)
	}
}

func TestMergeServicesKeepsTheNameAndTheBannerOfOneService(t *testing.T) {
	key := model.DeviceKey{MAC: "aa:aa:aa:aa:aa:01", IP: "192.168.1.1"}
	base := []model.ServiceInput{
		{Device: key, Port: 80, Proto: "tcp", Name: "http"},
		{Device: key, Port: 22, Proto: "tcp", Name: "ssh"},
	}
	// The HTTP stage reports the same service again, with a banner.
	got := mergeServices(base, []model.ServiceInput{
		{Device: key, Port: 80, Proto: "tcp", Banner: "nginx"},
		{Device: key, Port: 8080, Proto: "tcp", Name: "http-alt", Banner: "lighttpd"},
	})
	if len(got) != 3 {
		t.Fatalf("services = %+v, want three, one per port", got)
	}
	if got[0].Name != "http" || got[0].Banner != "nginx" {
		t.Errorf("port 80 = %+v, want its name kept and the banner added", got[0])
	}
	if got[2].Port != 8080 || got[2].Banner != "lighttpd" {
		t.Errorf("port 8080 = %+v", got[2])
	}
}
