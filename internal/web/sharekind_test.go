package web

import (
	"path/filepath"
	"testing"

	"usnfetch/internal/share"
)

// A share's own Games and BIOS search: its lists, matched as on the Search page, limited to
// the folder searched in, best first. A game of several files (regions here) is one item
// with them all; a game kept in a folder of its own comes back as that folder.
func TestShareKindSearch(t *testing.T) {
	s := testServer(t)
	p := filepath.Join(t.TempDir(), "f0-1.usnindex")
	b, err := share.NewBuilder(p)
	if err != nil {
		t.Fatal(err)
	}
	d := &share.Doc{SegSize: 100}
	for i, path := range []string{"RetroBat/roms/orbit/Turbo Badger (USA).o16", "RetroBat/roms/orbit/Gravity Orchard (Europe).o16",
		"RetroBat/roms/orbit/Gravity Orchard (USA).o16", "RetroBat/roms/vortex/Echo Lagoon/Echo Lagoon (Disc 1).chd",
		"RetroBat/bios/orbit_boot.rom"} {
		d.Files = append(d.Files, share.FileRec{FileID: string(rune('a' + i)), Path: path}) // size 0: in the folder tree without segments
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
	ix.Info["folder_id"] = "f0"
	writeList(s.gamesFile("f0"), []share.Game{
		{Title: "Turbo Badger", System: "Orbit 16", Folder: "RetroBat/roms/orbit", Name: "Turbo Badger (USA).o16", Count: 1},
		{Title: "Gravity Orchard", System: "Orbit 16", Folder: "RetroBat/roms/orbit", Name: "Gravity Orchard (Europe).o16", Count: 2},
		{Title: "Echo Lagoon", System: "Vortex CD", Folder: "RetroBat/roms/vortex", Name: "Echo Lagoon", Count: 1},
	})
	writeList(s.biosFile("f0"), []share.Bios{{Name: "orbit_boot.rom", Path: "RetroBat/bios/orbit_boot.rom", System: "Orbit 16", Label: "Boot ROM", Known: true}})
	ss := &session{ID: "x", Link: "usn://test", Title: "Test build"}
	items := func(r map[string]any) []map[string]any { return r["items"].([]map[string]any) }

	r := s.shareKindSearch(ss, ix, "games", "turbo", "", 300)
	if it := items(r); len(it) != 1 || it[0]["type"] != "file" || it[0]["name"] != "Turbo Badger (USA).o16" || it[0]["system"] != "Orbit 16" || r["file_matches"] != 1 {
		t.Errorf("games matching turbo: %v", r)
	}
	r = s.shareKindSearch(ss, ix, "games", "gravity", "", 300)
	if it := items(r); len(it) != 1 || it[0]["type"] != "game" || len(it[0]["parts"].([]map[string]any)) != 2 {
		t.Errorf("a game in two regions should be one item with both files: %v", r)
	}
	r = s.shareKindSearch(ss, ix, "games", "", "", 300)
	if r["file_matches"] != 3 || len(items(r)) != 3 {
		t.Errorf("every game: %v", r)
	}
	r = s.shareKindSearch(ss, ix, "games", "", `\RetroBat\roms\vortex\`, 300)
	if it := items(r); r["file_matches"] != 1 || len(it) != 1 || it[0]["type"] != "folder" || it[0]["path"] != "RetroBat/roms/vortex/Echo Lagoon" {
		t.Errorf("games in the vortex folder: %v", r)
	}
	r = s.shareKindSearch(ss, ix, "bios", "orbit", "", 300)
	if it := items(r); len(it) != 1 || it[0]["label"] != "Boot ROM" {
		t.Errorf("BIOS matching orbit: %v", r)
	}
	if r := s.shareKindSearch(ss, ix, "games", "!!", "", 300); r["file_matches"] != 0 {
		t.Errorf("only punctuation matched %v games", r["file_matches"])
	}
	ix.Info["folder_id"] = "nolist"
	if r := s.shareKindSearch(ss, ix, "games", "", "", 300); r["no_list"] != true || r["file_matches"] != 0 {
		t.Errorf("a build with no game list: %v", r)
	}
}
