package share

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"usnfetch/internal/nntp"
	"usnfetch/internal/yenc"
)

// articleServer: a news server holding the given articles (BODY only), each answer delay
// late; most: the most connections it had open at once.
func articleServer(t *testing.T, arts map[string][]byte, delay time.Duration) (acc *nntp.Account, most *atomic.Int64) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	var open atomic.Int64
	most = &atomic.Int64{}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				n := open.Add(1)
				for m := most.Load(); n > m && !most.CompareAndSwap(m, n); m = most.Load() {
				}
				defer open.Add(-1)
				defer c.Close()
				r, w := bufio.NewReader(c), bufio.NewWriter(c)
				w.WriteString("200 fake news\r\n")
				w.Flush()
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					cmd, arg, _ := strings.Cut(strings.TrimSpace(line), " ")
					switch strings.ToUpper(cmd) {
					case "BODY":
						time.Sleep(delay)
						body, ok := arts[strings.Trim(arg, "<>")]
						if !ok {
							w.WriteString("430 no such article\r\n")
							break
						}
						w.WriteString("222 0 " + arg + "\r\n")
						for _, l := range bytes.Split(bytes.TrimRight(body, "\r\n"), []byte("\r\n")) {
							if bytes.HasPrefix(l, []byte(".")) {
								w.WriteString(".")
							}
							w.Write(l)
							w.WriteString("\r\n")
						}
						w.WriteString(".\r\n")
					case "QUIT":
						w.WriteString("205 bye\r\n")
						w.Flush()
						return
					default:
						w.WriteString("500 what?\r\n")
					}
					w.Flush()
				}
			}()
		}
	}()
	return &nntp.Account{Name: "fake", Server: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, Connections: 8}, most
}

// shardedShare posts a v5 share whose shards have the given numbers of pool entries (one
// file per 10 entries), and returns its manifest and a file in each shard with the message
// ID its first segment must come from.
func shardedShare(t *testing.T, key []byte, entries []int) (Manifest, map[string]string, map[string][]byte) {
	t.Helper()
	rng := rand.New(rand.NewSource(1))
	randHex := func(n int) string {
		b := make([]byte, n)
		rng.Read(b)
		return hex.EncodeToString(b)
	}
	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)
	arts := map[string][]byte{}
	want := map[string]string{} // path -> msgid of its first segment
	var shards []any
	for si, n := range entries {
		pool := make([]map[string]any, 0, n)
		var files []map[string]any
		for k := 0; k < n; k++ {
			pool = append(pool, map[string]any{"m": fmt.Sprintf("s%dp%d.%s@test", si, k, randHex(8)), "n": randHex(12), "t": randHex(16), "s": 700000})
		}
		for f := 0; f*10 < n; f++ {
			p := fmt.Sprintf("shard%d/file%05d.bin", si, f)
			cnt := min(10, n-f*10)
			files = append(files, map[string]any{"file_id": randHex(8), "path": p, "name": filepath.Base(p), "size": cnt * 700000,
				"hash": randHex(32), "seg_range": map[string]any{"start": f * 10, "stride": 1, "count": cnt}})
			if f == 0 || f*10+10 >= n {
				want[p] = pool[f*10]["m"].(string)
			}
		}
		doc, _ := json.Marshal(map[string]any{"shard_index": si, "segment_size": 700000, "msgid_pool": pool, "files": files})
		var z bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&z, gzip.BestSpeed)
		zw.Write(doc)
		zw.Close()
		nonce := []byte(randHex(6)) // 12 bytes
		sealed := aead.Seal(nil, nonce, z.Bytes(), nil)
		ct, tag := sealed[:len(sealed)-16], sealed[len(sealed)-16:]
		blob := append(append(append([]byte{}, nonce...), tag...), ct...)
		var parts []any
		for off, pn := 0, 1; off < len(blob); off, pn = off+700000, pn+1 {
			chunk := blob[off:min(off+700000, len(blob))]
			m := fmt.Sprintf("idx%d-%d@test", si, pn)
			arts[m] = yenc.Encode(chunk, "x")
			parts = append(parts, map[string]any{"part_number": pn, "primary_msgid": m, "redundancy_msgids": []any{}, "size": len(chunk)})
		}
		d := sha256.Sum256(blob)
		shards = append(shards, map[string]any{"shard_index": si, "parts": parts, "digest": hex.EncodeToString(d[:]), "size": len(blob)})
	}
	raw, _ := json.Marshal(map[string]any{"version": 5, "folder_id": "f0", "share_id": "s0", "shards": shards})
	m, err := ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	return m, want, arts
}

