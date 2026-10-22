package collector

import (
	"context"
	"strings"
	"testing"
)

func TestAPanicInDiscoveryFailsTheScanAndKeepsTheProcess(t *testing.T) {
	st := &fakeStore{}
	col := &fakeCollector{run: func(context.Context) error { panic("bad packet") }}
	sc := newTestScanner(st, col, &fakeResolver{})

	if _, err := sc.Start(context.Background(), prefixes(t, "192.168.1.0/24")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the scan to end", func() bool { return !sc.busy.Load() })

	st.mu.Lock()
	defer st.mu.Unlock()
	if st.scan.Status != "failed" || !strings.Contains(st.scan.Error, "panicked") {
		t.Errorf("scan = %+v, want it failed with the panic named", st.scan)
	}
}

func TestAPanicInAnEnricherCostsOnlyThatSource(t *testing.T) {
	st := &fakeStore{}
	sc := newTestScanner(st, &fakeCollector{res: twoDevices()}, &fakeResolver{})
	sc.enrichers = []Collector{&fakeCollector{run: func(context.Context) error { panic("bad answer") }}}

	if _, err := sc.Start(context.Background(), prefixes(t, "192.168.1.0/24")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the scan to end", func() bool { return !sc.busy.Load() })

	st.mu.Lock()
	defer st.mu.Unlock()
	if st.scan.Status != "done" {
		t.Errorf("scan = %+v, want it done: one enricher failing must not fail the scan", st.scan)
	}
}
