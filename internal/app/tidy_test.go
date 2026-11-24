package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"usnfetch/internal/share"
)

// buildIndex: a small full index (two files in two folders) at path.
func buildIndex(t *testing.T, path string) {
	t.Helper()
	b, err := share.NewBuilder(path)
	if err != nil {
		t.Fatal(err)
	}
	d := &share.Doc{SegSize: 100}
	for i := 0; i < 4; i++ {
		d.Pool = append(d.Pool, share.PoolEntry{Msgid: fmt.Sprintf("m%d@x", i), Nonce: "00112233445566778899aabb", Tag: "00112233445566778899aabbccddeeff", Size: 100})
	}
	d.Files = []share.FileRec{{FileID: "aa", Path: "Games/a.bin", Size: 200, Segments: []int64{0, 1}},
		{FileID: "bb", Path: "Media/b.png", Size: 200, Segments: []int64{2, 3}}}
	if err := b.ApplyDoc(d, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Finish(nil); err != nil {
		t.Fatal(err)
	}
}

func TestTidyIndexCache(t *testing.T) {
	m, err := NewManager(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Store.Close)
	dir := m.IndexDir()
	idle, open := filepath.Join(dir, "f1-aaaa.usnindex"), filepath.Join(dir, "f2-bbbb.usnindex")
	buildIndex(t, idle)
	buildIndex(t, open)
	leftover := filepath.Join(dir, "f3-cccc.usnindex.123-4.tmp")
	os.WriteFile(leftover, []byte("x"), 0o644)
	ix, err := share.OpenIndex(open, nil) // a share someone has open
	if err != nil {
		t.Fatal(err)
	}

	m.Settings.IndexCacheGB = 20 // within the limit: full indexes stay full
	m.tidyPass(true)
	if !exists(idle) || exists(leftover) {
		t.Fatalf("within the limit: idle full index kept %v, leftover removed %v", exists(idle), !exists(leftover))
	}

	m.Settings.IndexCacheGB = 0 // keep only file lists of what nothing uses
	m.tidyPass(false)
	if exists(idle) || exists(share.ListPath(idle)) || !exists(share.ListPath(idle)+share.GzExt) {
		t.Fatalf("over the limit: full %v, list %v, zipped %v", exists(idle), exists(share.ListPath(idle)), exists(share.ListPath(idle)+share.GzExt))
	}
	if !exists(open) || exists(share.ListPath(open)+share.GzExt) {
		t.Error("an index in use was cut down")
	}
	ix.Close()

	// the zipped list opens for browsing after unzipping, without the download data
	list := share.ListPath(idle)
	if err := share.GunzipFile(list); err != nil {
		t.Fatal(err)
	}
	lx, err := share.OpenIndex(list, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, folders, _, err := lx.ListDir("", 0, 10, share.SortName); err != nil || len(folders) != 2 || lx.HasPool {
		t.Errorf("file list: %v folders, pool %v, %v", len(folders), lx.HasPool, err)
	}
	lx.Close()

	// a full index of the same share again (fetched for a download): its file list goes
	buildIndex(t, idle)
	m.Settings.IndexCacheGB = 20
	m.tidyPass(false)
	if !exists(idle) || exists(list) || exists(list+share.GzExt) {
		t.Errorf("full again: full %v, list %v, zipped %v", exists(idle), exists(list), exists(list+share.GzExt))
	}
	st := m.IndexCacheStats()
	if st["full"] != 2 || st["lists"] != 0 {
		t.Errorf("stats %v", st)
	}
}
