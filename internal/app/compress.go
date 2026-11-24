package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

// Compressing the share index cache. On Windows the cache files are handed to the file
// system's own compression (XPRESS16K, as `compact /c /exe:xpress16k` does): an index takes
// about half the space or less, a 2.3 GB one compresses in seconds, and reads work as before
// (the first read of a page decompresses it, then it is cached like any other). Only indexes
// nothing has open are compressed. Writing to a compressed file makes Windows decompress it
// (a share's folder tree being added later, say), and the next pass compresses it again.

// compressRun: a pass is running (compressing) and another was asked for meanwhile (again).
type compressRun struct{ running, again atomic.Bool }

// CompressIdleIndexes compresses, in the background, the cache's indexes that no open share
// or job is using. Called at startup and whenever a share is closed.
func (m *Manager) CompressIdleIndexes() {
	if !canCompress {
		return
	}
	m.compress.again.Store(true)
	if !m.compress.running.CompareAndSwap(false, true) {
		return // the running pass goes round once more
	}
	go func() {
		for m.compress.again.Swap(false) {
			m.compressPass()
		}
		m.compress.running.Store(false)
		if m.compress.again.Load() { // asked for while stopping
			m.CompressIdleIndexes()
		}
	}()
}

// indexInUse: an open share or a job holds the index (the same test as dropIndex).
func (m *Manager) indexInUse(p string) bool {
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

func (m *Manager) compressPass() {
	dir := m.IndexDir()
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	n := 0
	var before, after int64
	for _, e := range ents {
		if m.shuttingDown.Load() {
			return
		}
		if !strings.HasSuffix(e.Name(), ".usnindex") { // a build in progress has a longer name
			continue
		}
		p := filepath.Join(dir, e.Name())
		info, err := e.Info()
		if err != nil || info.Size() < 1<<20 || m.indexInUse(p) {
			continue
		}
		size := info.Size()
		if diskSize(p, size) < size*9/10 { // compressed already
			continue
		}
		if err := compressFile(p); err != nil {
			continue // another file system (FAT32, exFAT, a network drive), or not worth it
		}
		n++
		before += size
		after += diskSize(p, size)
	}
	if n > 0 {
		m.Log(fmt.Sprintf("share index cache: compressed %d index(es), %s now take %s", n, sizeText(before), sizeText(after)))
	}
}

// sizeText: "1.17 GB", "48.2 MB".
func sizeText(n int64) string {
	if n >= 1e9 {
		return fmt.Sprintf("%.2f GB", float64(n)/1e9)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/1e6)
}

// IndexCacheStats: the share index cache's files, their size and the space they take on disk.
func (m *Manager) IndexCacheStats() map[string]any {
	dir := m.IndexDir()
	ents, _ := os.ReadDir(dir)
	var files int
	var size, disk int64
	for _, e := range ents {
		if !strings.Contains(e.Name(), ".usnindex") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files++
		size += info.Size()
		disk += diskSize(filepath.Join(dir, e.Name()), info.Size())
	}
	return map[string]any{"path": dir, "files": files, "size": size, "on_disk": disk, "compresses": canCompress}
}
