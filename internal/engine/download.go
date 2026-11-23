package engine

import (
	"bytes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"usnfetch/internal/nntp"
	"usnfetch/internal/share"
)

// ---- checkpoint bitmap (same file format as the Python version: USNF1) ----------

var bitmapMagic = []byte("USNF1")

type Bitmap struct {
	path, ident string
	count       int64
	mu          sync.Mutex
	bits        []byte
	done        int64
	dirty       bool
}

func LoadBitmap(path string, count int64, ident string) *Bitmap {
	b := &Bitmap{path: path, ident: ident, count: count, bits: make([]byte, (count+7)/8)}
	raw, err := os.ReadFile(path)
	if err != nil || !bytes.HasPrefix(raw, bitmapMagic) || len(raw) < 9 {
		return b
	}
	p := 5
	n := int(binary.LittleEndian.Uint32(raw[p:]))
	p += 4
	if len(raw) < p+n+8 || string(raw[p:p+n]) != ident || int64(binary.LittleEndian.Uint64(raw[p+n:])) != count {
		return b
	}
	copy(b.bits, raw[p+n+8:])
	for _, x := range b.bits {
		for ; x != 0; x &= x - 1 {
			b.done++
		}
	}
	return b
}

func (b *Bitmap) IsDone(i int64) bool { return b.bits[i>>3]&(1<<(i&7)) != 0 }

func (b *Bitmap) Mark(i int64) {
	b.mu.Lock()
	if b.bits[i>>3]&(1<<(i&7)) == 0 {
		b.bits[i>>3] |= 1 << (i & 7)
		b.done++
		b.dirty = true
	}
	b.mu.Unlock()
}

func (b *Bitmap) Done() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.done
}

func (b *Bitmap) Flush() error {
	b.mu.Lock()
	if !b.dirty {
		b.mu.Unlock()
		return nil
	}
	data := append([]byte(nil), b.bits...)
	b.dirty = false
	b.mu.Unlock()
	var buf bytes.Buffer
	buf.Write(bitmapMagic)
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(b.ident)))
	buf.WriteString(b.ident)
	_ = binary.Write(&buf, binary.LittleEndian, uint64(b.count))
	buf.Write(data)
	tmp := b.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		f.Close()
		return err
	}
	_ = f.Sync()
	f.Close()
	return os.Rename(tmp, b.path)
}

// ---- progress ------------------------------------------------------------------

type Stats struct {
	Total   int64
	bytes   atomic.Int64
	Missing atomic.Int64
	Errors  atomic.Int64
	Retries atomic.Int64
	Start   time.Time
	mu      sync.Mutex
	window  []sample
}

type sample struct {
	t time.Time
	n int64
}

func NewStats(total, already int64) *Stats {
	s := &Stats{Total: total, Start: time.Now()}
	s.bytes.Store(already)
	return s
}

func (s *Stats) Add(n int64) {
	s.bytes.Add(n)
	now := time.Now()
	s.mu.Lock()
	s.window = append(s.window, sample{now, n})
	for len(s.window) > 0 && now.Sub(s.window[0].t) > 10*time.Second {
		s.window = s.window[1:]
	}
	s.mu.Unlock()
}

func (s *Stats) Bytes() int64 { return s.bytes.Load() }

// Rate in bytes/second over the last 10 seconds.
func (s *Stats) Rate() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.window) < 2 {
		return 0
	}
	span := s.window[len(s.window)-1].t.Sub(s.window[0].t).Seconds()
	if span <= 0 {
		return 0
	}
	var sum int64
	for _, w := range s.window {
		sum += w.n
	}
	return float64(sum) / span
}

// Task is anything a job runs that can be watched and stopped (Engine, PackFetcher).
type Task interface {
	Stop()
	Progress() (stats *Stats, phase string)
}

// ---- segmented file engine -----------------------------------------------------

