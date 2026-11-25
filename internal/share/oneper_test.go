package share

import (
	"sort"
	"strings"
	"testing"
)

func names(fs []GameFile) string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Name)
	}
	sort.Strings(out)
	return strings.Join(out, " | ")
}

func files(ns ...string) []GameFile {
	var out []GameFile
	for _, n := range ns {
		out = append(out, GameFile{Name: n, Path: "roms/x/" + n})
	}
	return out
}

func TestBestRelease(t *testing.T) {
	us := []string{"USA", "World", "Europe", "Japan"}
	eu := []string{"Europe", "World", "USA", "Japan"}
	for _, x := range []struct {
		files  []GameFile
		prefer []string
		want   string
	}{
		{files("Super Metroid (USA).sfc", "Super Metroid (Europe) (En,Fr,De).sfc", "Super Metroid (Japan).sfc"), us, "Super Metroid (USA).sfc"},
		{files("Super Metroid (USA).sfc", "Super Metroid (Europe) (En,Fr,De).sfc", "Super Metroid (Japan).sfc"), eu, "Super Metroid (Europe) (En,Fr,De).sfc"},
		{files("Tetris (World).gb", "Tetris (Japan).gb"), us, "Tetris (World).gb"},                                      // no USA: World
		{files("Zelda (USA).nes", "Zelda (USA) (Rev 1).nes", "Zelda (USA) (Rev 2).nes"), us, "Zelda (USA) (Rev 2).nes"}, // the newest revision
		{files("Game (USA) (Beta).sfc", "Game (Japan).sfc"), us, "Game (Japan).sfc"},                                    // a real release over a beta
		{files("Game (U) [!].smc", "Game (U) [b1].smc", "Game (U) [h1C].smc"), us, "Game (U) [!].smc"},                  // GoodTools: verified, not bad or hacked
		{files("Game (USA, Europe).md", "Game (Japan).md"), eu, "Game (USA, Europe).md"},                                // two regions: its best counts
		{files("FF7 (USA) (Disc 1).chd", "FF7 (USA) (Disc 2).chd", "FF7 (USA) (Disc 3).chd", "FF7 (Europe) (Disc 1).chd", "FF7 (Europe) (Disc 2).chd"), us,
			"FF7 (USA) (Disc 1).chd | FF7 (USA) (Disc 2).chd | FF7 (USA) (Disc 3).chd"}, // every disc of the one release
		{files("Rayman (USA).cue", "Rayman (USA) (Track 1).bin", "Rayman (USA) (Track 2).bin", "Rayman (Europe).cue", "Rayman (Europe) (Track 1).bin"), us,
			"Rayman (USA) (Track 1).bin | Rayman (USA) (Track 2).bin | Rayman (USA).cue"}, // a cue and its tracks
		{files("Odd Game.zip"), us, "Odd Game.zip"}, // one release, no tags
	} {
		if got := names(BestRelease(x.files, x.prefer)); got != x.want {
			t.Errorf("%v %v:\n got %s\nwant %s", names(x.files), x.prefer, got, x.want)
		}
	}
}
