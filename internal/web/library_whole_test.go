package web

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"usnfetch/internal/app"
	"usnfetch/internal/share"
)

// Putting a whole share into a library goes through every file while looking up each game's
// pictures and folder in the same index. It used to wait forever: the listing held the
// index's only read connection that those lookups needed.
func TestInstallWholeShare(t *testing.T) {
	s := testServer(t)
	p := filepath.Join(t.TempDir(), "f0-1.usnindex")
	b, err := share.NewBuilder(p)
	if err != nil {
		t.Fatal(err)
	}
	d := &share.Doc{SegSize: 100}
	for i, path := range []string{"RetroBat/roms/snes/Turbo Badger (USA).sfc", "RetroBat/roms/snes/Gravity Orchard (Europe).sfc",
		"RetroBat/roms/snes/Gravity Orchard (USA).sfc", "RetroBat/roms/psx/Echo Lagoon/Echo Lagoon (Disc 1).chd",
		"RetroBat/bios/boot.rom"} {
		d.Files = append(d.Files, share.FileRec{FileID: string(rune('a' + i)), Path: path})
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
	defer ix.Close()
	if err := ix.EnsureTree(nil); err != nil {
		t.Fatal(err)
	}
	lib := app.Library{ID: "l", Name: "L", Kind: app.LibRetroBat, Path: t.TempDir()}
	type result struct {
		files int
		err   error
	}
	done := make(chan result, 1)
	go func() {
		pl, err := s.planInstall(context.Background(), ix, &selection{folders: []string{""}}, lib)
		r := result{err: err}
		if pl != nil {
			r.files = pl.Files
		}
		done <- r
	}()
	select {
	case r := <-done:
		if r.err != nil || r.files < 4 {
			t.Fatalf("planned %d files, %v", r.files, r.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("putting the whole share into a library never finished planning")
	}
}
