package events

import (
	"runtime"
	"testing"
	"time"
)

func receive(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case e, ok := <-ch:
		if !ok {
			t.Fatal("channel closed, want an event")
		}
		return e
	case <-time.After(time.Second):
		t.Fatal("no event within a second")
		return Event{}
	}
}

func TestEverysubscriberGetsEveryEvent(t *testing.T) {
	h := NewHub()
	a, cancelA := h.Subscribe()
	b, cancelB := h.Subscribe()
	defer cancelA()
	defer cancelB()

	h.Publish("scan.started", 1)
	for _, ch := range []<-chan Event{a, b} {
		if e := receive(t, ch); e.Name != "scan.started" || e.Data != 1 {
			t.Errorf("event = %+v", e)
		}
	}
}

func TestCancelStopsDeliveryAndClosesTheChannel(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Subscribe()
	cancel()
	cancel() // a second call must not panic

	h.Publish("late", nil)
	if _, ok := <-ch; ok {
		t.Error("channel still open after cancel")
	}
}

func TestSlowSubscriberDoesNotBlockPublish(t *testing.T) {
	h := NewHub()
	_, cancel := h.Subscribe() // never read
	defer cancel()

	done := make(chan struct{})
	go func() {
		for i := 0; i < subscriberBuffer*3; i++ {
			h.Publish("x", i)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a full subscriber")
	}
}

func TestSubscribeLeavesNoGoroutineBehind(t *testing.T) {
	h := NewHub()
	before := runtime.NumGoroutine()
	for i := 0; i < 50; i++ {
		_, cancel := h.Subscribe()
		h.Publish("x", i)
		cancel()
	}
	if after := runtime.NumGoroutine(); after > before+2 {
		t.Errorf("goroutines grew from %d to %d", before, after)
	}
}
