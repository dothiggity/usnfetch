package share

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"

	"usnfetch/internal/nntp"
	"usnfetch/internal/yenc"
)

// FetchArticle fetches a plain (yEnc) article, trying each copy; corrupt copies count as
// absent. Returns nntp.ErrMissing when no copy is usable, or a connection error.
func FetchArticle(c *nntp.Conn, msgids []string) ([]byte, error) {
	var last error = nntp.ErrMissing
	for _, m := range msgids {
		body, err := c.Body(m)
		if errors.Is(err, nntp.ErrMissing) {
			continue
		}
		if err != nil {
			return nil, err
		}
		data, _, err := yenc.Decode(body)
		if err != nil {
			last = fmt.Errorf("%w (<%s>: %v)", nntp.ErrMissing, m, err)
			continue
		}
		return data, nil
	}
	return nil, last
}

func partMsgids(p any) []string {
	m, _ := p.(map[string]any)
	out := []string{firstNonEmpty(str(m["primary_msgid"]), str(m["msgid"]))}
	if red, ok := m["redundancy_msgids"].([]any); ok {
		for _, r := range red {
			out = append(out, str(r))
		}
	}
	return out
}

func sortedParts(parts []any) []any {
	out := append([]any(nil), parts...)
	sort.SliceStable(out, func(i, j int) bool {
		return num(out[i].(map[string]any)["part_number"]) < num(out[j].(map[string]any)["part_number"])
	})
	return out
}

// connPool hands out connections to the index fetchers, re-dialling broken ones, at most max
// of them at once (0: no limit). A fetcher that finds none idle and none to spare waits for
// one to come back: a big shard streamed while small ones are still being fetched shares the
// build's connections with them, rather than dialling more than the account allows (those
// waited out the provider's budget, 45 s, and on a slow line failed the build).
type connPool struct {
	dial func() (*nntp.Conn, error)
	max  int
	mu   sync.Mutex
	cond *sync.Cond
	idle []*nntp.Conn
	live int // connections out or idle
}

func newConnPool(dial func() (*nntp.Conn, error), max int) *connPool {
	p := &connPool{dial: dial, max: max}
	p.cond = sync.NewCond(&p.mu)
	return p
}

func (p *connPool) get() (*nntp.Conn, error) {
	p.mu.Lock()
	for {
		if n := len(p.idle); n > 0 {
			c := p.idle[n-1]
			p.idle = p.idle[:n-1]
			p.mu.Unlock()
			return c, nil
		}
		if p.max <= 0 || p.live < p.max {
			p.live++
			p.mu.Unlock()
			c, err := p.dial()
			if err != nil {
				p.mu.Lock()
				p.live--
				p.cond.Signal()
				p.mu.Unlock()
			}
			return c, err
		}
		p.cond.Wait()
	}
}

func (p *connPool) put(c *nntp.Conn) {
	p.mu.Lock()
	p.idle = append(p.idle, c)
	p.cond.Signal()
	p.mu.Unlock()
}

// drop: a broken connection, closed; its place goes to the next fetcher.
func (p *connPool) drop(c *nntp.Conn) {
	c.Close()
	p.mu.Lock()
	p.live--
	p.cond.Signal()
	p.mu.Unlock()
}

func (p *connPool) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.idle {
		c.Close()
	}
	p.live -= len(p.idle)
	p.idle = nil
	p.cond.Broadcast()
}

// fetch one article with retries on connection trouble (a fresh connection each time).
func (p *connPool) fetch(msgids []string) ([]byte, error) {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		var c *nntp.Conn
		if c, err = p.get(); err != nil {
			time.Sleep(time.Duration(attempt+1) * time.Second)
			continue
		}
		var data []byte
		data, err = FetchArticle(c, msgids)
		if err == nil || errors.Is(err, nntp.ErrMissing) {
			p.put(c)
			return data, err
		}
		p.drop(c)
	}
	return nil, err
}

