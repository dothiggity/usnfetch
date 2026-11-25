package share

import (
	"path/filepath"
	"testing"
)

func TestInside(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f-1.usnindex")
	b, err := NewBuilder(p)
	if err != nil {
		t.Fatal(err)
	}
	d := &Doc{SegSize: 100}
	for i, path := range []string{
		"RetroBat/roms/snes/Mario.sfc", "RetroBat/roms/snes/Zelda.sfc", "RetroBat/roms/snes/gamelist.xml",
		"RetroBat/roms/snes/images/Mario.png", "RetroBat/roms/snes/_info.txt",
		"RetroBat/roms/psx/Crash.cue", "RetroBat/roms/psx/Crash (Track 1).bin", "RetroBat/roms/psx/Crash (Track 2).bin",
		"RetroBat/roms/n64/Mario 64.z64", "RetroBat/roms/ps3/Some Game.ps3/PS3_GAME/EBOOT.BIN", "RetroBat/roms/megadrive/Sonic.md",
		// look-alikes: bezels laid out per system, an emulator's title-id folders
		"RetroBat/decorations/bezels/games/snes/a.png", "RetroBat/decorations/bezels/games/psx/b.png",
		"RetroBat/decorations/bezels/games/n64/c.png", "RetroBat/decorations/bezels/games/nes/d.png",
		"RetroBat/saves/switch/games/0100aa/s.bin", "RetroBat/saves/switch/games/0100bb/s.bin",
	} {
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
	defer ix.Close()
	if err := ix.EnsureTree(nil); err != nil {
		t.Fatal(err)
	}
	got, err := ix.Inside()
	if err != nil {
		t.Fatal(err)
	}
	want := []System{{"SNES", "RetroBat/roms/snes", 2}, {"Mega Drive / Genesis", "RetroBat/roms/megadrive", 1}, {"Nintendo 64", "RetroBat/roms/n64", 1},
		{"PlayStation", "RetroBat/roms/psx", 1}, {"PlayStation 3", "RetroBat/roms/ps3", 1}}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("system %d: %+v, want %+v", i, got[i], want[i])
		}
	}
	// a game's files, to download it from a search: its disc tracks, not its artwork; a game folder
	files, err := ix.GameFiles("RetroBat/roms/psx", "Crash.cue")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range files {
		names = append(names, f.Path)
	}
	if len(names) != 3 || names[0] != "RetroBat/roms/psx/Crash (Track 1).bin" || names[2] != "RetroBat/roms/psx/Crash.cue" {
		t.Errorf("GameFiles Crash: %q", names)
	}
	if files, _ := ix.GameFiles("RetroBat/roms/snes", "Mario.sfc"); len(files) != 1 || files[0].Path != "RetroBat/roms/snes/Mario.sfc" {
		t.Errorf("GameFiles Mario (not its box art): %+v", files)
	}
	if files, _ := ix.GameFiles("RetroBat/roms/ps3", "Some Game.ps3"); len(files) != 1 || !files[0].Folder || files[0].Count != 1 {
		t.Errorf("GameFiles PS3 folder: %+v", files)
	}
	if files, _ := ix.GameFiles("RetroBat/roms/snes", "Metroid.sfc"); len(files) != 0 {
		t.Errorf("GameFiles of a game that isn't there: %+v", files)
	}
	if !looksLikeSystem("Nintendo 64") || !looksLikeSystem("Sony Playstation 2") || looksLikeSystem("0100aa") || looksLikeSystem("Taisen Hot Gimmick 5") {
		t.Error("looksLikeSystem")
	}
}
