package engine

import (
	"sync"
	"time"
)

// Limiter caps the download rate across every job (a token bucket, filled at the
// rate, holding up to one second's worth). Workers reserve an article's size before
// fetching it and sleep off any debt, so the rate holds across all connections.
type Limiter struct {
	mu      sync.Mutex
	rate    float64 // bytes per second; 0 = unlimited
	tokens  float64
	last    time.Time
	changed chan struct{} // closed (and replaced) when the rate changes, to wake sleepers
}

// DownloadLimit is the limiter downloads go through (streaming to the browser does not).
var DownloadLimit = NewLimiter()

func NewLimiter() *Limiter { return &Limiter{changed: make(chan struct{}), last: time.Now()} }

// SetRate sets the cap in bytes per second (<= 0: unlimited).
func (l *Limiter) SetRate(bps float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if bps < 0 {
		bps = 0
	}
	if bps == l.rate {
		return
	}
	l.rate, l.tokens, l.last = bps, 0, time.Now()
	close(l.changed)
	l.changed = make(chan struct{})
}

// Rate: the current cap in bytes per second (0 = unlimited).
func (l *Limiter) Rate() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rate
}

// Wait reserves n bytes and sleeps until the rate allows them. False when stop closed first.
func (l *Limiter) Wait(stop <-chan struct{}, n int64) bool {
	l.mu.Lock()
	if l.rate <= 0 {
		l.mu.Unlock()
		return true
	}
	now := time.Now()
	l.tokens = min(l.tokens+now.Sub(l.last).Seconds()*l.rate, l.rate)
	l.last = now
	l.tokens -= float64(n)
	if l.tokens >= 0 {
		l.mu.Unlock()
		return true
	}
	wait := time.Duration(-l.tokens / l.rate * float64(time.Second))
	changed := l.changed
	l.mu.Unlock()
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-stop:
		return false
	case <-changed: // a new rate: go on (a lower one throttles the next reservation)
		return true
	case <-t.C:
		return true
	}
}