// Result of one file.
type Result struct {
	File      string           `json:"file"`
	Out       string           `json:"out"`
	Size      int64            `json:"size"`
	Segments  int64            `json:"segments"`
	Done      int64            `json:"done"`
	Missing   []int64          `json:"missing"`
	Failed    map[int64]string `json:"failed"`
	ElapsedS  float64          `json:"elapsed_s"`
	AvgMiBps  float64          `json:"avg_MiBps"`
	Complete  bool             `json:"complete"`
	SHA256OK  *bool            `json:"sha256_ok,omitempty"`
	SHA256    string           `json:"sha256,omitempty"`
}

// Engine downloads one segmented file: every segment fetched by Message-ID over the
// pool's connections, decrypted, and written once at its final offset; a bitmap
// checkpoint makes any interruption resumable to the segment.
type Engine struct {
	Plan             *share.Plan
	Out              string
	Pool             *Pool
	Log              func(string)
	MissingRetries   int
	RetryWait        time.Duration
	Verify           bool
	ProgressEvery    time.Duration
	bitmap           *Bitmap
	aead             cipher.AEAD
	stop             chan struct{}
	stopOnce         sync.Once
	stats            atomic.Pointer[Stats]
	phase            atomic.Value
	mu               sync.Mutex
	missing          []int64
	failed           map[int64]string
	queue            *workQueue
}

