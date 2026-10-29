package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/events"
	"github.com/FlexEbat/Netscribe/internal/model"
)

type scanRecorder struct {
	mu   sync.Mutex
	scan []model.Scan
}

func (r *scanRecorder) Handle(_ context.Context, s model.Scan) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scan = append(r.scan, s)
}

func (r *scanRecorder) ids() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []int64
	for _, s := range r.scan {
		out = append(out, s.ID)
	}
	return out
}

func TestNotifyOnScansHandlesOnlyFinishedScans(t *testing.T) {
	hub := events.NewHub()
	rec := &scanRecorder{}
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { notifyOnScans(ctx, hub, rec); close(done) }()

	// Subscribe happens inside the goroutine: publish until the first scan is seen.
	deadline := time.Now().Add(2 * time.Second)
	for len(rec.ids()) == 0 && time.Now().Before(deadline) {
		hub.Publish("topology.changed", struct{}{})
		hub.Publish("scan.progress", model.Scan{ID: 99})
		hub.Publish("scan.finished", model.Scan{ID: 5, Status: "done"})
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	<-done

	for _, id := range rec.ids() {
		if id != 5 {
			t.Fatalf("handled scan %d, only the finished scan 5 may be handled", id)
		}
	}
	if len(rec.ids()) == 0 {
		t.Fatal("the finished scan was never handled")
	}
}
