package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"usnfetch/internal/share"
)

// A saved index the cache has compressed stays compressed while a build is opened and browsed
// from it: opening a file for writing makes Windows unpack it, seconds for a big one.
func TestCompressedIndexOpensWithoutUnpacking(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f0-1.usnindex")
	b, err := share.NewBuilder(p)
	if err != nil {
		t.Fatal(err)
	}
	d := &share.Doc{SegSize: 100}
	for i := range 60000 {
		d.Files = append(d.Files, share.FileRec{FileID: fmt.Sprint(i), Path: fmt.Sprintf("Games/Orbit 16/Made Up Game %05d (USA).o16", i)}) // size 0: in the tree without segments
	}
	if err := b.ApplyDoc(d, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Finish(nil); err != nil {
		t.Fatal(err)
	}
	ix, err := share.OpenIndex(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ix.EnsureTree(nil); err != nil { // the first open builds the folder tree (a write)
		t.Fatal(err)
	}
	ix.Close()

	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	size := st.Size()
	if err := compressFile(p); err != nil {
		t.Skipf("no file compression here: %v", err)
	}
	packed := diskSize(p, size)
	if packed >= size*9/10 {
		t.Fatalf("on disk %d after compressing, size %d", packed, size)
	}
	for range 2 { // opened again, as a build you've browsed before is
		ix, err := share.OpenIndex(p, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := ix.EnsureTree(nil); err != nil {
			t.Fatal(err)
		}
		if s, err := ix.Summary(); err != nil || s.Files != 60000 {
			t.Fatalf("summary %+v, %v", s, err)
		}
		if f, err := ix.FileByPath("Games/Orbit 16/Made Up Game 04242 (USA).o16"); err != nil || f == nil {
			t.Fatalf("a file by its path: %v, %v", f, err)
		}
		if dir, err := ix.Dir("Games/Orbit 16"); err != nil || dir == nil || dir.Count != 60000 {
			t.Fatalf("its folder: %+v, %v", dir, err)
		}
		ix.Close()
		if now := diskSize(p, size); now != packed {
			t.Fatalf("on disk %d after opening it, %d compressed (size %d): opening it unpacked it", now, packed, size)
		}
	}
}