// A share split into a few big shards (with small ones between them) builds a complete,
// correctly numbered index without holding whole shards in memory.
func TestBuildBigShards(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	entries := []int{900, 140000, 1200, 140000, 800, 140000, 500} // the big ones: about 10 MB encrypted each
	m, want, arts := shardedShare(t, key, entries)
	for _, sh := range m.List("shards") {
		if n := shardBytes(sh.(map[string]any)); (n > bigShard) != (num(sh.(map[string]any)["shard_index"])%2 == 1) {
			t.Fatalf("shard %v: %d bytes, not the size the test needs", sh.(map[string]any)["shard_index"], n)
		}
	}
	acc, _ := articleServer(t, arts, 0)
	path := filepath.Join(t.TempDir(), "x.usnindex")

	runtime.GC()
	var base runtime.MemStats
	runtime.ReadMemStats(&base)
	var peak atomic.Uint64
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		var ms runtime.MemStats
		for {
			select {
			case <-done:
				return
			case <-time.After(5 * time.Millisecond):
			}
			runtime.ReadMemStats(&ms)
			if ms.HeapInuse > peak.Load() {
				peak.Store(ms.HeapInuse)
			}
		}
	}()
	progressEvery = 0 // every step logs how far it is
	t.Cleanup(func() { progressEvery = 4 * time.Second })
	var pcts []int
	var written []int64 // the meter's bytes written, at each progress line
	meter := &Meter{}
	logf := func(l string) {
		if mm := regexp.MustCompile(`\((\d+)%\)`).FindStringSubmatch(l); mm != nil {
			p, _ := strconv.Atoi(mm[1])
			pcts = append(pcts, p)
			written = append(written, meter.done.Load())
			if meter.Saving() {
				t.Errorf("saving the index while it's still coming: %s", l)
			}
		}
	}
	_, err := BuildIndex(m, key, path, BuildOpts{Dial: func() (*nntp.Conn, error) { return nntp.Dial(acc, 10*time.Second) }, Conns: 8, Log: logf, Meter: meter})
	close(done)
	wg.Wait()
	if err != nil {
		t.Fatal(err)
	}
	// one figure for the whole index, so the progress bar only goes up (and reaches 100 at the end)
	if len(pcts) < 10 || slices.Index(pcts, 100) != len(pcts)-1 || !slices.IsSorted(pcts) {
		t.Errorf("progress went %v", pcts)
	}
	// the meter: bytes written of the index's size, only going up, all of it at the end; and
	// every article's bytes as they arrived (the speed)
	var size int64
	for _, sh := range m.List("shards") {
		size += shardBytes(sh.(map[string]any))
	}
	wrote, total, _ := meter.Progress()
	if total != size || wrote != size || !slices.IsSorted(written) || written[0] >= size {
		t.Errorf("meter: %d of %d written (index %d bytes), along the way %v", wrote, total, size, written)
	}
	if got := meter.got.Load(); got < size*9/10 {
		t.Errorf("meter: %d bytes arrived of %d", got, size)
	}
	if !meter.Saving() {
		t.Error("meter: not saving once all of it arrived")
	}
	grew := int64(peak.Load()) - int64(base.HeapInuse)
	t.Logf("peak heap during the build: +%d MB", grew>>20)

	// each shard's files point at its own articles: looked up by scanning, then (folder tree
	// built) through the tree. Each opened afresh, the tree built before anything is read, as
	// the app does: the read-only connections take the file as never changing.
	for _, tree := range []bool{false, true} {
		ix, err := OpenIndex(path, key)
		if err != nil {
			t.Fatal(err)
		}
		if tree {
			if err := ix.EnsureTree(nil); err != nil {
				t.Fatal(err)
			}
		} else {
			files, got := 0, 0
			for _, n := range entries {
				files += (n + 9) / 10
			}
			if err := ix.EachFile(func(FileEntry) error { got++; return nil }); err != nil || got != files {
				t.Errorf("%d files (%v), want %d", got, err, files)
			}
		}
		for p, msgid := range want {
			fe, err := ix.FileByPath(p)
			if err != nil || fe == nil || fe.Path != p {
				t.Fatalf("tree=%v %s: %v, %v", tree, p, fe, err)
			}
			plan, err := ix.Plan(*fe)
			if err != nil {
				t.Fatal(err)
			}
			seg, err := ix.Segment(plan, 0)
			if err != nil || len(seg.Msgids) == 0 || seg.Msgids[0] != msgid {
				t.Errorf("tree=%v %s: first segment %v (%v), want %s", tree, p, seg.Msgids, err, msgid)
			}
		}
		if fe, err := ix.FileByPath("shard1/no-such-file.bin"); fe != nil || err != nil {
			t.Errorf("tree=%v: a missing file gave %v, %v", tree, fe, err)
		}
		ix.Close()
	}
	// read whole and all at once, the three big shards took about 300 MB here, and that grows
	// with the shards' size; streamed, it stays at what's in flight
	if grew > 200<<20 {
		t.Errorf("the build's heap grew by %d MB", grew>>20)
	}
}