func NewEngine(plan *share.Plan, out string, pool *Pool, log func(string)) (*Engine, error) {
	aead, err := NewAEAD(plan.Index.Key)
	if err != nil {
		return nil, err
	}
	first := ""
	if plan.Count > 0 {
		s, err := plan.Index.Segment(plan, 0)
		if err != nil {
			return nil, err
		}
		first = s.Msgids[0]
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s", plan.File.FileID, plan.File.Size, first)))
	e := &Engine{Plan: plan, Out: out, Pool: pool, Log: log, MissingRetries: 3, RetryWait: 60 * time.Second,
		ProgressEvery: 2 * time.Second, aead: aead, stop: make(chan struct{}), failed: map[int64]string{}}
	e.bitmap = LoadBitmap(out+".usnfetch", plan.Count, hex.EncodeToString(sum[:]))
	e.phase.Store("starting")
	return e, nil
}

func (e *Engine) Stop()          { e.stopOnce.Do(func() { close(e.stop) }) }
func (e *Engine) stopped() bool  { select { case <-e.stop: return true; default: return false } }
func (e *Engine) logf(f string, a ...any) {
	if e.Log != nil {
		e.Log(fmt.Sprintf(f, a...))
	}
}

func (e *Engine) Progress() (*Stats, string) { return e.stats.Load(), e.phase.Load().(string) }

type workQueue struct {
	mu     sync.Mutex
	bm     *Bitmap
	cursor int64
	retry  []int64
}

func (q *workQueue) next() (int64, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if n := len(q.retry); n > 0 {
		i := q.retry[0]
		q.retry = q.retry[1:]
		return i, true
	}
	for q.cursor < q.bm.count {
		i := q.cursor
		q.cursor++
		if !q.bm.IsDone(i) {
			return i, true
		}
	}
	return 0, false
}

func (q *workQueue) put(i ...int64) {
	q.mu.Lock()
	q.retry = append(q.retry, i...)
	q.mu.Unlock()
}

func (e *Engine) worker(id int, f *os.File, acct *nntp.Account, st *Stats, wg *sync.WaitGroup) {
	defer wg.Done()
	var conn *nntp.Conn
	defer func() {
		if conn != nil {
			e.Pool.Put(conn)
		}
	}()
	backoff := time.Second
	for !e.stopped() {
		i, ok := e.queue.next()
		if !ok {
			return
		}
		seg, err := e.Plan.Index.Segment(e.Plan, i)
		if err != nil {
			e.mu.Lock()
			e.failed[i] = err.Error()
			e.mu.Unlock()
			continue
		}
		if conn == nil {
			if conn, err = e.Pool.Get(acct); err != nil {
				conn = nil
				st.Errors.Add(1)
				e.queue.put(i)
				if st.Errors.Load()%50 == 1 {
					e.logf("worker %d: %v (reconnecting)", id, err)
				}
				e.sleep(backoff)
				backoff = min(backoff*2, 30*time.Second)
				continue
			}
		}
		if !DownloadLimit.Wait(e.stop, seg.Size) {
			e.queue.put(i)
			continue
		}
		data, err := FetchAndDecrypt(conn, seg, e.aead, true)
		if err != nil && !IsArticleErr(err) { // connection trouble: drop it, put the segment back, back off
			st.Errors.Add(1)
			e.queue.put(i)
			conn.Close()
			conn = nil
			if st.Errors.Load()%50 == 1 {
				e.logf("worker %d: %v (reconnecting)", id, err)
			}
			e.sleep(backoff)
			backoff = min(backoff*2, 30*time.Second)
			continue
		}
		backoff = time.Second
		if err != nil { // absent or corrupt on every copy here: another provider may have it intact
			if len(e.Pool.Accounts.Backup) > 0 {
				data = e.Pool.BackupFetch(seg, e.aead, true)
			}
			if data == nil {
				e.mu.Lock()
				if errors.Is(err, nntp.ErrMissing) {
					e.missing = append(e.missing, i)
					st.Missing.Add(1)
				} else {
					e.failed[i] = err.Error()
				}
				e.mu.Unlock()
				continue
			}
			st.Retries.Add(1)
		}
		if _, err := f.WriteAt(data, seg.Offset); err != nil {
			e.mu.Lock()
			e.failed[i] = "write: " + err.Error()
			e.mu.Unlock()
			e.Stop()
			return
		}
		e.bitmap.Mark(i)
		st.Add(int64(len(data)))
	}
}

func (e *Engine) sleep(d time.Duration) {
	select {
	case <-e.stop:
	case <-time.After(d):
	}
}

func (e *Engine) runWorkers(f *os.File, n int, st *Stats) {
	slots := e.Pool.Accounts.Slots(n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go e.worker(i, f, slots[i%len(slots)], st, &wg)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	flush := time.NewTicker(5 * time.Second)
	prog := time.NewTicker(e.ProgressEvery)
	defer flush.Stop()
	defer prog.Stop()
	total, count := e.Plan.File.Size, e.Plan.Count
	for {
		select {
		case <-done:
			return
		case <-flush.C:
			_ = f.Sync()
			_ = e.bitmap.Flush()
		case <-prog.C:
			r := st.Rate()
			eta := "--:--"
			if r > 0 {
				eta = fmtETA(float64(total-st.Bytes()) / r)
			}
			pct := 100.0
			if total > 0 {
				pct = 100 * float64(st.Bytes()) / float64(total)
			}
			e.logf("%6.2f%%  %9.2f/%.2f GiB  %7.1f MiB/s  eta %s  missing %d  conn-errors %d  left %d",
				pct, float64(st.Bytes())/(1<<30), float64(total)/(1<<30), r/(1<<20), eta, st.Missing.Load(), st.Errors.Load(), count-e.bitmap.Done())
		}
	}
}

// Run downloads the file and returns its result.
func (e *Engine) Run() Result {
	p := e.Plan
	total, n := p.File.Size, p.Count
	done0 := e.bitmap.Done()
	var doneBytes int64
	if done0 > 0 {
		doneBytes = p.Index.DoneBytes(p, e.bitmap.IsDone, done0)
	}
	e.logf("file: %s  %.2f GiB  %d segments x %d B", p.File.Path, float64(total)/(1<<30), n, p.Index.SegmentSize)
	if done0 > 0 {
		e.logf("resume: %d segments already on disk (%.2f GiB)", done0, float64(doneBytes)/(1<<30))
	}
	e.phase.Store("preallocating")
	t0 := time.Now()
	f, err := preallocate(e.Out, total, e.logf)
	res := Result{File: p.File.Path, Out: e.Out, Size: total, Segments: n, Failed: map[int64]string{}, Missing: []int64{}}
	if err != nil {
		res.Failed[-1] = err.Error()
		return res
	}
	defer f.Close()
	e.logf("preallocated %.2f GiB sparse file in %.1fs: %s", float64(total)/(1<<30), time.Since(t0).Seconds(), e.Out)
	st := NewStats(total, doneBytes)
	e.stats.Store(st)
	e.queue = &workQueue{bm: e.bitmap}
	left := n - done0
	nconn := 0
	if left > 0 {
		nconn = int(max(1, min(int64(e.Pool.Accounts.Connections()), left)))
	}
	e.phase.Store("downloading")
	e.logf("started %d connection(s) (%s); %d segments to fetch", nconn, e.Pool.Accounts.Describe(), left)
	if nconn > 0 {
		e.runWorkers(f, nconn, st)
	}
	_ = f.Sync()
	_ = e.bitmap.Flush()
	for round := 1; len(e.missing) > 0 && round <= e.MissingRetries && !e.stopped(); round++ {
		idxs := uniq(e.missing)
		e.missing = nil
		wait := min(e.RetryWait*time.Duration(round), 300*time.Second)
		e.phase.Store(fmt.Sprintf("waiting %ds to retry %d missing", int(wait.Seconds()), len(idxs)))
		e.logf("%d segment(s) missing on every copy; retry pass %d/%d in %ds", len(idxs), round, e.MissingRetries, int(wait.Seconds()))
		select {
		case <-e.stop:
		case <-time.After(wait):
		}
		if e.stopped() {
			e.missing = idxs
			break
		}
		e.phase.Store("retrying missing")
		e.queue.put(idxs...)
		e.runWorkers(f, min(8, len(idxs)), st)
		_ = f.Sync()
		_ = e.bitmap.Flush()
	}
	res.Done = e.bitmap.Done()
	res.Missing = uniq(e.missing)
	for k, v := range e.failed {
		res.Failed[k] = v
	}
	el := time.Since(st.Start).Seconds()
	res.ElapsedS = float64(int(el*10)) / 10
	res.AvgMiBps = float64(int(float64(st.Bytes()-doneBytes)/(1<<20)/max(el, 1e-9)*100)) / 100
	res.Complete = res.Done == n
	if res.Complete && e.Verify && p.File.SHA256 != "" {
		e.phase.Store("verifying sha256")
		e.logf("verifying SHA-256 of output…")
		sum, err := fileSHA256(f)
		ok := err == nil && sum == lower(p.File.SHA256)
		res.SHA256OK, res.SHA256 = &ok, sum
	}
	if res.Complete && len(res.Missing) == 0 && len(res.Failed) == 0 {
		_ = os.Remove(e.bitmap.path)
	}
	return res
}

func fileSHA256(f *os.File) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, io.NewSectionReader(f, 0, 1<<62)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func uniq(a []int64) []int64 {
	m := map[int64]bool{}
	out := []int64{}
	for _, x := range a {
		if !m[x] {
			m[x] = true
			out = append(out, x)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func fmtETA(s float64) string {
	if s <= 0 || s != s || s > 1e9 {
		return "--:--"
	}
	t := int(s)
	return fmt.Sprintf("%d:%02d:%02d", t/3600, t/60%60, t%60)
}

// preallocate opens (or creates) the output at its full size without writing it: a
// sparse file on NTFS (set sparse, then set the end of file) and on Linux filesystems.
func preallocate(path string, total int64, logf func(string, ...any)) (*os.File, error) {
	st, err := os.Stat(path)
	exists := err == nil
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	if exists && (!st.Mode().IsRegular() || st.Size() == total) {
		return f, nil
	}
	if err := setSparse(f); err != nil {
		logf("note: filesystem refused the sparse attribute (%v); extends may zero-fill", err)
	}
	if err := f.Truncate(total); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// Stopped reports whether Stop was called.
func (e *Engine) Stopped() bool { return e.stopped() }