func (p *connPool) fetchWhole(parts []any) ([]byte, error) {
	var out []byte
	for _, part := range sortedParts(parts) {
		b, err := p.fetch(partMsgids(part))
		if err != nil {
			return nil, err
		}
		out = append(out, b...)
	}
	return out, nil
}

// Limits on what a share's index may unpack to, so a hostile share can't fill the memory.
// They match the official client: zstd's default 128 MB window (it reads with libzstd's
// defaults), and 1 GB for a piece read whole (its cap on the shard list).
const (
	maxZstdWindow = 1 << 27
	maxPlain      = 1 << 30
)

// decompress picks gzip, zstd, xz or plain by magic bytes.
func decompress(r io.Reader) (io.Reader, func(), error) {
	br := bufio.NewReaderSize(r, 1<<20)
	head, _ := br.Peek(6)
	switch {
	case bytes.HasPrefix(head, []byte{0x28, 0xb5, 0x2f, 0xfd}):
		z, err := zstd.NewReader(br, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxWindow(maxZstdWindow))
		if err != nil {
			return nil, nil, err
		}
		return z, z.Close, nil
	case bytes.HasPrefix(head, []byte{0xfd, '7', 'z', 'X', 'Z', 0}):
		x, err := xz.NewReader(br)
		return x, func() {}, err
	case bytes.HasPrefix(head, []byte{0x1f, 0x8b}):
		g, err := gzip.NewReader(br)
		if err != nil {
			return nil, nil, err
		}
		return g, func() { g.Close() }, nil
	}
	return br, func() {}, nil
}

