package web

import (
	"strings"
	"testing"

	"usnfetch/internal/share"
)

func score(q, title string) int {
	s, ok := titleScore(strings.Fields(share.TitleKey(q)), prep(title))
	if !ok {
		return 0
	}
	return max(s, 1)
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
	// an abbreviation stands for all its words: the game itself above a longer title around it
	if score("smb3", "Super Mario Bros. 3") <= score("smb3", "Super Mario Advance 4 - Super Mario Bros. 3") {
		t.Error("smb3: the game itself should rank above Super Mario Advance 4")
	}
	if score("smb3", "Super Mario Bros. 3") <= score("smb3", "smb3") {
		t.Error("smb3: Super Mario Bros. 3 should rank above an arcade set named smb3")
	}
}

// TestGameRanking: what the builds opened so far gave back, in the order it should come.
func TestGameRanking(t *testing.T) {
	type g struct {
		title, system string
		builds        int
	}
	rank := func(q string, games []g) []string {
		toks := strings.Fields(share.TitleKey(q))
		var out []*gameHit
		for _, x := range games {
			p := prep(x.title)
			s, ok := titleScore(toks, p)
			if !ok {
				t.Fatalf("%q doesn't match %q", q, x.title)
			}
			out = append(out, &gameHit{Title: x.title, System: x.system, Builds: make([]gameBuild, x.builds), score: s,
				key: strings.Join(p.words, " "), words: len(p.words)})
		}
		rankGames(out)
		var names []string
		for _, h := range out {
			names = append(names, h.Title+" · "+h.System)
		}
		return names
	}
	pos := func(names []string, name string) int {
		for i, n := range names {
			if n == name {
				return i
			}
		}
		return -1
	}

	mario := rank("mario 3", []g{
		{"Mario Hoops 3 on 3", "Nintendo DS", 3},
		{"Excitebike - Bunbun Mario Battle - Stadium 3", "Satellaview", 3},
		{"Super Mario Advance 3 - Yoshi's Island", "Game Boy Advance", 3},
		{"Super Mario Bros 3+", "Wii", 3},
		{"Super Mario Bros. 3", "NES", 3},
		{"Mario Party 3", "Nintendo 64", 3},
		{"Super Mario Advance 4 - Super Mario Bros. 3", "Game Boy Advance", 3},
		{"Super Mario 3D World + Bowser's Fury", "Switch", 2},
		{"Super Mario 3D World", "Wii U", 2},
		{"Super Mario Bros 3", "NES (hacks)", 1},
	})
	if mario[0] != "Mario Party 3 · Nintendo 64" || mario[1] != "Super Mario Bros. 3 · NES" || mario[2] != "Super Mario Bros 3+ · Wii" {
		t.Errorf("mario 3: %q", mario)
	}
	if pos(mario, "Excitebike - Bunbun Mario Battle - Stadium 3 · Satellaview") < pos(mario, "Mario Hoops 3 on 3 · Nintendo DS") {
		t.Errorf("mario 3: a title with the words in its subtitle should come after Mario Hoops 3 on 3: %q", mario)
	}
	if pos(mario, "Super Mario Bros 3 · NES (hacks)") != len(mario)-1 {
		t.Errorf("mario 3: the hack should come last: %q", mario)
	}

	smb := rank("smb3", []g{
		{"Super Mario Bros 3+", "Wii", 3},
		{"Super Mario Advance 4 - Super Mario Bros. 3", "Game Boy Advance", 3},
		{"smb3", "Arcade", 1},
		{"Super Mario Bros. 3", "NES", 3},
		{"Super Mario Bros III", "Commodore 64", 1},
	})
	if smb[0] != "Super Mario Bros. 3 · NES" {
		t.Errorf("smb3: %q", smb)
	}

	zelda := rank("zelda ocarina", []g{
		{"Zelda Ocarina Of Time PC", "Windows", 1},
		{"Legend of Zelda, The - Ocarina of Time - Master Quest", "GameCube", 3},
		{"Legend of Zelda, The - Ocarina of Time", "Nintendo 64", 3},
		{"Legend of Zelda, The - Ocarina of Time 3D", "Nintendo 3DS", 2},
	})
	if zelda[0] != "Legend of Zelda, The - Ocarina of Time · Nintendo 64" {
		t.Errorf("zelda ocarina: %q", zelda)
	}

	ff := rank("ff7", []g{
		{"Dirge of Cerberus - Final Fantasy VII", "PlayStation 2", 3},
		{"FINAL FANTASY VII", "Switch", 3},
		{"Final Fantasy VII", "PlayStation", 3},
		{"Crisis Core - Final Fantasy VII", "PSP", 3},
	})
	if ff[0] != "Final Fantasy VII · PlayStation" || ff[1] != "FINAL FANTASY VII · Switch" {
		t.Errorf("ff7: %q", ff)
	}
}

func TestGameTitles(t *testing.T) {
	for in, want := range map[string]string{
		"Super Mario Bros. 3 (USA) (Rev 1) [!].nes":  "Super Mario Bros. 3",
		"Crash Bandicoot (Track 2).bin":              "Crash Bandicoot",
		"Final Fantasy VII (USA) (Disc 1).chd":       "Final Fantasy VII",
		"Dr. Mario":                                  "Dr. Mario", // no file type: nothing cut
		"Some Game.ps3":                              "Some Game",
		"Super.Mario.3D.World.USA.wux":               "Super Mario 3D World",
		"Final Fantasy VII - Jenova Coronoa.easyrpg": "Final Fantasy VII - Jenova Coronoa",
		"Super_Mario_Bros._3.nes":                    "Super Mario Bros 3",
		"Super Mario 3D World (USA)":                 "Super Mario 3D World",
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
	for in, want := range map[string]string{"Flash Games": "Flash", "flash": "Flash", "easyrpg": "EasyRPG"} {
		if got := share.SystemName(in); got != want {
			t.Errorf("SystemName(%q) = %q, want %q", in, got, want)
		}
	}
}
