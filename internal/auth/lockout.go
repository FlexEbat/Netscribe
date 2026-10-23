package auth

import (
	"sync"
	"time"
)

// maxLockEntries bounds the memory the lock table can take. Every failed attempt for a
// new address and username creates an entry, and anyone can send those.
const maxLockEntries = 10_000

// attemptLock counts consecutive failed logins per key (an address and a username
// together) and locks the key for a while after too many. It lives in memory: a restart
// forgets the counters, which gives an attacker nothing beyond a fresh set of attempts
// that the per-address limit of the API layer still caps.
//
// Keying by the pair, and not by the username alone, is the point: whoever knows only a
// username cannot lock the real owner out from another address.
type attemptLock struct {
	mu      sync.Mutex
	max     int
	lockFor time.Duration
	entries map[string]*lockEntry
}

type lockEntry struct {
	fails int
	until time.Time // zero while not locked
	seen  time.Time // last failure, to drop stale counters
}

func newAttemptLock(maxFails int, lockFor time.Duration) *attemptLock {
	return &attemptLock{max: maxFails, lockFor: lockFor, entries: make(map[string]*lockEntry)}
}

// lockKey joins the address and the normalized username.
func lockKey(ip, username string) string { return ip + "\x00" + NormalizeUsername(username) }

// locked reports whether the key is locked at now.
func (l *attemptLock) locked(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.entries[key]
	return e != nil && now.Before(e.until)
}

// fail records one failed attempt and locks the key on the max-th in a row.
func (l *attemptLock) fail(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	e := l.entries[key]
	if e == nil {
		l.makeRoom(now)
		e = &lockEntry{}
		l.entries[key] = e
	}
	e.seen = now
	e.fails++
	if e.fails >= l.max {
		e.fails = 0
		e.until = now.Add(l.lockFor)
	}
}

// reset forgets the key after a successful login.
func (l *attemptLock) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

// makeRoom drops entries that no longer matter once the table is full: locks that ended
// and counters that have been idle for a whole lock period. If the table is still full
// the unlocked counters go, since a lock is what protects an account, and only then
// active locks.
func (l *attemptLock) makeRoom(now time.Time) {
	if len(l.entries) < maxLockEntries {
		return
	}
	for k, e := range l.entries {
		if !now.Before(e.until) && now.Sub(e.seen) >= l.lockFor {
			delete(l.entries, k)
		}
	}
	if len(l.entries) < maxLockEntries {
		return
	}
	for k, e := range l.entries {
		if !now.Before(e.until) {
			delete(l.entries, k)
		}
	}
	// Only active locks are left. The table must stay bounded however many addresses an
	// attacker comes from, so some of them go too (map order is random).
	for k := range l.entries {
		if len(l.entries) < maxLockEntries {
			return
		}
		delete(l.entries, k)
	}
}