func decompressAll(b []byte) ([]byte, error) {
	r, done, err := decompress(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer done()
	b, err = io.ReadAll(io.LimitReader(r, maxPlain+1))
	if err == nil && len(b) > maxPlain {
		err = fmt.Errorf("a piece of this share's index unpacks to more than %d MB: not reading it", maxPlain>>20)
	}
	return b, err
}

// BuildOpts: how the index articles are fetched.
type BuildOpts struct {
	Dial  func() (*nntp.Conn, error)
	Conns int
	Log   func(string)
}

func (o BuildOpts) log(f string, a ...any) {
	if o.Log != nil {
		o.Log(fmt.Sprintf(f, a...))
	}
}

// BuildIndex fetches the core index (v3/v4 parts or v5 shards) and writes the index file.
func BuildIndex(m Manifest, key []byte, path string, o BuildOpts) (string, error) {
	if o.Conns <= 0 {
		o.Conns = 8
	}
	pool := newConnPool(o.Dial, o.Conns)
	defer pool.close()
	b, err := NewBuilder(path)
	if err != nil {
		return "", err
	}
	ok := false
	defer func() {
		if !ok {
			b.Abort()
		}
	}()
	if m.IsSharded() {
		shards := m.List("shards")
		if shards == nil {
			loc := m.Map("shards_locator")
			parts, _ := loc["parts"].([]any)
			blob, err := pool.fetchWhole(parts)
			if err != nil {
				return "", fmt.Errorf("shard list: %w", err)
			}
			plain, err := OpenEnvelope(blob, key)
			if err != nil {
				return "", fmt.Errorf("shard list: %w", err)
			}
			if plain, err = decompressAll(plain); err != nil {
				return "", err
			}
			dd := json.NewDecoder(bytes.NewReader(plain))
			dd.UseNumber()
			var doc map[string]any
			if err := dd.Decode(&doc); err != nil {
				return "", err
			}
			shards, _ = doc["shards"].([]any)
			if root := m.Str("shards_root"); root != "" {
				r, err := shardSetHash(shards)
				if err != nil || r != strings.ToLower(root) {
					return "", errors.New("shard list fetched via the locator does not match the signed shards_root; refusing")
				}
			}
		}
		sort.SliceStable(shards, func(i, j int) bool {
			return num(shards[i].(map[string]any)["shard_index"]) < num(shards[j].(map[string]any)["shard_index"])
		})
		o.log("core index: %d shards", len(shards))
		if len(shards) == 1 {
			sh := shards[0].(map[string]any)
			err = streamEnvelope(pool, shardParts(sh), key, str(sh["digest"]), o, func(d *Doc) error { return b.ApplyDoc(d, 0) }, nil)
		} else {
			err = buildShards(pool, shards, key, b, o)
		}
	} else {
		ci := m.Map("core_index")
		parts, _ := ci["parts"].([]any)
		if len(parts) == 0 {
			if loc, _ := ci["parts_locator"].(map[string]any); loc != nil {
				lp, _ := loc["parts"].([]any)
				blob, err := pool.fetchWhole(lp)
				if err != nil {
					return "", fmt.Errorf("core-index part list: %w", err)
				}
				plain, err := OpenEnvelope(blob, key)
				if err != nil {
					return "", err
				}
				if plain, err = decompressAll(plain); err != nil {
					return "", err
				}
				dd := json.NewDecoder(bytes.NewReader(plain))
				dd.UseNumber()
				var v any
				if err := dd.Decode(&v); err != nil {
					return "", err
				}
				if mm, ok := v.(map[string]any); ok {
					v = mm["parts"]
				}
				parts, _ = v.([]any)
				if root := str(m.Map("sig")["core_index_root"]); root != "" {
					h := sha256.Sum256(corePartsBytes(parts))
					if hex.EncodeToString(h[:]) != strings.ToLower(root) {
						return "", errors.New("core-index part list fetched via the locator does not match the signed root; refusing")
					}
				}
			}
		}
		if len(parts) == 0 {
			return "", errors.New("manifest has no core_index parts")
		}
		o.log("core index: %d part(s)", len(parts))
		err = streamEnvelope(pool, sortedParts(parts), key, "", o, func(d *Doc) error { return b.ApplyDoc(d, 0) }, nil)
	}
	if err != nil {
		return "", err
	}
	out, err := b.Finish(map[string]any{"folder_id": m.Str("folder_id"), "share_id": m.Str("share_id")})
	if err != nil {
		return "", err
	}
	ok = true
	return out, nil
}

func shardParts(sh map[string]any) []any {
	if parts, _ := sh["parts"].([]any); len(parts) > 0 {
		return sortedParts(parts)
	}
	red, _ := sh["redundancy_msgids"].([]any)
	return []any{map[string]any{"part_number": json.Number("0"), "primary_msgid": sh["primary_msgid"], "redundancy_msgids": red}}
}

// streamEnvelope: one encrypted envelope arriving as many articles. They are prefetched
// over the pool's connections (a window ahead, consumed in order) and streamed through
// AES-GCM, decompression and the JSON parser; the tag and digest are checked at the end.
// progress, if given, hears of each article written (done of n) in place of the log line
// (a shard of many reports how far the whole index is).
func streamEnvelope(pool *connPool, parts []any, key []byte, digest string, o BuildOpts, emit func(*Doc) error, progress func(done, n int)) error {
	n := len(parts)
	window := max(16, 6*o.Conns)
	type res struct {
		data []byte
		err  error
	}
	slots := make([]chan res, n)
	for i := range slots {
		slots[i] = make(chan res, 1)
	}
	jobs := make(chan int, n)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for w := 0; w < min(o.Conns, max(n, 1)); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				select {
				case <-stop:
					return
				default:
				}
				d, err := pool.fetch(partMsgids(parts[i]))
				slots[i] <- res{d, err}
			}
		}()
	}
	defer func() { close(stop); wg.Wait() }()
	pr, pw := io.Pipe()
	parseDone := make(chan error, 1)
	go func() {
		r, closeR, err := decompress(pr)
		if err != nil {
			pr.CloseWithError(err)
			parseDone <- err
			return
		}
		err = ParseDoc(r, 50000, emit)
		if err == nil {
			_, err = io.Copy(io.Discard, r) // read to the end so a bad tag surfaces
		}
		closeR()
		pr.CloseWithError(err)
		parseDone <- err
	}()
	h := sha256.New()
	var head []byte
	var g *gcmStream
	next, t0 := 0, time.Now()
	for k := 0; k < min(window, n); k++ {
		jobs <- k
		next++
	}
	writeErr := func() error {
		for i := 0; i < n; i++ {
			r := <-slots[i]
			if next < n {
				jobs <- next
				next++
			}
			if r.err != nil {
				return fmt.Errorf("core index article %d/%d: %w", i+1, n, r.err)
			}
			if digest != "" {
				h.Write(r.data)
			}
			chunk := r.data
			if g == nil {
				head = append(head, chunk...)
				if len(head) < 28 {
					continue
				}
				var err error
				if g, err = newGCMStream(key, head[:12], head[12:28]); err != nil {
					return err
				}
				chunk = head[28:]
			}
			plain := make([]byte, len(chunk))
			g.Decrypt(plain, chunk)
			if _, err := pw.Write(plain); err != nil {
				return err
			}
			if progress != nil {
				progress(i+1, n)
			} else if n > 1 && time.Since(t0) > 4*time.Second {
				t0 = time.Now()
				o.log("core index: %d/%d articles fetched (%d%%)", i+1, n, 100*(i+1)/n)
			}
		}
		if g == nil {
			return errors.New("encrypted core index too short")
		}
		if err := g.Finish(); err != nil {
			return err
		}
		if digest != "" && hex.EncodeToString(h.Sum(nil)) != strings.ToLower(digest) {
			return errors.New("core index digest mismatch")
		}
		return nil
	}()
	close(jobs)
	pw.CloseWithError(writeErr)
	perr := <-parseDone
	if writeErr != nil {
		return writeErr
	}
	return perr
}

