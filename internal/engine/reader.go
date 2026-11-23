package engine

import (
	"container/list"
	"crypto/cipher"
	"fmt"
	"io"
	"sync"

	"usnfetch/internal/nntp"
	"usnfetch/internal/share"
)

// SegmentCache keeps recently decrypted segments (LRU by bytes) so a player's
// overlapping range requests don't refetch articles.
type SegmentCache struct {
	max, size int64
	mu        sync.Mutex
	ll        *list.List
	items     map[string]*list.Element
}

type cacheItem struct {
	key  string
	off  int64
	data []byte
}

func NewSegmentCache(maxBytes int64) *SegmentCache {
	return &SegmentCache{max: maxBytes, ll: list.New(), items: map[string]*list.Element{}}
}

func (c *SegmentCache) get(key string) (int64, []byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.items[key]; ok {
		c.ll.MoveToFront(e)
		it := e.Value.(*cacheItem)
		return it.off, it.data, true
	}
	return 0, nil, false
}

func (c *SegmentCache) put(key string, off int64, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.items[key]; ok {
		return
	}
	c.items[key] = c.ll.PushFront(&cacheItem{key, off, data})
	c.size += int64(len(data))
	for c.size > c.max && c.ll.Len() > 1 {
		e := c.ll.Back()
		it := e.Value.(*cacheItem)
		c.ll.Remove(e)
		delete(c.items, it.key)
		c.size -= int64(len(it.data))
	}
}

// FileReader reads byte ranges of one share file straight from Usenet: the segments
// covering a range are fetched a few ahead in parallel (sem caps the connections used),
// decrypted and sliced. A packed file is cut out of its shared article; an empty file
// reads as nothing. Every byte is GCM-authenticated; a packed file is also hash-checked.
type FileReader struct {
	Index  *share.Index
	File   share.FileEntry
	Pool   *Pool
	Cache  *SegmentCache
	Sem    chan struct{}
	Window int
	plan   *share.Plan
	aead   cipher.AEAD
}

func NewFileReader(ix *share.Index, fe share.FileEntry, pool *Pool, cache *SegmentCache, sem chan struct{}) (*FileReader, error) {
	aead, err := NewAEAD(ix.Key)
	if err != nil {
		return nil, err
	}
	r := &FileReader{Index: ix, File: fe, Pool: pool, Cache: cache, Sem: sem, Window: 6, aead: aead}
	if fe.SegCount > 0 {
		if r.plan, err = ix.Plan(fe); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (r *FileReader) fetch(seg share.Segment, checkSize bool) ([]byte, error) {
	r.Sem <- struct{}{}
	defer func() { <-r.Sem }()
	var data []byte
	var err error
	for attempt := 0; ; attempt++ { // a pooled connection the server dropped fails on first use: retry on a new one
		var c *nntp.Conn
		if c, err = r.Pool.Get(nil); err != nil {
			return nil, err
		}
		data, err = FetchAndDecrypt(c, seg, r.aead, checkSize)
		if err == nil || IsArticleErr(err) {
			r.Pool.Put(c)
			break
		}
		c.Close()
		if attempt == 2 {
			return nil, err
		}
		r.Pool.DropIdle(c.Account)
	}
	if err != nil && len(r.Pool.Accounts.Backup) > 0 {
		if d := r.Pool.BackupFetch(seg, r.aead, checkSize); d != nil {
			return d, nil
		}
	}
	return data, err
}

func (r *FileReader) segment(i int64) (int64, []byte, error) {
	key := fmt.Sprintf("%s|%d|%d", r.Index.Path, r.File.FID, i)
	if off, d, ok := r.Cache.get(key); ok {
		return off, d, nil
	}
	seg, err := r.Index.Segment(r.plan, i)
	if err != nil {
		return 0, nil, err
	}
	d, err := r.fetch(seg, true)
	if err != nil {
		return 0, nil, err
	}
	r.Cache.put(key, seg.Offset, d)
	return seg.Offset, d, nil
}

func (r *FileReader) packed() ([]byte, error) {
	key := fmt.Sprintf("%s|pack|%d", r.Index.Path, *r.File.Pack)
	_, plain, ok := r.Cache.get(key)
	if !ok {
		seg, err := r.Index.PackSegment(*r.File.Pack)
		if err != nil {
			return nil, err
		}
		if plain, err = r.fetch(seg, false); err != nil {
			return nil, err
		}
		r.Cache.put(key, 0, plain)
	}
	data, start, members, err := ParsePack(plain)
	if err != nil {
		return nil, err
	}
	return MemberOf(data, start, members, r.File)
}

// WriteRange writes bytes [start, end) of the file to w, in order.
func (r *FileReader) WriteRange(w io.Writer, start, end int64) error {
	end = min(end, r.File.Size)
	if start >= end {
		return nil
	}
	if r.plan == nil {
		d, err := r.packed()
		if err != nil {
			return err
		}
		_, err = w.Write(d[start:end])
		return err
	}
	i0, err := r.Index.SegmentAt(r.plan, start)
	if err != nil {
		return err
	}
	i1, err := r.Index.SegmentAt(r.plan, end-1)
	if err != nil {
		return err
	}
	type res struct {
		off  int64
		data []byte
		err  error
	}
	window := max(1, r.Window)
	pending := make([]chan res, 0, window)
	next := i0
	launch := func() {
		ch := make(chan res, 1)
		i := next
		next++
		go func() { off, d, err := r.segment(i); ch <- res{off, d, err} }()
		pending = append(pending, ch)
	}
	for next <= i1 && len(pending) < window {
		launch()
	}
	for len(pending) > 0 {
		x := <-pending[0]
		pending = pending[1:]
		if next <= i1 {
			launch()
		}
		if x.err != nil {
			return x.err
		}
		lo := max(start-x.off, 0)
		hi := min(end-x.off, int64(len(x.data)))
		if _, err := w.Write(x.data[lo:hi]); err != nil {
			return err
		}
	}
	return nil
}
