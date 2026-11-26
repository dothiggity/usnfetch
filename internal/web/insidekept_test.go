package web

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"usnfetch/internal/share"
)

// Opening a share again reuses its systems, packed count and game and BIOS lists when they
// came from the same index and the list files are the ones written then; a new index (a
// re-posted share), newer code, or list files rewritten for another post or removed by a
// reset mean working them out again.
func TestInsideKept(t *testing.T) {
	s := testServer(t)
	ix := &share.Index{Path: filepath.Join("x", "abc-123.usnindex"), Info: map[string]any{"folder_id": "abc"}}
	writeList(s.gamesFile("abc"), []share.Game{{Title: "Turbo Badger", System: "Orbit 16", Folder: "roms/orbit", Name: "Turbo Badger.o16"}})
	writeList(s.biosFile("abc"), []share.Bios{{Name: "orbit_boot.rom", Path: "bios/orbit_boot.rom"}})
	stored := func() map[string]any {
		return map[string]any{"inside_of": "abc-123", "inside_v": float64(insideVersion), "packed": float64(7), "lists": s.listsMark("abc")}
	}
	info := stored()
	if !s.insideKept(ix, info, "abc") {
		t.Error("same index, same lists: should be kept")
	}
	if !s.insideKept(&share.Index{Path: filepath.Join("x", "abc-123.usnlist"), Info: ix.Info}, info, "abc") {
		t.Error("the saved file list of the same index: should be kept")
	}
	if s.insideKept(&share.Index{Path: filepath.Join("x", "abc-456.usnindex"), Info: ix.Info}, info, "abc") {
		t.Error("a re-posted share's new index: should be worked out again")
	}
	old := stored()
	old["inside_v"] = float64(insideVersion - 1)
	if s.insideKept(ix, old, "abc") {
		t.Error("worked out by older code: should be worked out again")
	}
	noPacked := stored()
	delete(noPacked, "packed")
	if s.insideKept(ix, noPacked, "abc") {
		t.Error("no packed count saved: should be worked out again")
	}
	// another post of the same folder rewrote the lists
	time.Sleep(20 * time.Millisecond)
	writeList(s.gamesFile("abc"), []share.Game{{Title: "Other", System: "Orbit 16", Folder: "roms/orbit", Name: "Other.o16"}, {Title: "More", Name: "More.o16"}})
	if s.insideKept(ix, info, "abc") {
		t.Error("lists rewritten for another post: should be worked out again")
	}
	// a reset removed a build's BIOS list (no games: only the BIOS file tells)
	_ = os.Remove(s.gamesFile("abc"))
	biosOnly := stored()
	if !s.insideKept(ix, biosOnly, "abc") {
		t.Error("BIOS-only build, list in place: should be kept")
	}
	_ = os.Remove(s.biosFile("abc"))
	if s.insideKept(ix, biosOnly, "abc") {
		t.Error("its BIOS list removed by a reset: should be worked out again")
	}
}
