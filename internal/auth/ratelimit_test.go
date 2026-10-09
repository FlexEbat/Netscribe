package auth

import (
	"strconv"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newClock() *fakeClock { return &fakeClock{t: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)} }

func TestLimiterAllowsTenPerMinuteThenRefuses(t *testing.T) {
	clk := newClock()
	l := NewLimiter(10, time.Minute, clk.now)
	for i := 1; i <= 10; i++ {
		if ok, _ := l.Allow("1.2.3.4"); !ok {
			t.Fatalf("attempt %d refused", i)
		}
	}
	ok, retry := l.Allow("1.2.3.4")
	if ok {
		t.Fatal("the 11th attempt within a minute was allowed")
	}
	if retry <= 0 || retry > time.Minute {
		t.Errorf("retry after = %v, want within (0, 1m]", retry)
	}
}

func TestLimiterRetryAfterTracksTheOldestEvent(t *testing.T) {
	clk := newClock()
	l := NewLimiter(2, time.Minute, clk.now)
	l.Allow("k")
	clk.advance(20 * time.Second)
	l.Allow("k")
	clk.advance(10 * time.Second)
	_, retry := l.Allow("k")
	if retry != 30*time.Second {
		t.Errorf("retry after = %v, want 30s (the first event leaves the window 30s from now)", retry)
	}
}

func TestLimiterRecoversAfterTheWindow(t *testing.T) {
	clk := newClock()
	l := NewLimiter(3, time.Minute, clk.now)
	for range 3 {
		l.Allow("k")
	}
	if ok, _ := l.Allow("k"); ok {
		t.Fatal("limit not enforced")
	}
	clk.advance(time.Minute)
	if ok, _ := l.Allow("k"); !ok {
		t.Error("still refused after the window passed")
	}
}

func TestLimiterRefusedAttemptsDoNotExtendTheBlock(t *testing.T) {
	clk := newClock()
	l := NewLimiter(2, time.Minute, clk.now)
	l.Allow("k")
	l.Allow("k")
	for range 50 { // hammering while blocked
		clk.advance(time.Second)
		l.Allow("k")
	}
	// 50 s passed since the start of the block: the first two events are 52 s old,
	// so the block must end within 8 s, not restart with every refused attempt.
	clk.advance(10 * time.Second)
	if ok, _ := l.Allow("k"); !ok {
		t.Error("refused attempts were counted and kept the block alive")
	}
}

func TestLimiterKeysAreIndependent(t *testing.T) {
	l := NewLimiter(1, time.Minute, newClock().now)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("first key refused")
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Error("an exhausted key blocked another one")
	}
	if ok, _ := l.Allow("a"); ok {
		t.Error("the first key was not blocked")
	}
}

func TestLimiterMemoryIsBounded(t *testing.T) {
	clk := newClock()
	l := NewLimiter(10, time.Minute, clk.now)
	for i := 0; i < maxLimiterKeys+500; i++ {
		l.Allow("ip-" + strconv.Itoa(i))
	}
	if n := len(l.hits); n > maxLimiterKeys {
		t.Errorf("%d tracked keys, the cap is %d", n, maxLimiterKeys)
	}
	if ok, _ := l.Allow("brand-new"); !ok {
		t.Error("a new key is refused at the cap: the limiter fails closed for everybody")
	}
}

func TestLimiterEvictsExpiredKeysFirst(t *testing.T) {
	clk := newClock()
	l := NewLimiter(10, time.Minute, clk.now)
	for i := 0; i < maxLimiterKeys; i++ {
		l.Allow("old-" + strconv.Itoa(i))
	}
	clk.advance(2 * time.Minute)
	l.Allow("fresh")
	if n := len(l.hits); n != 1 {
		t.Errorf("%d keys remain, want only the fresh one", n)
	}
}

func TestNewLimiterDefaultsToRealTime(t *testing.T) {
	l := NewLimiter(1, time.Minute, nil)
	if ok, _ := l.Allow("k"); !ok {
		t.Error("first attempt refused")
	}
}
