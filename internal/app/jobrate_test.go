package app

import (
	"testing"
	"time"
)

// A job's speed comes from the bytes it moved across all its files, so a job of many small
// files (a task per file, each too short to have a speed of its own) still shows one. Samples
// from before a gap (a pause, nobody looking) are dropped rather than averaged in.
func TestJobRate(t *testing.T) {
	j := &Job{State: "downloading"}
	if r := j.jobRate(0, 123); r != 123 {
		t.Errorf("nothing to go on yet: %v, want the task's 123", r)
	}
	j.seen[0].t = time.Now().Add(-4 * time.Second) // 4 s ago it had moved nothing
	if r := j.jobRate(20000, 0); r < 4700 || r > 5300 {
		t.Errorf("20000 B in 4 s: %.0f B/s, want about 5000", r)
	}

	// a pause: the last sample is a minute old, so it starts again (the task's speed meanwhile)
	for i := range j.seen {
		j.seen[i].t = time.Now().Add(-time.Minute)
	}
	if r := j.jobRate(30000, 77); r != 77 || len(j.seen) != 1 {
		t.Errorf("after a gap: %v with %d samples, want the task's 77 and a fresh start", r, len(j.seen))
	}

	// a long run, looked at all along: only what came in over the last 10 s counts
	j.seen = nil
	now := time.Now()
	for i := 30; i >= 1; i-- {
		j.seen = append(j.seen, byteSample{now.Add(-time.Duration(i) * time.Second), int64(30-i) * 1000})
	}
	if r := j.jobRate(30000, 0); r < 900 || r > 1100 {
		t.Errorf("1000 B/s for 30 s: %.0f", r)
	}
	if len(j.seen) > 13 {
		t.Errorf("old samples kept: %d", len(j.seen))
	}

	j.State = "extracting"
	if r := j.jobRate(30000, 0); r != 0 || len(j.seen) != 0 {
		t.Errorf("not downloading: %v, %d samples", r, len(j.seen))
	}
}
