package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"usnfetch/internal/share"
)

// Upkeep of the share index cache (see internal/share/cache.go for the two forms an index is
// kept in). Shares never change, so nothing is dropped for its age: the full indexes used
// most recently are kept up to the size limit (Settings.IndexCacheGB, on disk); past it, the
// least recently used are cut down to their file list, which is zipped while idle and kept.
// On Windows, idle full indexes are also handed to the file system's own compression
// (XPRESS16K): reads work as before, and a write makes Windows decompress the file, after
// which the next pass compresses it again.

// listsLimit: zipped file lists kept at most (least recently used go first past it).
const listsLimit = int64(10) << 30

// artLimit: box art kept on disk (web/fullindex.go), least recently shown first past it.
const artLimit = int64(1) << 30

// tidyRun: a pass is running, and another was asked for meanwhile; the first pass ran.
type tidyRun struct{ running, again, first atomic.Bool }

// TidyIndexCache runs a pass over the index cache in the background. Called at startup, when
// a share is closed and when a job lets go of its index.
func (m *Manager) TidyIndexCache() {
	m.tidy.again.Store(true)
	if !m.tidy.running.CompareAndSwap(false, true) {
		return // the running pass goes round once more
	}
	go func() {
		first := true
		for m.tidy.again.Swap(false) {
			m.tidyPass(first && !m.tidy.first.Swap(true))
			first = false
		}
		m.tidy.running.Store(false)
		if m.tidy.again.Load() { // asked for while stopping
			m.TidyIndexCache()
		}
	}()
}

// indexInUse: an open share or a job holds the file.
func (m *Manager) indexInUse(p string) bool {
	if share.InUse(p) {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pinned[strings.ToLower(p)] > 0 {
		return true
	}
	for _, j := range m.jobs {
		if strings.EqualFold(j.IndexFile, p) {
			return true
		}
	}
	return false
}

type cacheFile struct {
	path       string
	size, disk int64
	used       time.Time
}

// cacheFiles: the cache's full indexes, plain file lists and zipped ones; and leftovers of
// builds and conversions that were interrupted.
func cacheFiles(dir string) (fulls, lists, zipped []cacheFile, leftovers []string) {
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		n, p := e.Name(), filepath.Join(dir, e.Name())
		if strings.Contains(n, ".tmp") {
			leftovers = append(leftovers, p)
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		c := cacheFile{p, info.Size(), diskSize(p, info.Size()), info.ModTime()}
		switch {
		case strings.HasSuffix(n, ".usnindex"):
			fulls = append(fulls, c)
		case strings.HasSuffix(n, ".usnlist"):
			lists = append(lists, c)
		case strings.HasSuffix(n, ".usnlist"+share.GzExt):
			zipped = append(zipped, c)
		}
	}
	return
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func (m *Manager) tidyPass(startup bool) {
	dir := m.IndexDir()
	fulls, lists, zipped, leftovers := cacheFiles(dir)
	if startup { // nothing of this process is being built yet
		for _, p := range leftovers {
			_ = os.Remove(p)
		}
	}
	// lock: one file at a time, and never one LoadShare is choosing or opening
	act := func(p string, fn func() error) bool {
		share.CacheMu.Lock()
		defer share.CacheMu.Unlock()
		if m.shuttingDown.Load() || !exists(p) || m.indexInUse(p) {
			return false
		}
		return fn() == nil
	}
	var converted, cut, zippedN, dropped int
	var before, after int64
	for _, f := range fulls {
		before += f.disk
	}
	// 1. indexes of the older format: converted once (about half the size), no download
	for _, f := range fulls {
		if sch, _ := share.FileInfo(f.path); sch == 2 && act(f.path, func() error { return share.Convert(f.path) }) {
			converted++
		}
	}
	// 2. file lists a full index has replaced
	for _, l := range append(append([]cacheFile{}, lists...), zipped...) {
		full := strings.TrimSuffix(strings.TrimSuffix(l.path, share.GzExt), ".usnlist") + ".usnindex"
		if sch, pool := share.FileInfo(full); sch == share.SchemaVersion && pool && act(l.path, func() error { return os.Remove(l.path) }) {
			dropped++
		}
	}
	// 3. Windows: idle full indexes compressed by the file system (before the limit is
	// checked: the limit counts the space they take on disk)
	if canCompress {
		fulls, _, _, _ = cacheFiles(dir)
		for _, f := range fulls {
			if f.size >= 1<<20 && f.disk >= f.size*9/10 {
				act(f.path, func() error { return compressFile(f.path) })
			}
		}
	}
	// 4. full indexes past the limit, least recently used first: cut down to their file list
	fulls, _, _, _ = cacheFiles(dir)
	sort.Slice(fulls, func(i, j int) bool { return fulls[i].used.Before(fulls[j].used) })
	var total int64
	for _, f := range fulls {
		total += diskSize(f.path, f.size)
	}
	limit := int64(m.Settings.IndexCacheGB) << 30
	for _, f := range fulls {
		if total <= limit {
			break
		}
		if act(f.path, func() error { return slimIndex(f.path) }) {
			total -= f.disk
			cut++
		}
	}
	// 5. idle file lists: zipped (about a quarter of the size)
	_, lists, _, _ = cacheFiles(dir)
	for _, l := range lists {
		if exists(l.path+share.GzExt) || act(l.path, func() error { return share.GzipFile(l.path) }) {
			zippedN++
		}
	}
	// 6. zipped lists past their own (generous) limit, least recently used first
	_, _, zipped, _ = cacheFiles(dir)
	sort.Slice(zipped, func(i, j int) bool { return zipped[i].used.After(zipped[j].used) })
	var zt int64
	for _, z := range zipped {
		if zt += z.size; zt > listsLimit {
			act(z.path, func() error { return os.Remove(z.path) })
		}
	}
	m.trimArt()
	fulls, lists, zipped, _ = cacheFiles(dir)
	for _, c := range append(append(fulls, lists...), zipped...) {
		after += c.disk
	}
	if converted+cut+zippedN+dropped > 0 || after < before*9/10 {
		var parts []string
		for _, p := range []struct {
			n    int
			what string
		}{{converted, "converted to the smaller format"}, {cut, "cut down to the file list"}, {zippedN, "file lists zipped"}, {dropped, "file lists replaced by the full index"}} {
			if p.n > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", p.n, p.what))
			}
		}
		m.Log(fmt.Sprintf("share index cache: %s; %s on disk now", strings.Join(parts, ", "), sizeText(after)))
	}
}

// artFiles: the box art kept on disk, most recently shown first, and their total size.
func (m *Manager) artFiles() ([]cacheFile, int64) {
	var out []cacheFile
	var total int64
	_ = filepath.WalkDir(filepath.Join(m.DataDir, "art_cache"), func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			out = append(out, cacheFile{p, info.Size(), info.Size(), info.ModTime()})
			total += info.Size()
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].used.After(out[j].used) })
	return out, total
}

