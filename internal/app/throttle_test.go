package app

import (
	"testing"
	"time"

	"usnfetch/internal/engine"
)

func at(day time.Weekday, hh, mm int) time.Time {
	// 2026-11-22 is a Sunday
	return time.Date(2026, 11, 22+int(day), hh, mm, 0, 0, time.Local)
}

func TestScheduleWindows(t *testing.T) {
	work := ScheduleRule{Enabled: true, Days: []int{1, 2, 3, 4, 5}, From: "08:00", To: "18:00", Action: "limit", Limit: 10}
	night := ScheduleRule{Enabled: true, Days: []int{5}, From: "23:00", To: "07:00", Action: "pause"}
	cases := []struct {
		r    ScheduleRule
		t    time.Time
		want bool
	}{
		{work, at(time.Monday, 8, 0), true}, {work, at(time.Monday, 17, 59), true}, {work, at(time.Monday, 18, 0), false},
		{work, at(time.Saturday, 12, 0), false}, {work, at(time.Monday, 7, 59), false},
		{night, at(time.Friday, 23, 30), true}, {night, at(time.Saturday, 6, 59), true}, // Friday night runs into Saturday
		{night, at(time.Saturday, 7, 0), false}, {night, at(time.Friday, 6, 0), false}, // Thursday night is not in the rule
		{ScheduleRule{Enabled: true, From: "00:00", To: "00:00", Action: "pause"}, at(time.Wednesday, 13, 0), true},
		{ScheduleRule{Enabled: false, From: "00:00", To: "00:00", Action: "pause"}, at(time.Wednesday, 13, 0), false},
	}
	for i, c := range cases {
		if got, _ := c.r.window(c.t); got != c.want {
			t.Errorf("case %d: window(%s) = %v, want %v", i, c.t.Format("Mon 15:04"), got, c.want)
		}
	}
	if _, end := night.window(at(time.Friday, 23, 30)); !end.Equal(at(time.Saturday, 7, 0)) {
		t.Errorf("overnight window ends %v", end)
	}
	s := &Settings{SpeedLimit: 25, Schedule: []ScheduleRule{night, work}}
	if th := s.ThrottleAt(at(time.Monday, 9, 0)); th.Paused || th.Limit != 10e6 {
		t.Errorf("weekday: %+v", th)
	}
	if th := s.ThrottleAt(at(time.Sunday, 9, 0)); th.Paused || th.Limit != 25e6 || th.Reason != "limit 25 MB/s" {
		t.Errorf("weekend: %+v", th)
	}
	if th := s.ThrottleAt(at(time.Saturday, 2, 0)); !th.Paused {
		t.Errorf("friday night: %+v", th)
	}
	s.ScheduleSkip = float64(at(time.Saturday, 7, 0).Unix()) // resumed by hand
	if th := s.ThrottleAt(at(time.Saturday, 2, 0)); th.Paused || th.Limit != 25e6 {
		t.Errorf("skipped pause: %+v", th)
	}
	s.Paused, s.PausedUntil = true, float64(at(time.Monday, 9, 30).Unix())
	if th := s.ThrottleAt(at(time.Monday, 9, 0)); !th.Paused || !th.Manual {
		t.Errorf("timed pause: %+v", th)
	}
	if th := s.ThrottleAt(at(time.Monday, 9, 31)); th.Paused {
		t.Errorf("timed pause ran out: %+v", th)
	}
	if !ValidRule(work) || ValidRule(ScheduleRule{From: "8", To: "18:00", Action: "pause"}) || ValidRule(ScheduleRule{From: "08:00", To: "18:00", Action: "limit"}) {
		t.Error("ValidRule")
	}
}

func TestLimiter(t *testing.T) {
	l := engine.NewLimiter()
	l.SetRate(4e6) // 4 MB/s, 1 s of burst
	stop := make(chan struct{})
	t0 := time.Now()
	for i := 0; i < 12; i++ { // 12 x 1 MB = 3 s of budget past the empty bucket
		if !l.Wait(stop, 1e6) {
			t.Fatal("stopped")
		}
	}
	if d := time.Since(t0); d < 2500*time.Millisecond || d > 4*time.Second {
		t.Errorf("12 MB at 4 MB/s took %v", d)
	}
	l.SetRate(0)
	t0 = time.Now()
	for i := 0; i < 100; i++ {
		l.Wait(stop, 1e6)
	}
	if d := time.Since(t0); d > 50*time.Millisecond {
		t.Errorf("unlimited waited %v", d)
	}
	l.SetRate(1)
	close(stop)
	if l.Wait(stop, 1e6) {
		t.Error("a stopped wait went through")
	}
}
