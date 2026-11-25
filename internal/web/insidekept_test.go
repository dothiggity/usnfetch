package web

import (
	"os"
	"path/filepath"
	"testing"

	"usnfetch/internal/share"
)

// Opening a share again reuses its systems, packed count and game lists when they came from
// the same index and are still there; a new index (re-posted share), new code, or lists a
// reset removed mean working them out again.
func TestInsideKept(t *testing.T) {
	s := testServer(t)
	ix := &share.Index{Path: filepath.Join("x", "abc-123.usnindex"), Info: map[string]any{"folder_id": "abc"}}
	stored := func(inside []any) map[string]any {
		return map[string]any{"inside_of": "abc-123", "inside_v": float64(insideVersion), "packed": float64(7), "inside": inside}
	}
	games := []any{map[string]any{"name": "Orbit 16", "folder": "roms/orbit", "games": float64(3)}}

	if !s.insideKept(ix, stored([]any{})) {
		t.Error("no games, same index: should be kept")
	}
	if s.insideKept(ix, stored(games)) {
		t.Error("its game list isn't on disk: should be worked out again")
	}
	p := s.gamesFile("abc")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !s.insideKept(ix, stored(games)) {
		t.Error("same index, list on disk: should be kept")
	}
	if !s.insideKept(&share.Index{Path: filepath.Join("x", "abc-123.usnlist"), Info: ix.Info}, stored(games)) {
		t.Error("the saved file list of the same index: should be kept")
	}
	if s.insideKept(&share.Index{Path: filepath.Join("x", "abc-456.usnindex"), Info: ix.Info}, stored(games)) {
		t.Error("a re-posted share's new index: should be worked out again")
	}
	old := stored(games)
	old["inside_v"] = float64(insideVersion - 1)
	if s.insideKept(ix, old) {
		t.Error("worked out by older code: should be worked out again")
	}
	noPacked := stored(games)
	delete(noPacked, "packed")
	if s.insideKept(ix, noPacked) {
		t.Error("no packed count saved: should be worked out again")
	}
}
