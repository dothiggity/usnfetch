package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMediaDest(t *testing.T) {
	rb := Library{Kind: LibRetroBat, Path: `D:\RetroBat`}
	snes := filepath.Join(rb.Path, "roms", "snes")
	for _, x := range []struct{ role, ext, want, tag string }{
		{"cover", ".PNG", filepath.Join(snes, "images", "Super Metroid (USA)-thumb.png"), "thumbnail"},
		{"screenshot", ".png", filepath.Join(snes, "images", "Super Metroid (USA)-image.png"), "image"},
		{"marquee", ".png", filepath.Join(snes, "images", "Super Metroid (USA)-marquee.png"), "marquee"},
		{"video", ".mp4", filepath.Join(snes, "videos", "Super Metroid (USA)-video.mp4"), "video"},
		{"manual", ".pdf", filepath.Join(snes, "manuals", "Super Metroid (USA)-manual.pdf"), "manual"},
		{"nonsense", ".png", "", ""},
	} {
		if d, tag := rb.MediaDest(snes, "Super Metroid (USA).sfc", "Super Metroid", x.role, x.ext); d != x.want || tag != x.tag {
			t.Errorf("RetroBat %s: %q %q, want %q %q", x.role, d, tag, x.want, x.tag)
		}
	}
	// a game in a subfolder of the system folder: its media beside it
	if d, _ := rb.MediaDest(snes, filepath.Join("Hacks", "Super Metroid Redesign.sfc"), "x", "cover", ".png"); d != filepath.Join(snes, "Hacks", "images", "Super Metroid Redesign-thumb.png") {
		t.Errorf("subfolder: %q", d)
	}
	es := Library{Kind: LibESDE, Path: `D:\Emulation\roms`, Data: `D:\Emulation\tools\downloaded_media`}
	if d, tag := es.MediaDest(filepath.Join(es.Path, "snes"), "Super Metroid (USA).sfc", "Super Metroid", "cover", ".png"); d != filepath.Join(es.Data, "snes", "covers", "Super Metroid (USA).png") || tag != "" {
		t.Errorf("ES-DE cover: %q %q", d, tag)
	}
	if d, _ := (Library{Kind: LibESDE, Path: es.Path}).MediaDest(filepath.Join(es.Path, "snes"), "x.sfc", "x", "cover", ".png"); d != "" {
		t.Errorf("ES-DE without a media folder: %q", d)
	}
	lb := Library{Kind: LibLaunchBox, Path: `D:\LaunchBox`}
	plat := filepath.Join(lb.Path, "Games", "Super Nintendo Entertainment System")
	if d, _ := lb.MediaDest(plat, "x.sfc", "Disney's 102 Dalmatians: Puppies to the Rescue", "cover", ".png"); d != filepath.Join(lb.Path, "Images", "Super Nintendo Entertainment System", "Box - Front", "Disney_s 102 Dalmatians_ Puppies to the Rescue-01.png") {
		t.Errorf("LaunchBox cover: %q", d)
	}
	if d, _ := lb.MediaDest(plat, "x.sfc", "Zelda", "manual", ".pdf"); d != filepath.Join(lb.Path, "Manuals", "Super Nintendo Entertainment System", "Zelda-01.pdf") {
		t.Errorf("LaunchBox manual: %q", d)
	}
}

func TestGameListFor(t *testing.T) {
	root := t.TempDir()
	rb := Library{Kind: LibRetroBat, Path: root}
	snes := filepath.Join(root, "roms", "snes")
	_ = os.MkdirAll(snes, 0o755)
	if got := rb.GameListFor(snes); got != filepath.Join(snes, "gamelist.xml") {
		t.Errorf("none yet: %q", got)
	}
	own := filepath.Join(root, "emulationstation", ".emulationstation", "gamelists", "snes", "gamelist.xml")
	_ = os.MkdirAll(filepath.Dir(own), 0o755)
	_ = os.WriteFile(own, []byte("<gameList/>"), 0o644)
	if got := rb.GameListFor(snes); got != own {
		t.Errorf("EmulationStation's own: %q", got)
	}
	_ = os.WriteFile(filepath.Join(snes, "gamelist.xml"), []byte("<gameList/>"), 0o644)
	if got := rb.GameListFor(snes); got != filepath.Join(snes, "gamelist.xml") {
		t.Errorf("beside the games wins: %q", got)
	}
	if (Library{Kind: LibESDE, Path: root}).GameListFor(snes) != "" || (Library{Kind: LibLaunchBox, Path: root}).GameListFor(snes) != "" {
		t.Error("ES-DE's and LaunchBox's lists aren't edited")
	}
}

func TestEsFields(t *testing.T) {
	lb := []byte(`<LaunchBox><Game><Title>Sonic</Title><ApplicationPath>Games\Sega Genesis\Sonic (USA).md</ApplicationPath><Notes>Fast.</Notes>
		<ReleaseDate>1991-06-23T00:00:00-07:00</ReleaseDate><CommunityStarRating>4</CommunityStarRating><MaxPlayers>2</MaxPlayers><Favorite>true</Favorite></Game></LaunchBox>`)
	src := srcEntries("launchbox", lb)
	f, ok := src["sonic (usa).md"]
	if !ok {
		t.Fatalf("keys: %v", src)
	}
	got := map[string]string{}
	for _, x := range esFields("launchbox", f) {
		got[x[0]] = x[1]
	}
	want := map[string]string{"name": "Sonic", "desc": "Fast.", "releasedate": "19910623T000000", "rating": "0.80", "players": "2"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q, want %q", k, got[k], v)
		}
	}
	es := []byte(`<gameList><game><path>./x.sfc</path><name>X</name><favorite>true</favorite><playcount>3</playcount><image>./images/x.png</image><genre>RPG</genre></game></gameList>`)
	for _, x := range esFields("es", srcEntries("es", es)["x.sfc"]) {
		if x[0] == "favorite" || x[0] == "playcount" || x[0] == "image" || x[0] == "path" {
			t.Errorf("%s carried over", x[0])
		}
	}
}