// bigShard: a shard with more than this much encrypted data is streamed on its own, the way
// a v4 index is: decrypted, unzipped and parsed as its articles arrive, each entry written
// to the index file as it's read. Read whole, a shard takes several times its size in memory
// (its bytes, the unzipped text, every entry parsed), and a share split into a few big
// shards (8 for 23 TB) ran out of memory with all of them read at once.
const bigShard = 4 << 20

// smallAhead: how much encrypted data of small shards is fetched and parsed ahead of the
// writer at most (they're read whole, several at a time).
const smallAhead = 48 << 20

// progressEvery: how often a sharded build logs how far it is (a test sets it shorter).
var progressEvery = 4 * time.Second

// shardBytes: a shard's encrypted size, from its descriptor (or its parts', or a guess of
// one index article each when they don't say).
func shardBytes(sh map[string]any) int64 {
	if n := int64(num(sh["size"])); n > 0 {
		return n
	}
	var n int64
	for _, p := range shardParts(sh) {
		if s := int64(num(p.(map[string]any)["size"])); s > 0 {
			n += s
		} else {
			n += 750_000
		}
	}
	return n
}

// buildShards: shards written to the index in order. Small ones are fetched, decrypted and
// parsed in parallel (each worker with its own connection), a bounded amount ahead of the
// writer; big ones are streamed by the writer itself, their articles fetched in parallel.
func buildShards(pool *connPool, shards []any, key []byte, b *Builder, o BuildOpts) error {
	n := len(shards)
	type res struct {
		docs []*Doc
		pool int64
		err  error
	}
	size, big := make([]int64, n), make([]bool, n)
	var small []int
	for i, sh := range shards {
		size[i] = shardBytes(sh.(map[string]any))
		if big[i] = size[i] > bigShard; !big[i] {
			small = append(small, i)
		}
	}
	slots := make([]chan res, n)
	for i := range slots {
		slots[i] = make(chan res, 1)
	}
	jobs := make(chan int, n)
	var wg sync.WaitGroup
	var stopped atomic.Bool
	workers := min(o.Conns, len(small))
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if stopped.Load() {
					slots[i] <- res{}
					continue
				}
				sh := shards[i].(map[string]any)
				docs, np, err := func() ([]*Doc, int64, error) {
					blob, err := pool.fetchWhole(shardParts(sh))
					if err != nil {
						return nil, 0, err
					}
					if d := str(sh["digest"]); d != "" {
						s := sha256.Sum256(blob)
						if hex.EncodeToString(s[:]) != strings.ToLower(d) {
							return nil, 0, errors.New("digest mismatch")
						}
					}
					plain, err := OpenEnvelope(blob, key)
					if err != nil {
						return nil, 0, err
					}
					if plain, err = decompressAll(plain); err != nil {
						return nil, 0, err
					}
					var docs []*Doc
					var np int64
					err = ParseDoc(bytes.NewReader(plain), 0, func(d *Doc) error { docs = append(docs, d); np += int64(len(d.Pool)); return nil })
					return docs, np, err
				}()
				if err != nil {
					err = fmt.Errorf("shard %d: %w", num(sh["shard_index"]), err)
				}
				slots[i] <- res{docs, np, err}
			}
		}()
	}
	// small shards in flight ahead of the writer: at most window of them, and smallAhead bytes
	// (one at least, whatever its size)
	window := max(32, 4*workers)
	next, ahead, aheadBytes := 0, 0, int64(0)
	var firstErr error
	dispatch := func() {
		for firstErr == nil && next < len(small) && ahead < window && (ahead == 0 || aheadBytes+size[small[next]] <= smallAhead) {
			jobs <- small[next]
			ahead, aheadBytes = ahead+1, aheadBytes+size[small[next]]
			next++
		}
	}
	// how far the whole index is, by encrypted bytes written (shards differ a lot in size, so
	// counting shards would jump); one figure for every line, so the progress bar only goes up
	var total, done int64
	for _, s := range size {
		total += s
	}
	var base int64
	t0 := time.Now()
	report := func(i int, part int64, pool int64, final bool) {
		if !final && time.Since(t0) < progressEvery {
			return
		}
		t0 = time.Now()
		pct := int64(100)
		if total > 0 {
			pct = min(100, 100*(done+part)/total)
		}
		if !final {
			pct = min(pct, 99) // 100 only once it's all written
		}
		o.log("core index: shard %d/%d (%d%%), %d pool entries", i+1, n, pct, pool)
	}
	for i := 0; i < n && firstErr == nil; i++ {
		if big[i] {
			sh := shards[i].(map[string]any)
			start := base
			var np atomic.Int64 // counted by the parser, read by the progress line
			err := streamEnvelope(pool, shardParts(sh), key, str(sh["digest"]), o, func(d *Doc) error {
				np.Add(int64(len(d.Pool)))
				return b.ApplyDoc(d, start)
			}, func(k, of int) { report(i, size[i]*int64(k)/int64(max(of, 1)), start+np.Load(), false) })
			if err != nil {
				firstErr = fmt.Errorf("shard %d: %w", num(sh["shard_index"]), err)
				break
			}
			base += np.Load()
		} else {
			dispatch() // shard i is on its way (every small shard before it has been written)
			r := <-slots[i]
			ahead, aheadBytes = ahead-1, aheadBytes-size[i]
			if r.err != nil {
				firstErr = r.err
				break
			}
			for _, d := range r.docs {
				if err := b.ApplyDoc(d, base); err != nil {
					firstErr = err
					break
				}
			}
			base += r.pool
			r.docs = nil
			dispatch()
		}
		if firstErr == nil { // (a shard that failed half-way isn't progress, nor 100%)
			done += size[i]
			report(i, 0, base, i == n-1)
		}
	}
	stopped.Store(firstErr != nil) // the shards still queued are skipped
	close(jobs)
	wg.Wait()
	return firstErr
}

