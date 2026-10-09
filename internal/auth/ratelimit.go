package auth

import (
	"sync"
	"time"
)

const maxLimiterKeys = 100_000

// Limiter allows at most limit events per key within a sliding window.
type Limiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	now    func() time.Time
	hits   map[string][]time.Time
}

// NewLimiter returns a limiter of limit events per window. A nil now means time.Now.
func NewLimiter(limit int, window time.Duration, now func() time.Time) *Limiter {
	if now == nil {
		now = time.Now
	}
	return &Limiter{limit: limit, window: window, now: now, hits: make(map[string][]time.Time)}
}

// Allow records an event for key. When the limit is reached it records nothing
// and returns how long until the oldest event leaves the window.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	cutoff := now.Add(-l.window)
	times := l.hits[key]
	i := 0
	for i < len(times) && !times[i].After(cutoff) {
		i++
	}
	times = times[i:]

	if len(times) >= l.limit {
		l.hits[key] = times
		return false, times[0].Add(l.window).Sub(now)
	}
	if _, known := l.hits[key]; !known && len(l.hits) >= maxLimiterKeys {
		l.evict(cutoff)
	}
	l.hits[key] = append(times, now)
	return true, 0
}

// evict frees room for a new key: expired keys first, then an arbitrary one.
func (l *Limiter) evict(cutoff time.Time) {
	for k, times := range l.hits {
		if len(times) == 0 || !times[len(times)-1].After(cutoff) {
			delete(l.hits, k)
		}
	}
	if len(l.hits) < maxLimiterKeys {
		return
	}
	for k := range l.hits {
		delete(l.hits, k)
		return
	}
}
