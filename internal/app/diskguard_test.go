package app

import (
	"strings"
	"testing"
	"time"
)

func TestDiskGuard(t *testing.T) {
	m, err := NewManager(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Store.Close)
	m.Settings.CompleteDir = t.TempDir()
	notes := make(chan string, 4)
	m.Notify = func(title, body string, warn bool) { notes <- title }

	m.Settings.MinFreeGB = 1 << 30 // more than any disk has
	m.checkDisk()
	m.applyThrottle()
	if m.Throttle().Paused {
		t.Fatal("paused with nothing to download")
	}

	m.jobs["j1"] = &Job{ID: "j1", State: "queued"}
	m.checkDisk()
	m.applyThrottle()
	th := m.Throttle()
	if !th.Paused || !th.Disk || !strings.Contains(th.Reason, "free on") {
		t.Fatalf("not paused for the disk: %+v", th)
	}
	select {
	case n := <-notes:
		if !strings.Contains(n, "nearly full") {
			t.Errorf("notification %q", n)
		}
	case <-time.After(2 * time.Second):
		t.Error("no notification when the disk guard paused downloads")
	}

	m.Settings.MinFreeGB = 0 // the guard off: downloads go on
	m.checkDisk()
	m.applyThrottle()
	if m.Throttle().Paused {
		t.Errorf("still paused with the guard off: %+v", m.Throttle())
	}

	m.Settings.Notify = false // notifications off: nothing shown
	m.notify("x", "y", false)
	select {
	case n := <-notes:
		t.Errorf("notified with notifications off: %q", n)
	case <-time.After(200 * time.Millisecond):
	}
}
