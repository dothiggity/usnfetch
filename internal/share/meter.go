package share

import (
	"sync"
	"sync/atomic"
	"time"
)

// Meter: how far a share's index download is, read while it runs (a progress bar): bytes of
// the index written to its file of the total, and how fast its articles are arriving. A nil
// Meter is fine to use: it measures nothing.
type Meter struct {
	done, total, got atomic.Int64
	mu               sync.Mutex
	samples          []meterSample // bytes arrived, read now and then, for the speed
}

type meterSample struct {
	t time.Time
	n int64
}

// meterWindow: the speed is over about this long, so it doesn't jump with each article.
const meterWindow = 5 * time.Second

var meterNow = time.Now // a test sets the time

func (m *Meter) setTotal(n int64) {
	if m != nil {
		m.total.Store(n)
	}
}

// setDone: bytes written so far (it never goes back).
func (m *Meter) setDone(n int64) {
	if m == nil {
		return
	}
	for {
		old := m.done.Load()
		if n <= old || m.done.CompareAndSwap(old, n) {
			return
		}
	}
}

func (m *Meter) arrived(n int) {
	if m != nil {
		m.got.Add(int64(n))
	}
}

// Progress: bytes written of total (total 0: not known yet), and the bytes a second arriving
// over the last few seconds (0 until two readings a second apart; each call is a reading).
func (m *Meter) Progress() (done, total, bps int64) {
	if m == nil {
		return 0, 0, 0
	}
	now, got := meterNow(), m.got.Load()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.samples) == 0 || now.Sub(m.samples[len(m.samples)-1].t) >= 200*time.Millisecond {
		m.samples = append(m.samples, meterSample{now, got})
	}
	for len(m.samples) > 2 && now.Sub(m.samples[1].t) >= meterWindow { // the newest one past the window starts it
		m.samples = m.samples[1:]
	}
	if first := m.samples[0]; now.Sub(first.t) >= time.Second {
		bps = int64(float64(got-first.n) / now.Sub(first.t).Seconds())
	}
	return m.done.Load(), m.total.Load(), bps
}