// trimArt keeps the most recently shown box art within artLimit.
func (m *Manager) trimArt() {
	files, _ := m.artFiles()
	var kept int64
	for _, f := range files {
		if kept += f.size; kept > artLimit {
			_ = os.Remove(f.path)
		}
	}
}

// slimIndex cuts a full index down to its file list (building its folder tree first if it was
// only ever used by a download) and removes the full index.
func slimIndex(full string) error {
	ix, err := share.OpenIndex(full, nil)
	if err != nil {
		return err
	}
	err = ix.EnsureTree(nil)
	ix.Close()
	if err != nil {
		return err
	}
	list := share.ListPath(full)
	_ = os.Remove(list + share.GzExt)
	if err := share.Slim(full, list); err != nil {
		return err
	}
	return os.Remove(full)
}

// sizeText: "1.17 GB", "48.2 MB".
func sizeText(n int64) string {
	if n >= 1e9 {
		return fmt.Sprintf("%.2f GB", float64(n)/1e9)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/1e6)
}

// IndexCacheStats: what the share index cache holds and the space it takes on disk.
func (m *Manager) IndexCacheStats() map[string]any {
	dir := m.IndexDir()
	fulls, lists, zipped, _ := cacheFiles(dir)
	sum := func(cs []cacheFile) (n int64) {
		for _, c := range cs {
			n += c.disk
		}
		return
	}
	var size int64
	for _, c := range append(append(fulls, lists...), zipped...) {
		size += c.size
	}
	art, artSize := m.artFiles()
	return map[string]any{"path": dir, "full": len(fulls), "lists": len(lists) + len(zipped), "files": len(fulls) + len(lists) + len(zipped),
		"full_disk": sum(fulls), "lists_disk": sum(lists) + sum(zipped), "on_disk": sum(fulls) + sum(lists) + sum(zipped), "size": size,
		"limit": int64(m.Settings.IndexCacheGB) << 30, "compresses": canCompress, "art": len(art), "art_disk": artSize}
}
