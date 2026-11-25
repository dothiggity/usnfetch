package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLibrarySystems(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"roms/snes", "roms/megadrive", "roms/genesis", "roms/psx", "bios"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	rb := Library{Kind: LibRetroBat, Path: root}
	have, err := rb.Systems()
	if err != nil {
		t.Fatal(err)
	}
	if got := have["SNES"]; len(got) != 1 || got[0] != "snes" {
		t.Errorf("SNES: %v", got)
	}
	if got := have["Mega Drive / Genesis"]; len(got) != 2 {
		t.Errorf("Mega Drive / Genesis: %v", got)
	}
	for _, x := range []struct{ system, src, want string }{
		{"SNES", "Super Nintendo Entertainment System", "snes"}, // a LaunchBox build's SNES game: the device's own folder
		{"Mega Drive / Genesis", "genesis", "genesis"},          // two folders for it: the one named like the build's
		{"Mega Drive / Genesis", "Sega Genesis", "megadrive"},   // else the usual one
		{"Nintendo 64", "Nintendo 64", "n64"},                   // a system it doesn't have yet: the usual name
		{"Some Odd System", "oddsys", "oddsys"},                 // one nobody knows: the build's name
	} {
		if got := rb.SystemFolder(x.system, x.src, have); got != x.want {
			t.Errorf("RetroBat %s (from %q): %q, want %q", x.system, x.src, got, x.want)
		}
	}
	if rb.BiosDir() != filepath.Join(root, "bios") || rb.RomsDir() != filepath.Join(root, "roms") {
		t.Error("RetroBat folders")
	}

	lb := Library{Kind: LibLaunchBox, Path: t.TempDir()}
	_ = os.MkdirAll(filepath.Join(lb.Path, "Data", "Platforms"), 0o755)
	_ = os.WriteFile(filepath.Join(lb.Path, "Data", "Platforms", "Super Nintendo Entertainment System.xml"), []byte("<LaunchBox/>"), 0o644)
	lhave, _ := lb.Systems()
	if got := lb.SystemFolder("SNES", "snes", lhave); got != "Super Nintendo Entertainment System" {
		t.Errorf("LaunchBox SNES: %q", got)
	}
	if got := lb.SystemFolder("PlayStation", "psx", lhave); got != "Sony Playstation" {
		t.Errorf("LaunchBox PlayStation: %q", got)
	}
	if lb.BiosDir() != "" {
		t.Error("LaunchBox has no one BIOS folder")
	}

	es := Library{Kind: LibESDE, Path: filepath.Join(t.TempDir(), "Emulation", "roms")}
	if es.BiosDir() != filepath.Join(filepath.Dir(es.Path), "bios") {
		t.Errorf("EmuDeck bios: %q", es.BiosDir())
	}
	if got := es.SystemFolder("GameCube", "gamecube", nil); got != "gc" {
		t.Errorf("ES-DE GameCube: %q", got)
	}
}

func TestDefaultRefresh(t *testing.T) {
	for _, x := range []struct{ kind, path, want string }{
		{LibBatocera, `\\BATOCERA\share`, "http://BATOCERA:1234/reloadgames"},
		{LibBatocera, `\\192.168.1.40\share`, "http://192.168.1.40:1234/reloadgames"},
		{LibBatocera, `Z:\`, "http://batocera:1234/reloadgames"},
		{LibRetroBat, `D:\RetroBat`, "http://127.0.0.1:1234/reloadgames"},
		{LibESDE, `D:\Emulation\roms`, ""},
		{LibLaunchBox, `D:\LaunchBox`, ""},
	} {
		if got := DefaultRefresh(x.kind, x.path); got != x.want {
			t.Errorf("%s %s: %q, want %q", x.kind, x.path, got, x.want)
		}
	}
}

func TestCheckLibrary(t *testing.T) {
	root := t.TempDir()
	if c := CheckLibrary(Library{Kind: LibRetroBat, Path: root}); c["ok"] != true || c["warn"] == nil {
		t.Errorf("an empty folder: ok with a warning, got %v", c)
	}
	_ = os.MkdirAll(filepath.Join(root, "roms", "snes"), 0o755)
	if c := CheckLibrary(Library{Kind: LibRetroBat, Path: root}); c["ok"] != true || c["systems"] != 1 || c["warn"] != nil {
		t.Errorf("one system: %v", c)
	}
	if c := CheckLibrary(Library{Kind: LibRetroBat, Path: filepath.Join(root, "nope")}); c["ok"] == true {
		t.Error("a folder that isn't there")
	}
	if c := CheckLibrary(Library{Kind: "kodi", Path: root}); c["ok"] == true {
		t.Error("an unknown kind")
	}
	if c := CheckLibrary(Library{Kind: LibBatocera, Path: root, Refresh: "javascript:alert(1)"}); c["ok"] == true {
		t.Error("a refresh address that isn't a web address")
	}
}
