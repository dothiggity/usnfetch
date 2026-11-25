package share

import (
	"path/filepath"
	"testing"
)

func TestDescribeBios(t *testing.T) {
	for _, c := range []struct {
		where, name           string
		size                  int64
		system, region, label string
		part                  bool
	}{
		{"", "scph5501.bin", 512 << 10, "PlayStation", "USA", "SCPH-5501", false},
		{"", "scph1001.bin", 512 << 10, "PlayStation", "USA", "SCPH-1001", false},
		{"", "scph5500.bin", 512 << 10, "PlayStation", "Japan", "SCPH-5500", false},
		{"", "SCPH7502.BIN", 512 << 10, "PlayStation", "Europe", "SCPH-7502", false},
		{"", "psxonpsp660.bin", 512 << 10, "PlayStation", "", "PS1 BIOS from the PSP (any region)", false},
		{"pcsx2/bios", "scph39001.bin", 4 << 20, "PlayStation 2", "USA", "SCPH-39001", false},
		{"pcsx2/bios", "scph39001.NVM", 1024, "PlayStation 2", "USA", "SCPH-39001", true},
		{"pcsx2/bios", "SCPH-70004_BIOS_V12_PAL_200.BIN", 4 << 20, "PlayStation 2", "Europe", "SCPH-70004", false},
		{"pcsx2/bios", "ps2-0230a-20080220.bin", 4 << 20, "PlayStation 2", "USA", "PS2 BIOS 2.30 (2008-02-20)", false},
		{"pcsx2/bios", "ps2-0200j-20040614.bin", 4 << 20, "PlayStation 2", "Japan", "PS2 BIOS 2.00 (2004-06-14)", false},
		{"", "PS2 Bios 30004R V6 Pal.bin", 4 << 20, "PlayStation 2", "Europe", "", false},
		{"", "bios_CD_U.bin", 128 << 10, "Mega CD / Sega CD", "USA", "Sega CD BIOS", false},
		{"", "eu_mcd1_9210.bin", 128 << 10, "Mega CD / Sega CD", "Europe", "Mega CD Model 1 BIOS (9210)", false},
		{"", "Bios Saturn 1.01a (U) [!].bin", 512 << 10, "Saturn", "USA", "", false},
		{"", "kick34005.A500", 256 << 10, "Amiga", "", "Kickstart 1.3 (A500, r34.5)", false},
		{"", "kick40060.CD32", 512 << 10, "Amiga CD32", "", "Kickstart 3.1 (CD32, r40.60)", false},
		{"Amiga", "Kickstart v1.3 r34.5 (1987)(Commodore)(A500-A1000-A2000-CDTV)[!].rom", 256 << 10, "Amiga", "", "", false},
		{"", "gba_bios.bin", 16 << 10, "Game Boy Advance", "", "GBA BIOS", false},
		{"dc", "dc_boot.bin", 2 << 20, "Dreamcast", "", "Dreamcast BIOS", false},
		{"dolphin-emu/Sys/GC/USA", "IPL.bin", 2 << 20, "GameCube", "USA", "", false},
		{"dolphin-emu/Sys/GC/EUR", "IPL.bin", 2 << 20, "GameCube", "Europe", "", false},
		{"", "7800 BIOS (U).rom", 4 << 10, "Atari 7800", "USA", "Atari 7800 BIOS", false},
		{"", "syscard3.pce", 256 << 10, "PC Engine CD / TurboGrafx-CD", "Japan", "System Card 3.0", false},
		{"fbneo", "neogeo.zip", 1 << 20, "Neo Geo", "", "Neo Geo BIOS", false},
		{"mame2003", "pgm.zip", 1 << 20, "Arcade", "", "", false},
		{"", "dsp1b.rom", 10 << 10, "SNES", "", "DSP1B chip ROM", false},
	} {
		b := DescribeBios(c.where, c.name, c.size)
		if b.System != c.system || b.Region != c.region || b.Label != c.label || b.Part != c.part {
			t.Errorf("%s/%s: %q %q %q part=%v, want %q %q %q part=%v", c.where, c.name, b.System, b.Region, b.Label, b.Part, c.system, c.region, c.label, c.part)
		}
	}
}

// matches: every word of q is a word of the file's, or starts one (or one of its folders').
func matches(q string, b Bios) bool {
	strong, weak := BiosWords(b)
	for _, t := range splitWords(TitleKey(q)) {
		ok := false
		for _, w := range append(strong, weak...) {
			ok = ok || len(w) >= len(t) && w[:len(t)] == t
		}
		if !ok {
			return false
		}
	}
	return true
}

func splitWords(s string) []string {
	var out []string
	w := ""
	for _, r := range s + " " {
		if r == ' ' {
			if w != "" {
				out = append(out, w)
			}
			w = ""
		} else {
			w += string(r)
		}
	}
	return out
}

func TestBiosWords(t *testing.T) {
	us := DescribeBios("", "scph5501.bin", 512<<10)
	eu := DescribeBios("", "scph5502.bin", 512<<10)
	ps2 := DescribeBios("pcsx2/bios", "scph39001.bin", 4<<20)
	for _, c := range []struct {
		q    string
		b    Bios
		want bool
	}{
		{"ps1 bios usa", us, true}, {"ps1 bios", us, true}, {"psx bios us", us, true}, {"playstation bios ntsc", us, true},
		{"ps1 bios usa", eu, false}, {"ps1 bios europe", eu, true}, {"ps1 bios pal", eu, true},
		{"ps1 bios usa", ps2, false}, {"ps2 bios usa", ps2, true}, {"playstation 2 bios", ps2, true}, {"scph5501", us, true},
		{"scph-5501", us, true}, {"pcsx2", ps2, true},
		{"sega cd bios usa", DescribeBios("", "bios_CD_U.bin", 128<<10), true},
		{"amiga 1.3", DescribeBios("", "kick34005.A500", 256<<10), true},
		{"gamecube bios usa", DescribeBios("dolphin-emu/Sys/GC/USA", "IPL.bin", 2<<20), true},
	} {
		if got := matches(c.q, c.b); got != c.want {
			s, w := BiosWords(c.b)
			t.Errorf("%q in %s: %v, want %v (%v / %v)", c.q, c.b.Name, got, c.want, s, w)
		}
	}
}

func TestBioses(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f-1.usnindex")
	b, err := NewBuilder(p)
	if err != nil {
		t.Fatal(err)
	}
	d := &Doc{SegSize: 100}
	for i, path := range []string{
		"RetroBat/bios/scph5501.bin", "RetroBat/bios/readme.txt", "RetroBat/bios/pcsx2/bios/ps2-0230a-20080220.bin",
		"RetroBat/bios/dc/dc_boot.bin", "RetroBat/roms/psx/Crash.chd", "LaunchBox/Emulators/RetroArch/system/gba_bios.bin",
		"Other/system/not-a-bios.bin",
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
	list, err := ix.Bioses()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, x := range list {
		got[x.Path] = x.System + "|" + x.Where
	}
	want := map[string]string{
		"RetroBat/bios/scph5501.bin":                        "PlayStation|",
		"RetroBat/bios/pcsx2/bios/ps2-0230a-20080220.bin":   "PlayStation 2|pcsx2/bios",
		"RetroBat/bios/dc/dc_boot.bin":                      "Dreamcast|dc",
		"LaunchBox/Emulators/RetroArch/system/gba_bios.bin": "Game Boy Advance|",
	}
	if len(got) != len(want) {
		t.Errorf("Bioses: %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("Bioses %s: %q, want %q", k, got[k], v)
		}
	}
}
