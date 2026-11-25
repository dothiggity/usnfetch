package share

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
)

// testTree: an index of these paths (no segments), its folder tree built.
func testTree(t *testing.T, paths []string) *Index {
	t.Helper()
	p := filepath.Join(t.TempDir(), "f-1.usnindex")
	b, err := NewBuilder(p)
	if err != nil {
		t.Fatal(err)
	}
	d := &Doc{SegSize: 100}
	for i, path := range paths {
		d.Files = append(d.Files, FileRec{FileID: string(rune('a' + i)), Path: path, Size: 0})
	}
	if err := b.ApplyDoc(d, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Finish(nil); err != nil {
		t.Fatal(err)
	}
	ix, err := OpenIndex(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ix.Close)
	if err := ix.EnsureTree(nil); err != nil {
		t.Fatal(err)
	}
	return ix
}

// A search narrowed to a kind of file: only such files (by name ending, any case), no
// folders, and with no words every one of them under the scope.
func TestSearchKind(t *testing.T) {
	ix := testTree(t, []string{"Movies/a.mp4", "Movies/B.MKV", "Movies/notes.txt", "Movies/Extras/c.mp4",
		"Movies/mp4 info/readme.txt", "Pics/x.jpg", "Pics/mp4.png"})
	names := func(r *SearchResult) []string {
		var out []string
		for _, h := range r.Files {
			out = append(out, filepath.Base(h.File.Path))
		}
		return out
	}
	ctx := context.Background()
	r, err := ix.SearchKind(ctx, "", "", 100, SortName, FileKinds["video"])
	if err != nil {
		t.Fatal(err)
	}
	if got := names(r); !slices.Equal(got, []string{"c.mp4", "a.mp4", "B.MKV"}) && !slices.Equal(got, []string{"a.mp4", "B.MKV", "c.mp4"}) || r.FileMatches != 3 || len(r.Folders) != 0 {
		t.Errorf("all videos: %v (%d), folders %v", got, r.FileMatches, r.Folders)
	}
	if r, _ := ix.SearchKind(ctx, "", "Movies/Extras", 100, SortName, FileKinds["video"]); !slices.Equal(names(r), []string{"c.mp4"}) {
		t.Errorf("videos in Movies/Extras: %v", names(r))
	}
	if r, _ := ix.SearchKind(ctx, "b", "", 100, SortName, FileKinds["video"]); !slices.Equal(names(r), []string{"B.MKV"}) {
		t.Errorf("videos matching b: %v", names(r))
	}
	if r, _ := ix.SearchKind(ctx, "mp4", "", 100, SortName, FileKinds["picture"]); !slices.Equal(names(r), []string{"mp4.png"}) {
		t.Errorf("pictures matching mp4: %v", names(r))
	}
	// no kind: as before, folders too, and nothing for no words
	if r, _ := ix.Search(ctx, "extras", "", 100, SortName); len(r.Folders) != 1 || r.Folders[0].Path != "Movies/Extras" {
		t.Errorf("folder search: %+v", r.Folders)
	}
	if r, _ := ix.Search(ctx, "", "", 100, SortName); r.FileMatches != 0 || len(r.Folders) != 0 {
		t.Errorf("an empty search found %d files", r.FileMatches)
	}
}
