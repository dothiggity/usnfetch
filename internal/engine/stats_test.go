package engine

import (
	"testing"
	"time"
)

// The speed comes from what arrived in the last 10 seconds: one sample is enough (slow
// downloads of small files), and a stall reads 0, not the speed from before it.
func TestStatsRate(t *testing.T) {
	s := NewStats(1000, 0)
	if r := s.Rate(); r != 0 {
		t.Errorf("nothing yet: %v", r)
	}
	s.Start = time.Now().Add(-4 * time.Second)
	s.Add(4000)
	if r := s.Rate(); r < 900 || r > 1100 {
		t.Errorf("4000 B four seconds in, one sample: %.0f B/s, want about 1000", r)
	}
	s.Start = time.Now().Add(-time.Minute) // long running: over the last 10 s only
	if r := s.Rate(); r < 350 || r > 450 {
		t.Errorf("4000 B in the last 10 s: %.0f B/s, want about 400", r)
	}
	s.mu.Lock()
	s.window[0].t = time.Now().Add(-11 * time.Second) // nothing since
	s.mu.Unlock()
	if r := s.Rate(); r != 0 {
		t.Errorf("stalled for 11 s: %.0f B/s, want 0", r)
	}
	s2 := NewStats(10, 0)
	s2.Add(5000) // a burst right at the start: over at least a second, not a spike
	if r := s2.Rate(); r > 5000 {
		t.Errorf("burst at the start: %.0f B/s", r)
	}
}
