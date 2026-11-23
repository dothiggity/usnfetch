package engine

import (
	"testing"
	"time"
)

// A worker stopped while it waits gives its reservation back: the next download must
// not sit at 0% paying for articles that were never fetched.
func TestLimiterRefundsStoppedWaits(t *testing.T) {
	l := NewLimiter()
	l.SetRate(1000) // 1000 B/s
	stopped := make(chan struct{})
	close(stopped)
	for i := 0; i < 4; i++ { // four workers of a stopped job, an article each
		if l.Wait(stopped, 5000) {
			t.Fatal("a stopped wait reported success")
		}
	}
	t0 := time.Now()
	if !l.Wait(make(chan struct{}), 100) {
		t.Fatal("wait failed")
	}
	if d := time.Since(t0); d > 500*time.Millisecond {
		t.Fatalf("the next job waited %v for bytes nobody fetched", d)
	}
}
