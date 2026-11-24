package web

import (
	"strings"
	"testing"

	"usnfetch/internal/share"
)

func score(q, title string) int {
	p := prep(title)
	total := 0
	for _, t := range strings.Fields(share.TitleKey(q)) {
		s := tokenScore(t, p)
		if s == 0 {
			return 0
		}
		total += s
	}
	return total
}

func TestGameMatching(t *testing.T) {
	for _, c := range []struct {
		q, title string
		want     bool
	}{
		{"mario 3", "Super Mario Bros. 3", true},
		{"mario 3", "Super Mario 3D World", true},
		{"mario 3", "Mario Kart 64", false},
		{"mario 3", "Dr. Mario", false},
		{"smb3", "Super Mario Bros. 3", true},
		{"sm64", "Super Mario 64", true},
		{"mk64", "Mario Kart 64", true},
		{"ff7", "Final Fantasy VII", true},
		{"final fantasy 7", "Final Fantasy VII", true},
		{"zelda 2", "Zelda II - The Adventure of Link", true},
		{"loz", "The Legend of Zelda", true},
		{"loz", "Lots of Zombies", true}, // abbreviations are loose by nature
		{"pokemon red", "Pokémon - Red Version", true},
		{"street fighter", "Super Street Fighter II Turbo", true},
		{"ocarina", "The Legend of Zelda - Ocarina of Time", true},
		{"zelda", "Super Mario World", false},
	} {
		if got := score(c.q, c.title) > 0; got != c.want {
			t.Errorf("%q in %q: %v, want %v", c.q, c.title, got, c.want)
		}
	}
	// a whole-word match ranks above the start of a word
	if score("mario 3", "Super Mario Bros. 3") <= score("mario 3", "Super Mario 3D World") {
		t.Error("Super Mario Bros. 3 should rank above Super Mario 3D World for \"mario 3\"")
	}
}

func TestGameTitles(t *testing.T) {
	for in, want := range map[string]string{
		"Super Mario Bros. 3 (USA) (Rev 1) [!].nes": "Super Mario Bros. 3",
		"Crash Bandicoot (Track 2).bin":             "Crash Bandicoot",
		"Final Fantasy VII (USA) (Disc 1).chd":      "Final Fantasy VII",
		"Dr. Mario":                                 "Dr. Mario", // no file type: nothing cut
		"Some Game.ps3":                             "Some Game",
	} {
		if got := share.GameTitle(in); got != want {
			t.Errorf("GameTitle(%q) = %q, want %q", in, got, want)
		}
	}
	if k := share.TitleKey("Pokémon - Red & Blue"); k != "pokemon red and blue" {
		t.Errorf("TitleKey: %q", k)
	}
	if k := share.TitleKey("Yoshi's Island"); k != "yoshis island" {
		t.Errorf("TitleKey keeps an apostrophe's word whole: %q", k)
	}
}