// ---------------------------------------------------------------------------
// loading a share
// ---------------------------------------------------------------------------

// LoadOpts for LoadShare.
type LoadOpts struct {
	Accounts []*nntp.Account // primaries first, then backups: the metadata may be gone at one provider
	Password string
	IndexDir string
	Log      func(string)
	Conns    int  // connections for fetching the index (default min(16, account connections))
	ListOK   bool // browsing: a saved file list will do (downloads, streaming and checks need the full index)
}

// IndexPath: the cache file for one manifest (a re-published share gets a new one).
func IndexPath(dir, folderID string, manifestRaw []byte) string {
	h := sha256.Sum256(manifestRaw)
	return filepath.Join(dir, fmt.Sprintf("%s-%s.usnindex", folderID, hex.EncodeToString(h[:])[:16]))
}

// LoadShare fetches the manifest, checks its signature and owner pin, derives the folder
// key and opens the share's index, building it unless a complete one is cached.
func LoadShare(linkStr string, o LoadOpts) (*Index, error) {
	link, err := ParseLink(linkStr)
	if err != nil {
		return nil, err
	}
	logf := func(f string, a ...any) {
		if o.Log != nil {
			o.Log(fmt.Sprintf(f, a...))
		}
	}
	extra := ""
	if len(link.ManifestMsgids) > 1 {
		extra = fmt.Sprintf(" (+%d copies)", len(link.ManifestMsgids)-1)
	}
	logf("link: folder %s, manifest %s%s", link.FolderID, link.ManifestMsgids[0], extra)
	if len(o.Accounts) == 0 {
		return nil, errors.New("no Usenet account configured (Settings)")
	}
	for n, a := range o.Accounts {
		ix, err := loadVia(link, a, o, logf)
		if errors.Is(err, nntp.ErrMissing) && n+1 < len(o.Accounts) {
			logf("share metadata article missing at %s; trying %s", a.Label(), o.Accounts[n+1].Label())
			continue
		}
		return ix, err
	}
	return nil, nntp.ErrMissing
}