// A big shard streamed while small ones after it are still being fetched shares the build's
// connections with them: never more open at once than the build may use (it dialled twice as
// many, which waited out the provider's connection budget and could fail the build).
func TestBuildShardsConnectionCap(t *testing.T) {
	key := bytes.Repeat([]byte{9}, 32)
	entries := []int{300, 100000, 300, 300, 300, 300, 300, 300, 300, 300, 300, 300}
	m, want, arts := shardedShare(t, key, entries)
	if n := shardBytes(m.List("shards")[1].(map[string]any)); n <= bigShard {
		t.Fatalf("shard 1: %d bytes, not big", n)
	}
	acc, most := articleServer(t, arts, 15*time.Millisecond) // slow enough that fetches overlap
	path := filepath.Join(t.TempDir(), "x.usnindex")
	const conns = 4
	if _, err := BuildIndex(m, key, path, BuildOpts{Dial: func() (*nntp.Conn, error) { return nntp.Dial(acc, 10*time.Second) }, Conns: conns}); err != nil {
		t.Fatal(err)
	}
	if n := most.Load(); n > conns {
		t.Errorf("%d connections open at once, the build may use %d", n, conns)
	}
	ix, err := OpenIndex(path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	for p := range want {
		if fe, err := ix.FileByPath(p); err != nil || fe == nil {
			t.Errorf("%s: %v, %v", p, fe, err)
		}
	}
}

// An index in one piece (one shard here, the way a v3/v4 index comes too) tells the meter
// as its articles are written.
func TestBuildOnePieceMeter(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	m, _, arts := shardedShare(t, key, []int{60000}) // a few MB: several articles
	sh := m.List("shards")[0].(map[string]any)
	if len(sh["parts"].([]any)) < 3 {
		t.Fatalf("%d articles: the test needs several", len(sh["parts"].([]any)))
	}
	acc, _ := articleServer(t, arts, 0)
	meter := &Meter{}
	var seen []int64
	path := filepath.Join(t.TempDir(), "x.usnindex")
	progressEvery = 0
	t.Cleanup(func() { progressEvery = 4 * time.Second })
	_, err := BuildIndex(m, key, path, BuildOpts{Dial: func() (*nntp.Conn, error) { return nntp.Dial(acc, 10*time.Second) }, Conns: 2, Meter: meter,
		Log: func(string) { seen = append(seen, meter.done.Load()) }})
	if err != nil {
		t.Fatal(err)
	}
	wrote, total, _ := meter.Progress()
	if size := shardBytes(sh); total != size || wrote != size || !slices.IsSorted(seen) {
		t.Errorf("meter: %d of %d written (piece %d bytes), along the way %v", wrote, total, size, seen)
	}
}
