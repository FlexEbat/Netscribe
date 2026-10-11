// Package events fans server events out to the open event streams.
package events

import "sync"

// Event is one named message. Data is encoded as JSON by the stream.
type Event struct {
	Name string
	Data any
}

// subscriberBuffer is how many events a slow stream may fall behind before it loses some.
const subscriberBuffer = 64

// Hub delivers every published event to every subscriber.
type Hub struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
}

// NewHub returns an empty hub.
func NewHub() *Hub {
	return &Hub{subs: make(map[chan Event]struct{})}
}

// Subscribe returns a channel of events and a function that ends the subscription.
// The function closes the channel, is safe to call more than once and leaves no
// goroutine behind.
func (h *Hub) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, subscriberBuffer)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, ch)
			h.mu.Unlock()
			close(ch)
		})
	}
}

// Publish sends an event to all subscribers. It never blocks: a subscriber whose
// buffer is full misses the event, because one stuck browser must not stall a scan.
func (h *Hub) Publish(name string, data any) {
	e := Event{Name: name, Data: data}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- e:
		default:
		}
	}
}