// describeManifest: the share's details in plain words for the log ("file list v5, public:
// no password needed"); the format's own name only when it is not one usnfetch knows.
func describeManifest(m Manifest) string {
	d := "file list v" + m.Str("version")
	switch f := m.Str("format"); f {
	case "", "core_index", "share_v4_manifest":
	default:
		d += " (format " + f + ")"
	}
	switch acc := m.AccessMode(); acc {
	case "public":
		d += ", public: no password needed"
	case "password":
		d += ", password protected"
	case "":
	default:
		d += ", access " + acc
	}
	return d
}

func loadVia(link *Link, a *nntp.Account, o LoadOpts, logf func(string, ...any)) (*Index, error) {
	dial := func() (*nntp.Conn, error) { return nntp.Dial(a, 60*time.Second) }
	conn, err := dial()
	if err != nil {
		return nil, err
	}
	raw, err := FetchArticle(conn, link.ManifestMsgids)
	conn.Close()
	if err != nil {
		return nil, err
	}
	m, err := ParseManifest(raw)
	if err != nil {
		return nil, err
	}
	logf("share: %s", describeManifest(m))
	if err := checkOwnerPin(m, link); err != nil {
		return nil, err
	}
	sig, err := CheckSignature(m, link)
	if err != nil {
		return nil, err
	}
	if sig == "verified" {
		logf("signature: verified (owner %s…, pinned by the link)", truncate(m.Str("owner_key_fingerprint"), 16))
	} else {
		logf("signature: unsigned (manifest format predates signing)")
	}
	key, err := FolderKey(m, link, o.Password)
	if err != nil {
		return nil, err
	}
	dir := o.IndexDir
	if dir == "" {
		dir = "index_cache"
	}
	path := IndexPath(dir, link.FolderID, raw)
	ix, err := openCached(path, o.ListOK, key, logf)
	if err != nil {
		return nil, err
	}
	if ix == nil { // nothing usable saved: fetch the index
		conns := o.Conns
		if conns <= 0 {
			conns = min(16, max(1, a.Connections))
		}
		t0 := time.Now()
		if _, err := BuildIndex(m, key, path, BuildOpts{Dial: dial, Conns: conns, Log: o.Log}); err != nil {
			return nil, err
		}
		logf("core index: indexed in %.1fs -> %s", time.Since(t0).Seconds(), filepath.Base(path))
		CacheMu.Lock()
		ix, err = OpenIndex(path, key)
		CacheMu.Unlock()
		if err != nil {
			return nil, err
		}
	}
	path = ix.Path
	ix.Info = map[string]any{
		"format": m.Str("format"), "version": m["version"], "access": m.AccessMode(), "created_at": m.Str("created_at"),
		"share_id": m.Str("share_id"), "folder_id": link.FolderID, "newsgroup": link.Newsgroup,
		"owner_fingerprint": strings.ToLower(firstNonEmpty(m.Str("owner_key_fingerprint"), link.OwnerFP)),
		"signature":         sig, "segment_size": ix.SegmentSize, "pool_count": ix.PoolCount,
	}
	_ = os.Chtimes(path, time.Now(), time.Now()) // last used: the cache keeps the most recently used
	if ix.HasPool {
		logf("core index: %d pool entries, %d files, segment_size=%d", ix.PoolCount, ix.FileCount, ix.SegmentSize)
	}
	return ix, nil
}

