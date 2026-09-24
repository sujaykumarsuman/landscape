package server

import (
	"math"
	"sync"
	"time"
)

// Failed logins are capped process-wide — the admin password is shared with
// other consoles (kubescope), so guessing it here must be as slow as there. Not
// per client: behind Traefik the client address is only as good as its
// headers. A burst of 10, then one more every 6 s (10/min); successful logins
// never spend the budget.
const (
	loginFailBurst = 10
	loginFailEvery = 6 * time.Second
)

// loginFailDelay slows each failed guess a little further (a var so tests can
// zero it).
var loginFailDelay = 400 * time.Millisecond

// failLimiter is a token bucket spent by failed logins only.
type failLimiter struct {
	mu     sync.Mutex
	tokens float64
	burst  float64
	every  time.Duration
	last   time.Time
	now    func() time.Time
}

func newFailLimiter(now func() time.Time) *failLimiter {
	return &failLimiter{tokens: loginFailBurst, burst: loginFailBurst, every: loginFailEvery, now: now}
}

func (l *failLimiter) refill() {
	now := l.now()
	if !l.last.IsZero() {
		l.tokens = math.Min(l.burst, l.tokens+float64(now.Sub(l.last))/float64(l.every))
	}
	l.last = now
}

// peek reports whether a login may be attempted, else how long until one may.
func (l *failLimiter) peek() (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refill()
	if l.tokens >= 1 {
		return true, 0
	}
	return false, time.Duration((1 - l.tokens) * float64(l.every))
}

// spend records one failed login.
func (l *failLimiter) spend() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refill()
	l.tokens = math.Max(0, l.tokens-1)
}
