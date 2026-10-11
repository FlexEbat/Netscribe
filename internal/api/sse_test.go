package api

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/events"
)

// openStream connects to /api/events of a real test server with the given session.
func openStream(t *testing.T, srv *httptest.Server, s session) (*http.Response, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/events", nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	req.AddCookie(s.cookie)
	resp, err := srv.Client().Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	return resp, func() { cancel(); _ = resp.Body.Close() }
}

func streamEnv(t *testing.T) (*env, *events.Hub, *httptest.Server) {
	t.Helper()
	hub := events.NewHub()
	e := newEnv(t, func(o *envOptions) { o.hub = hub })
	srv := httptest.NewServer(e.handler)
	t.Cleanup(srv.Close)
	return e, hub, srv
}

func TestEventStreamNeedsASession(t *testing.T) {
	_, _, srv := streamEnv(t)
	resp, err := http.Get(srv.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestEventStreamDeliversEvents(t *testing.T) {
	e, hub, srv := streamEnv(t)
	resp, closeStream := openStream(t, srv, e.mustLogin("admin"))
	defer closeStream()

	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("status %d, content type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", resp.Header.Get("Cache-Control"))
	}

	lines := make(chan string, 8)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()

	// The stream is subscribed once the headers arrived, so this event cannot be missed.
	hub.Publish("scan.progress", map[string]any{"scanId": 3, "stage": "dns", "done": 2, "total": 3})
	want := []string{"event: scan.progress", `data: {"done":2,"scanId":3,"stage":"dns","total":3}`}
	for _, w := range want {
		select {
		case got := <-lines:
			if got != w {
				t.Errorf("line = %q, want %q", got, w)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("no line %q within 2s", w)
		}
	}
}

func TestEventStreamLimitPerUserAndRelease(t *testing.T) {
	e, _, srv := streamEnv(t)
	s := e.mustLogin("admin")

	var closers []context.CancelFunc
	for i := 0; i < maxStreamsPerUser; i++ {
		resp, c := openStream(t, srv, s)
		closers = append(closers, c)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("stream %d: status %d", i+1, resp.StatusCode)
		}
	}
	resp, c := openStream(t, srv, s)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("stream %d: status %d, want 429", maxStreamsPerUser+1, resp.StatusCode)
	}
	c()

	// Closing one stream frees its slot.
	closers[0]()
	deadline := time.Now().Add(2 * time.Second)
	for {
		resp, c := openStream(t, srv, s)
		code := resp.StatusCode
		if code == http.StatusOK {
			closers = append(closers, c)
			break
		}
		c()
		if time.Now().After(deadline) {
			t.Fatalf("slot not released: status %d", code)
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, c := range closers[1:] {
		c()
	}
}

func TestStreamCounter(t *testing.T) {
	c := newStreamCounter()
	for i := 0; i < maxStreamsPerUser; i++ {
		if !c.acquire(1) {
			t.Fatalf("acquire %d refused", i+1)
		}
	}
	if c.acquire(1) {
		t.Error("acquired beyond the limit")
	}
	if !c.acquire(2) {
		t.Error("another user was refused")
	}
	c.release(1)
	if !c.acquire(1) {
		t.Error("slot not freed by release")
	}
}