// openCached opens what the cache has for this index: the full index (an older-format one is
// converted first, on disk), or, when a file list will do, the saved file list (unzipped
// first if it is zipped). nil, nil: nothing usable, fetch the index.
func openCached(path string, listOK bool, key []byte, logf func(string, ...any)) (*Index, error) {
	CacheMu.Lock()
	defer CacheMu.Unlock()
	if sch, _ := FileInfo(path); sch == 2 {
		t0 := time.Now()
		logf("core index: converting the saved index to the smaller format (once)")
		if err := Convert(path); err != nil {
			logf("core index: could not convert it (%v); fetching it again", err)
		} else {
			logf("core index: converted in %.1fs", time.Since(t0).Seconds())
		}
	}
	if sch, pool := FileInfo(path); sch == SchemaVersion && pool {
		logf("core index: cached in %s", filepath.Base(path))
		return OpenIndex(path, key)
	}
	if !listOK {
		return nil, nil
	}
	list := ListPath(path)
	if _, err := os.Stat(list); err != nil {
		if _, err := os.Stat(list + GzExt); err != nil {
			return nil, nil
		}
		t0 := time.Now()
		if err := GunzipFile(list); err != nil {
			logf("core index: could not unzip the saved file list (%v); fetching the index", err)
			return nil, nil
		}
		logf("core index: unzipped the saved file list in %.1fs", time.Since(t0).Seconds())
	}
	if sch, _ := FileInfo(list); sch != SchemaVersion {
		return nil, nil
	}
	logf("core index: opened the saved file list (%s): downloading, previews and checks fetch the rest of the index", filepath.Base(list))
	return OpenIndex(list, key)
}

// checkOwnerPin: for any manifest that names an owner key, the link's pin must match it.
func checkOwnerPin(m Manifest, link *Link) error {
	if link.OwnerFP == "" {
		return nil
	}
	fp := strings.ToLower(m.Str("owner_key_fingerprint"))
	if p := m.Str("owner_public_key"); p != "" {
		pub, err := parseOwnerKey(p)
		if err == nil {
			s := sha256.Sum256(pub)
			calc := hex.EncodeToString(s[:])
			if fp != "" && calc != fp {
				return errors.New("the manifest's stated key fingerprint does not belong to its own public key")
			}
			fp = calc
		}
	}
	if fp != "" && !strings.HasPrefix(fp, link.OwnerFP) {
		return fmt.Errorf("publisher key mismatch: the link is pinned to %s…, the manifest was published by %s…", truncate(link.OwnerFP, 16), truncate(fp, 16))
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
