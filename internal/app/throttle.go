package app

import (
	"fmt"
	"strings"
	"time"

	"usnfetch/internal/engine"
)

// ScheduleRule pauses or slows downloads during a weekly time window.
type ScheduleRule struct {
	Enabled bool    `json:"enabled"`
	Days    []int   `json:"days"`       // time.Weekday numbers (0 = Sunday); empty = every day
	From    string  `json:"from"`       // "HH:MM"
	To      string  `json:"to"`         // "HH:MM"; at or before From = the window runs past midnight
	Action  string  `json:"action"`     // "pause" | "limit"
	Limit   float64 `json:"limit_mbps"` // MB/s for "limit"
}

func hhmm(s string) (int, bool) {
	var h, m int
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d:%d", &h, &m); err != nil || h < 0 || h > 24 || m < 0 || m > 59 {
		return 0, false
	}
	return min(h*60+m, 24*60), true
}

func (r ScheduleRule) onDay(d int) bool {
	if len(r.Days) == 0 {
		return true
	}
	for _, x := range r.Days {
		if x == d {
			return true
		}
	}
	return false
}

// window: whether the rule covers t, and when that stretch ends.
func (r ScheduleRule) window(t time.Time) (bool, time.Time) {
	from, ok1 := hhmm(r.From)
	to, ok2 := hhmm(r.To)
	if !r.Enabled || !ok1 || !ok2 {
		return false, time.Time{}
	}
	day0 := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	at := func(d time.Time, min int) time.Time { return d.Add(time.Duration(min) * time.Minute) }
	m := t.Hour()*60 + t.Minute()
	wd := int(t.Weekday())
	switch {
	case from < to:
		if r.onDay(wd) && m >= from && m < to {
			return true, at(day0, to)
		}
	case from == to: // the whole day
		if r.onDay(wd) {
			return true, day0.AddDate(0, 0, 1)
		}
	default: // past midnight: started today, or started yesterday
		if r.onDay(wd) && m >= from {
			return true, at(day0.AddDate(0, 0, 1), to)
		}
		if r.onDay((wd+6)%7) && m < to {
			return true, at(day0, to)
		}
	}
	return false, time.Time{}
}

var dayNames = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

func (r ScheduleRule) label() string {
	days := "daily"
	if len(r.Days) > 0 && len(r.Days) < 7 {
		var n []string
		for _, d := range r.Days {
			if d >= 0 && d < 7 {
				n = append(n, dayNames[d])
			}
		}
		days = strings.Join(n, " ")
	}
	return fmt.Sprintf("%s %s–%s", days, r.From, r.To)
}

// Throttle is what downloads may do right now.
type Throttle struct {
	Paused bool    `json:"paused"`
	Disk   bool    `json:"disk,omitempty"`  // paused because the download drive is nearly full
	Limit  float64 `json:"limit"`           // bytes per second, 0 = unlimited
	Reason string  `json:"reason"`          // for people: "paused until 14:30", "schedule: 10 MB/s Mon–Fri 08:00–18:00"
	Manual bool    `json:"manual"`          // paused by hand
	Until  float64 `json:"until,omitempty"` // when a pause by hand ends (0 = until resumed)
	Skip   float64 `json:"skip,omitempty"`  // schedule pauses are ignored until then (resumed by hand)
}

// fmtMBps: a speed limit for people: "10 MB/s", "2.5 MB/s", and under 1 MB/s in KB/s ("40 KB/s",
// not "0 MB/s").
func fmtMBps(v float64) string {
	if v > 0 && v < 1 {
		return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.1f", v*1000), "0"), ".") + " KB/s"
	}
	return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.1f", v), "0"), ".") + " MB/s"
}

// ThrottleAt works out the pause / speed limit in force at t: a pause by hand first, then
// the first schedule rule covering t, then the plain speed limit.
func (s *Settings) ThrottleAt(t time.Time) Throttle {
	now := float64(t.Unix())
	if s.Paused && (s.PausedUntil == 0 || now < s.PausedUntil) {
		th := Throttle{Paused: true, Manual: true, Until: s.PausedUntil, Reason: "paused"}
		if s.PausedUntil > 0 {
			th.Reason = "paused until " + time.Unix(int64(s.PausedUntil), 0).Format("15:04")
		}
		return th
	}
	for _, r := range s.Schedule {
		on, _ := r.window(t)
		if !on {
			continue
		}
		if r.Action == "pause" {
			if now < s.ScheduleSkip {
				continue
			}
			return Throttle{Paused: true, Reason: "schedule: paused " + r.label()}
		}
		if r.Limit > 0 {
			return Throttle{Limit: r.Limit * 1e6, Reason: "schedule: " + fmtMBps(r.Limit) + " " + r.label(), Skip: s.ScheduleSkip}
		}
	}
	if s.SpeedLimit > 0 {
		return Throttle{Limit: s.SpeedLimit * 1e6, Reason: "limit " + fmtMBps(s.SpeedLimit)}
	}
	return Throttle{}
}

// ---- manager side ----

// Throttle: the pause / limit currently applied.
func (m *Manager) Throttle() Throttle {
	if t := m.throttle.Load(); t != nil {
		return *t
	}
	return Throttle{}
}

