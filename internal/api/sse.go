package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/FlexEbat/Netscribe/internal/auth"
)

const maxStreamsPerUser = 5 // section 9.5 of the spec

// heartbeat is the time between keep-alive comments. Each one also checks again that the
// credential behind the stream is valid. It is a variable so that a test can shorten it.
var heartbeat = 25 * time.Second

// streamCounter limits the open event streams of each user.
type streamCounter struct {
	mu sync.Mutex
	n  map[int64]int
}

func newStreamCounter() *streamCounter { return &streamCounter{n: make(map[int64]int)} }

func (c *streamCounter) acquire(userID int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.n[userID] >= maxStreamsPerUser {
		return false
	}
	c.n[userID]++
	return true
}

func (c *streamCounter) release(userID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.n[userID]--; c.n[userID] <= 0 {
		delete(c.n, userID)
	}
}

// streamEvents answers GET /api/events with a server-sent event stream until the client leaves.
func (s *server) streamEvents(w http.ResponseWriter, r *http.Request) {
	p, _ := principal(r)
	rc := http.NewResponseController(w)
	// The stream outlives the server's write timeout, so the deadline is lifted for this response only.
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		s.internalError(w, "lift write deadline", err)
		return
	}
	if !s.streams.acquire(p.User.ID) {
		writeError(w, http.StatusTooManyRequests, "too many open event streams")
		return
	}
	defer s.streams.release(p.User.ID)

	ch, cancel := s.hub.Subscribe()
	defer cancel()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if err := rc.Flush(); err != nil {
		return
	}

	tick := time.NewTicker(heartbeat)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			// A stream outlives the request that opened it, so a session that was closed,
			// a disabled account or a revoked token must end it, and a changed role must
			// apply to what it carries.
			next, err := s.reauthenticate(r)
			switch {
			case errors.Is(err, auth.ErrUnauthenticated):
				return
			case err != nil:
				s.log.Warn("recheck event stream", "err", err) // the store may recover, keep the stream
			default:
				p = next
			}
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
		case e, ok := <-ch:
			if !ok {
				return
			}
			if e.Perm != "" && !p.Can(e.Perm) {
				continue
			}
			data, err := json.Marshal(e.Data)
			if err != nil {
				s.log.Error("encode event", "event", e.Name, "err", err)
				continue
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Name, data); err != nil {
				return
			}
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}
