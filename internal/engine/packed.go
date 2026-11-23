package engine

import (
	"bytes"
	"compress/gzip"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
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

	"usnfetch/internal/nntp"
	"usnfetch/internal/share"
)

// Packed segments: files under 500 KB share one article. Publisher layout (binary
// header v3), after AES-GCM:
//   [header_size u32le][version u32le = 3][file_count u32le][file_id 16B | offset u32le | size u32le | flags u8] x N [data]
// header_size counts from `version` (8 + 25*N); member offsets are relative to 4 + header_size.
// The whole segment may be a gzip or zstd frame, and a member with flags bit 0 is stored
// gzip- or zstd-compressed -- told apart by their magic bytes.

const packVersion = 3

var zstdDec, _ = zstd.NewReader(nil, zstd.WithDecoderConcurrency(0))

// Unframe: a gzip / zstd frame -> its content; anything else unchanged (the publisher's rule).
func Unframe(data []byte) ([]byte, error) {
	switch {
	case bytes.HasPrefix(data, []byte{0x1f, 0x8b}):
		r, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		defer r.Close()
		return io.ReadAll(r)
	case bytes.HasPrefix(data, []byte{0x28, 0xb5, 0x2f, 0xfd}):
		return zstdDec.DecodeAll(data, nil)
	}
	return data, nil
}

type packEntry struct {
	off, size uint32
	flags     byte
}

// ParsePack: a decrypted packed segment -> (segment bytes, data start, file_id -> entry).
func ParsePack(plain []byte) ([]byte, int, map[string]packEntry, error) {
	data, err := Unframe(plain)
	if err != nil {
		return nil, 0, nil, err
	}
	if len(data) < 12 {
		return nil, 0, nil, fmt.Errorf("packed segment too short (%d B)", len(data))
	}
	hs := binary.LittleEndian.Uint32(data[0:])
	ver := binary.LittleEndian.Uint32(data[4:])
	count := binary.LittleEndian.Uint32(data[8:])
	if ver != packVersion {
		return nil, 0, nil, fmt.Errorf("unsupported packed segment version %d", ver)
	}
	if int64(hs) != 8+25*int64(count) || 4+int64(hs) > int64(len(data)) {
		return nil, 0, nil, fmt.Errorf("packed segment header size %d does not fit %d files / %d B", hs, count, len(data))
	}
	m := make(map[string]packEntry, count)
	for k := 0; k < int(count); k++ {
		e := data[12+25*k:]
		m[hex.EncodeToString(e[:16])] = packEntry{binary.LittleEndian.Uint32(e[16:]), binary.LittleEndian.Uint32(e[20:]), e[24]}
	}
	return data, 4 + int(hs), m, nil
}

// PackMember: one member's plaintext, checked against the index's size and SHA-256.
func PackMember(data []byte, start int, e packEntry, fe share.FileEntry) ([]byte, error) {
	lo, hi := int64(start)+int64(e.off), int64(start)+int64(e.off)+int64(e.size)
	if hi > int64(len(data)) {
		return nil, fmt.Errorf("%s: packed bytes %d-%d run past the segment (%d B)", fe.Path, lo, hi, len(data))
	}
	blob := data[lo:hi]
	if e.flags&1 != 0 {
		var err error
		if blob, err = Unframe(blob); err != nil {
			return nil, fmt.Errorf("%s: %w", fe.Path, err)
		}
	}
	if int64(len(blob)) != fe.Size {
		return nil, fmt.Errorf("%s: packed member is %d B, index says %d B", fe.Path, len(blob), fe.Size)
	}
	if fe.SHA256 != "" {
		s := sha256.Sum256(blob)
		if hex.EncodeToString(s[:]) != strings.ToLower(fe.SHA256) {
			return nil, fmt.Errorf("%s: sha256 mismatch", fe.Path)
		}
	}
	return blob, nil
}

