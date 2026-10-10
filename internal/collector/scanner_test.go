package collector

import (
	"context"
	"errors"
	"io"
	"log/slog"
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
	// finishCtxErr records ctx.Err() seen by FinishScan: it must be nil even after cancellation.
	finishCtxErr error
}

func (s *fakeStore) StartScan(_ context.Context, target string) (model.Scan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