// applyThrottle sets the download speed limit and, when a pause starts, stops the
// running jobs (they go back to the queue and resume from their checkpoints).
func (m *Manager) applyThrottle() {
	s := m.Settings
	t := time.Now()
	if s.Paused && s.PausedUntil > 0 && float64(t.Unix()) >= s.PausedUntil { // a timed pause ran out
		s.Paused, s.PausedUntil = false, 0
		_ = m.SaveSettings()
	}
	th := s.ThrottleAt(t)
	if d := m.diskLow.Load(); d != nil { // the disk guard outranks everything else
		th.Paused, th.Disk = true, true
		th.Reason = fmt.Sprintf("paused: only %s free on %s (keeping %d GB free)", gbText(d.free), d.path, s.MinFreeGB)
	}
	old := m.throttle.Swap(&th)
	engine.DownloadLimit.SetRate(th.Limit)
	// the disk guard logs its own pause and resume (checkDisk), and its reason's free space
	// changes on every check: not a line for each of those
	if (old == nil || old.Reason != th.Reason) && !th.Disk {
		if th.Reason == "" {
			m.Log("downloads: full speed")
		} else {
			m.Log("downloads: " + th.Reason)
		}
	}
	if th.Paused && !m.paused.Swap(true) {
		m.mu.Lock()
		for _, j := range m.jobs {
			j.mu.Lock()
			if runningStates[j.State] && j.State != "stopping" {
				j.pauseStop = true
				j.cancel.Store(true)
				if j.task != nil {
					j.task.Stop()
				}
			}
			j.mu.Unlock()
		}
		m.mu.Unlock()
	} else if !th.Paused {
		m.paused.Store(false)
	}
}

func (m *Manager) governor() {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-m.stopSched:
			return
		case <-t.C:
		case <-m.kick:
		}
		m.checkDisk()
		m.applyThrottle()
	}
}

// diskLow: the download drive had less free space than Settings.MinFreeGB.
type diskLow struct {
	path string
	free int64
}

// checkDisk pauses downloads before the download drive fills up, and lets them go on once
// there is a GB more than the minimum again. Only while there is something to download.
func (m *Manager) checkDisk() {
	reserve := int64(m.Settings.MinFreeGB) * 1e9
	dir := m.Settings.CompleteDir
	cur := m.diskLow.Load()
	m.mu.Lock()
	busy := false
	for _, j := range m.jobs {
		j.mu.Lock()
		busy = runningStates[j.State] || j.State == "queued"
		j.mu.Unlock()
		if busy {
			break
		}
	}
	m.mu.Unlock()
	if reserve <= 0 || dir == "" || !busy {
		if cur != nil {
			m.diskLow.Store(nil)
		}
		return
	}
	free, err := DiskFree(dir)
	if err != nil {
		return
	}
	switch {
	case cur == nil && free < reserve:
		m.diskLow.Store(&diskLow{dir, free})
		m.Log(fmt.Sprintf("downloads paused: only %s free on %s (keeping %d GB free; Settings -> Downloads)", gbText(free), dir, m.Settings.MinFreeGB))
		m.notify("Downloads paused: the disk is nearly full", fmt.Sprintf("Only %s free on %s. They carry on once there's room.", gbText(free), dir), true)
	case cur != nil && free >= reserve+1e9:
		m.diskLow.Store(nil)
		m.Log(fmt.Sprintf("downloads resume: %s free on %s", gbText(free), dir))
		m.notify("Downloads resumed", fmt.Sprintf("%s free on %s again.", gbText(free), dir), false)
	case cur != nil:
		m.diskLow.Store(&diskLow{dir, free}) // keep the figure shown current
	}
}

func gbText(n int64) string { return fmt.Sprintf("%.1f GB", float64(n)/1e9) }

// notify: a notification on this machine, when it has a way to show one and they are on.
func (m *Manager) notify(title, body string, warn bool) {
	if f := m.Notify; f != nil && m.Settings.Notify {
		go f(title, body, warn)
	}
}

// Kick re-evaluates the pause / limit now (after a settings change).
func (m *Manager) Kick() {
	select {
	case m.kick <- struct{}{}:
	default:
	}
}

// PauseDownloads stops downloading: for the given minutes, or until Resume (minutes <= 0).
func (m *Manager) PauseDownloads(minutes float64) {
	s := m.Settings
	s.Paused, s.PausedUntil = true, 0
	if minutes > 0 {
		s.PausedUntil = float64(time.Now().Add(time.Duration(minutes * float64(time.Minute))).Unix())
	}
	_ = m.SaveSettings()
	m.applyThrottle()
}

// ResumeDownloads undoes a pause by hand; during a scheduled pause it skips the rest of that window.
func (m *Manager) ResumeDownloads() {
	s := m.Settings
	s.Paused, s.PausedUntil = false, 0
	t := time.Now()
	for _, r := range s.Schedule {
		if on, end := r.window(t); on && r.Action == "pause" {
			s.ScheduleSkip = max(s.ScheduleSkip, float64(end.Unix()))
		}
	}
	_ = m.SaveSettings()
	m.applyThrottle()
}

// Reorder puts the given jobs in this order, in the places they hold in the queue
// (jobs not listed keep their place). The scheduler starts queued jobs in queue order.
func (m *Manager) Reorder(ids []string) {
	m.mu.Lock()
	seen := map[string]bool{}
	var uniq []string
	for _, id := range ids {
		if m.jobs[id] != nil && !seen[id] {
			seen[id] = true
			uniq = append(uniq, id)
		}
	}
	var slots []int
	for i, id := range m.order {
		if seen[id] {
			slots = append(slots, i)
		}
	}
	for k, i := range slots {
		m.order[i] = uniq[k]
	}
	m.mu.Unlock()
	m.SaveState()
}

// ValidRule: well-formed times and action.
func ValidRule(r ScheduleRule) bool {
	_, ok1 := hhmm(r.From)
	_, ok2 := hhmm(r.To)
	for _, d := range r.Days {
		if d < 0 || d > 6 {
			return false
		}
	}
	return ok1 && ok2 && (r.Action == "pause" || (r.Action == "limit" && r.Limit > 0))
}