// MemberOf finds a file in a parsed pack.
func MemberOf(data []byte, start int, members map[string]packEntry, fe share.FileEntry) ([]byte, error) {
	e, ok := members[strings.ToLower(fe.FileID)]
	if !ok {
		return nil, fmt.Errorf("%s: not in its packed segment's header", fe.Path)
	}
	return PackMember(data, start, e, fe)
}

// WriteWhole writes a file via a temp file, so a file that exists is complete.
func WriteWhole(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".usnfetch-part"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Wanted is a file to fetch and where it goes.
type Wanted struct {
	File share.FileEntry
	Out  string
}

// PackResult of a PackFetcher run.
type PackResult struct {
	Files    int               `json:"files"`
	Done     []string          `json:"done"`
	Missing  map[string]string `json:"missing"`
	Failed   map[string]string `json:"failed"`
	ElapsedS float64           `json:"elapsed_s"`
	Complete bool              `json:"complete"`
}

// PackFetcher downloads packed files: each packed segment holding a wanted file is
// fetched once (primary, then redundancy copies, then backup accounts), opened, split,
// and every wanted member is verified and written whole.
type PackFetcher struct {
	Index          *share.Index
	Pool           *Pool
	Log            func(string)
	MissingRetries int
	RetryWait      time.Duration
	OnFile         func(fe share.FileEntry, out string)
	groups         map[int64][]Wanted
	total          int64
	aead           cipher.AEAD
	stop           chan struct{}
	stopOnce       sync.Once
	stats          atomic.Pointer[Stats]
	phase          atomic.Value
	mu             sync.Mutex
	done           []string
	missing        map[string]string
	failed         map[string]string
}

func NewPackFetcher(ix *share.Index, wanted []Wanted, pool *Pool, log func(string)) (*PackFetcher, error) {
	aead, err := NewAEAD(ix.Key)
	if err != nil {
		return nil, err
	}
	p := &PackFetcher{Index: ix, Pool: pool, Log: log, MissingRetries: 3, RetryWait: 60 * time.Second, groups: map[int64][]Wanted{},
		aead: aead, stop: make(chan struct{}), missing: map[string]string{}, failed: map[string]string{}}
	for _, w := range wanted {
		if w.File.Pack == nil {
			return nil, fmt.Errorf("%s is not a packed file", w.File.Path)
		}
		p.groups[*w.File.Pack] = append(p.groups[*w.File.Pack], w)
		p.total += w.File.Size
	}
	p.phase.Store("starting")
	return p, nil
}

func (p *PackFetcher) Stop()                      { p.stopOnce.Do(func() { close(p.stop) }) }
func (p *PackFetcher) Progress() (*Stats, string) { return p.stats.Load(), p.phase.Load().(string) }
func (p *PackFetcher) stopped() bool {
	select {
	case <-p.stop:
		return true
	default:
		return false
	}
}

var errStopped = errors.New("stopped")

func (p *PackFetcher) one(c *nntp.Conn, pid int64, st *Stats) error {
	seg, err := p.Index.PackSegment(pid)
	if err != nil {
		return err
	}
	if !DownloadLimit.Wait(p.stop, max(seg.Size, 1)) {
		return errStopped
	}
	// a pack's index `size` is not reliably the article length (compressed packs): the GCM
	// tag authenticates the bytes and each member is hash-checked instead.
	plain, err := FetchAndDecrypt(c, seg, p.aead, false)
	if err != nil {
		if !IsArticleErr(err) {
			return err
		}
		if len(p.Pool.Accounts.Backup) > 0 {
			plain = p.Pool.BackupFetch(seg, p.aead, false)
		}
		if plain == nil {
			return err
		}
	}
	data, start, members, err := ParsePack(plain)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	for _, w := range p.groups[pid] {
		blob, err := MemberOf(data, start, members, w.File)
		if err == nil {
			err = WriteWhole(w.Out, blob)
		}
		if err != nil {
			p.mu.Lock()
			p.failed[w.File.Path] = err.Error()
			p.mu.Unlock()
			continue
		}
		st.Add(w.File.Size)
		p.mu.Lock()
		p.done = append(p.done, w.File.Path)
		p.mu.Unlock()
		if p.OnFile != nil {
			p.OnFile(w.File, w.Out)
		}
	}
	return nil
}

func (p *PackFetcher) pass(pids []int64, st *Stats) []int64 {
	var mu sync.Mutex
	queue := append([]int64(nil), pids...)
	var missing []int64
	n := max(1, min(p.Pool.Accounts.Connections(), len(pids)))
	slots := p.Pool.Accounts.Slots(n)
	var wg sync.WaitGroup
	for w := 0; w < n; w++ {
		wg.Add(1)
		go func(acct *nntp.Account) {
			defer wg.Done()
			var c *nntp.Conn
			defer func() {
				if c != nil {
					p.Pool.Put(c)
				}
			}()
			backoff := time.Second
			for !p.stopped() {
				mu.Lock()
				if len(queue) == 0 {
					mu.Unlock()
					return
				}
				pid := queue[0]
				queue = queue[1:]
				mu.Unlock()
				var err error
				if c == nil {
					if c, err = p.Pool.Get(acct); err != nil {
						c = nil
					}
				}
				if c != nil {
					err = p.one(c, pid, st)
				}
				switch {
				case err == nil:
					backoff = time.Second
				case errors.Is(err, errStopped): // stopped while waiting for the speed limit
					mu.Lock()
					queue = append(queue, pid)
					mu.Unlock()
					return
				case errors.Is(err, nntp.ErrMissing):
					mu.Lock()
					missing = append(missing, pid)
					mu.Unlock()
					st.Missing.Add(1)
				case errors.Is(err, ErrCorrupt) || (c != nil && !nntp.IsConnErr(err)):
					p.mu.Lock()
					for _, w := range p.groups[pid] {
						p.failed[w.File.Path] = err.Error()
					}
					p.mu.Unlock()
				default: // connection trouble: requeue, reconnect, back off
					st.Errors.Add(1)
					mu.Lock()
					queue = append(queue, pid)
					mu.Unlock()
					if c != nil {
						c.Close()
						c = nil
					}
					if st.Errors.Load()%50 == 1 && p.Log != nil {
						p.Log(fmt.Sprintf("packed: %v (reconnecting)", err))
					}
					select {
					case <-p.stop:
					case <-time.After(backoff):
					}
					backoff = min(backoff*2, 30*time.Second)
				}
			}
		}(slots[w%len(slots)])
	}
	wg.Wait()
	return missing
}

func (p *PackFetcher) Run() PackResult {
	st := NewStats(p.total, 0)
	p.stats.Store(st)
	pids := make([]int64, 0, len(p.groups))
	files := 0
	for pid, g := range p.groups {
		pids = append(pids, pid)
		files += len(g)
	}
	sort.Slice(pids, func(i, j int) bool { return pids[i] < pids[j] })
	if p.Log != nil {
		p.Log(fmt.Sprintf("packed: %d small file(s) in %d packed segment(s), %.2f MiB", files, len(pids), float64(p.total)/(1<<20)))
	}
	p.phase.Store("downloading packed files")
	missing := p.pass(pids, st)
	for round := 1; len(missing) > 0 && round <= p.MissingRetries && !p.stopped(); round++ {
		wait := min(p.RetryWait*time.Duration(round), 300*time.Second)
		p.phase.Store(fmt.Sprintf("waiting %ds to retry %d missing packed segment(s)", int(wait.Seconds()), len(missing)))
		select {
		case <-p.stop:
		case <-time.After(wait):
		}
		if p.stopped() {
			break
		}
		p.phase.Store("retrying missing packed segments")
		missing = p.pass(missing, st)
	}
	for _, pid := range missing {
		for _, w := range p.groups[pid] {
			p.missing[w.File.Path] = "packed segment missing on every copy"
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return PackResult{Files: files, Done: append([]string{}, p.done...), Missing: p.missing, Failed: p.failed,
		ElapsedS: float64(int(time.Since(st.Start).Seconds()*10)) / 10, Complete: len(p.done) == files}
}

// Stopped reports whether Stop was called.
func (p *PackFetcher) Stopped() bool { return p.stopped() }
