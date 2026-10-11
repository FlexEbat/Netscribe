package collector

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) Publish(name string, _ any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, name)
}

func (r *recorder) names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestStartReturnsWhileTheScanRunsAndRejectsASecondOne(t *testing.T) {
	st := &fakeStore{}
	release := make(chan struct{})
	col := &fakeCollector{res: twoDevices(), run: func(ctx context.Context) error {
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	sc := newTestScanner(st, col, &fakeResolver{})
	rec := &recorder{}
	sc.SetEvents(rec)

	scan, err := sc.Start(context.Background(), prefixes(t, "192.168.1.0/24"))
	if err != nil {
		t.Fatal(err)
	}
	if scan.ID != 7 || scan.Status != "running" {
		t.Errorf("scan = %+v, want the running scan", scan)
	}
	if _, err := sc.Start(context.Background(), prefixes(t, "192.168.1.0/24")); !errors.Is(err, ErrBusy) {
		t.Errorf("second Start() error = %v, want ErrBusy", err)
	}
	if _, err := sc.Run(context.Background(), prefixes(t, "192.168.1.0/24")); !errors.Is(err, ErrBusy) {
		t.Errorf("Run() during a scan error = %v, want ErrBusy", err)
	}

	close(release)
	waitFor(t, "the scan to finish", func() bool { return !sc.busy.Load() })

	want := []string{"scan.started", "scan.progress", "scan.progress", "scan.progress", "scan.finished", "topology.changed"}
	got := rec.names()
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %q, want %q", i, got[i], want[i])
		}
	}
	if st.scan.Status != "done" {
		t.Errorf("scan status = %q, want done", st.scan.Status)
	}
	if _, err := sc.Start(context.Background(), prefixes(t, "192.168.1.0/24")); err != nil {
		t.Errorf("Start() after the scan ended: %v", err)
	}
}

func TestStartedScanStopsWithItsContext(t *testing.T) {
	st := &fakeStore{}
	col := &fakeCollector{run: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	sc := newTestScanner(st, col, &fakeResolver{})
	rec := &recorder{}
	sc.SetEvents(rec)

	ctx, cancel := context.WithCancel(context.Background())
	if _, err := sc.Start(ctx, prefixes(t, "192.168.1.0/24")); err != nil {
		t.Fatal(err)
	}
	cancel()
	waitFor(t, "the canceled scan to end", func() bool { return !sc.busy.Load() })

	st.mu.Lock()
	status, msg := st.scan.Status, st.scan.Error
	st.mu.Unlock()
	if status != "failed" || msg != "canceled" {
		t.Errorf("scan = %q %q, want failed canceled", status, msg)
	}
	names := rec.names()
	if names[len(names)-1] != "scan.finished" {
		t.Errorf("events = %v, want scan.finished last and no topology.changed", names)
	}
	for _, n := range names {
		if n == "topology.changed" {
			t.Error("topology.changed published for a failed scan")
		}
	}
}

func TestNoTargetsIsReportedAndDoesNotTakeTheSlot(t *testing.T) {
	sc := newTestScanner(&fakeStore{}, &fakeCollector{}, &fakeResolver{})
	if _, err := sc.Start(context.Background(), nil); !errors.Is(err, ErrNoTargets) {
		t.Errorf("Start(nil) error = %v, want ErrNoTargets", err)
	}
	if sc.busy.Load() {
		t.Error("the scan slot stayed taken after a rejected start")
	}
}
