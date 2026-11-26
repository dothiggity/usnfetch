package share

import (
	"testing"
	"time"
)

// The speed is over the last few seconds of readings, and nothing until a second has passed;
// a nil meter measures nothing.
func TestMeterSpeed(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	meterNow = func() time.Time { return now }
	t.Cleanup(func() { meterNow = time.Now })
	m := &Meter{}
	m.setTotal(100 << 20)
	if _, _, bps := m.Progress(); bps != 0 {
		t.Errorf("speed before anything arrived: %d", bps)
	}
	for i := 1; i <= 20; i++ { // 2 MB every half second: 4 MB/s
		now = now.Add(500 * time.Millisecond)
		m.arrived(2 << 20)
		m.setDone(int64(i) * 2 << 20)
		_, _, bps := m.Progress()
		if i == 1 && bps != 0 {
			t.Errorf("speed after half a second: %d", bps)
		}
		if i >= 4 && (bps < 4<<20*9/10 || bps > 4<<20*11/10) {
			t.Errorf("after %d readings: %d B/s, want about 4 MB/s", i, bps)
		}
	}
	for range 12 { // then it slows to 1 MB/s: within the window it shows that
		now = now.Add(500 * time.Millisecond)
		m.arrived(512 << 10)
		m.Progress()
	}
	if _, _, bps := m.Progress(); bps < 1<<20*9/10 || bps > 1<<20*11/10 {
		t.Errorf("after slowing down: %d B/s, want about 1 MB/s", bps)
	}
	m.setDone(1) // written never goes back
	if done, total, _ := m.Progress(); done != 40<<20 || total != 100<<20 {
		t.Errorf("written %d of %d", done, total)
	}
	var none *Meter
	none.arrived(5)
	none.setDone(5)
	if d, tot, bps := none.Progress(); d != 0 || tot != 0 || bps != 0 {
		t.Errorf("a nil meter: %d %d %d", d, tot, bps)
	}
}
