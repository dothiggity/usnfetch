package share

import (
	"database/sql"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// BIOS files: what a build keeps in its BIOS folders, with the console, region and what each
// one is, so a search for "ps1 bios usa" finds scph1001.bin and scph5501.bin. Emulators know
// BIOS files by name, and those names rarely say what they are, so the well-known ones are
// listed here (and families of them, like Sony's model numbers, go by rule); for the rest,
// the console and region come from the words in the name and the folders around it.

// Bios is one file in a build's BIOS folders.
type Bios struct {
	Name   string `json:"n"`
	Path   string `json:"p"` // in the share
	Size   int64  `json:"z"`
	System string `json:"s,omitempty"` // for people, as SystemName; "" when not known
	Region string `json:"r,omitempty"` // USA, Europe, Japan, … ("" for any or not known)
	Label  string `json:"l,omitempty"` // what it is: "SCPH-5501", "Kickstart 1.3 (A500)"
	Known  bool   `json:"k,omitempty"` // a BIOS file known by its name, not only by where it is
	Part   bool   `json:"x,omitempty"` // a side file of a BIOS (a PS2 BIOS's NVM or MEC)
	Where  string `json:"w,omitempty"` // its folder under the BIOS folder ("dolphin-emu/Sys/GC/USA")
}

const (
	biosMaxSub = 5000  // a subfolder of a BIOS folder with more files than this is something else (PCSX2's texture packs)
	biosMax    = 40000 // BIOS files kept per build, at most
)

// notBios: file types in BIOS folders that are notes, pictures or settings.
var notBios = map[string]bool{".txt": true, ".xml": true, ".ini": true, ".cfg": true, ".xls": true, ".xlsx": true, ".pdf": true,
	".html": true, ".htm": true, ".url": true, ".lnk": true, ".db": true, ".log": true, ".json": true, ".yml": true, ".yaml": true,
	".md": true, ".sha": true, ".md5": true, ".sfv": true, ".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".bmp": true,
	".ico": true, ".bak": true, ".old": true}

// Bioses: every file in the build's BIOS folders (any folder named bios, and RetroArch's
// system folder), described. Nil when it has none.
func (ix *Index) Bioses() ([]Bios, error) {
	type dir struct {
		did        int64
		name, path string
		count      int64
	}
	query := func(q string, args ...any) ([]dir, error) {
		rows, err := ix.db.Query(q, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []dir
		for rows.Next() {
			var d dir
			var n sql.NullInt64
			if err := rows.Scan(&d.did, &d.name, &d.path, &n); err != nil {
				return nil, err
			}
			d.count = n.Int64
			out = append(out, d)
		}
		return out, rows.Err()
	}
	roots, err := query("SELECT did, name, path, count FROM dirs WHERE lower(name) = 'bios' OR lower(path) LIKE '%retroarch/system' ORDER BY path")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []Bios
	var walk func(d dir, where string) error
	walk = func(d dir, where string) error {
		rows, err := ix.db.Query("SELECT df.name, f.path, f.size FROM dir_files df JOIN files f ON f.fid = df.fid WHERE df.did = ? ORDER BY df.name", d.did)
		if err != nil {
			return err
		}
		for rows.Next() && len(out) < biosMax {
			var name, p string
			var size sql.NullInt64
			if err := rows.Scan(&name, &p, &size); err != nil {
				rows.Close()
				return err
			}
			if seen[p] || notBios[strings.ToLower(path.Ext(name))] {
				continue
			}
			seen[p] = true
			b := DescribeBios(where, name, size.Int64)
			b.Path = p
			out = append(out, b)
		}
		rows.Close()
		subs, err := query("SELECT did, name, path, count FROM dirs WHERE parent = ? ORDER BY name", d.did)
		if err != nil {
			return err
		}
		for _, s := range subs {
			if s.count <= biosMaxSub && len(out) < biosMax {
				if err := walk(s, strings.TrimPrefix(where+"/"+s.name, "/")); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, r := range roots {
		if err := walk(r, ""); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ---- describing one ---------------------------------------------------------------------

type biosInfo struct{ system, region, label string }

// biosKnown: BIOS files emulators look for by name (lower case).
var biosKnown = map[string]biosInfo{
	// PlayStation (model numbers go by rule: scphRE)
	"psxonpsp660.bin": {"PlayStation", "", "PS1 BIOS from the PSP (any region)"},
	"ps1_rom.bin":     {"PlayStation", "", "PS1 BIOS from the PS3 (any region)"},
	"ps3updat.pup":    {"PlayStation 3", "", "PS3 system software"},
	"psvupdat.pup":    {"PS Vita", "", "PS Vita system software"},
	"psp2updat.pup":   {"PS Vita", "", "PS Vita system software"},
	// Sega
	"bios_cd_u.bin":      {"Mega CD / Sega CD", "USA", "Sega CD BIOS"},
	"bios_cd_e.bin":      {"Mega CD / Sega CD", "Europe", "Mega CD BIOS"},
	"bios_cd_j.bin":      {"Mega CD / Sega CD", "Japan", "Mega CD BIOS"},
	"bios_md.bin":        {"Mega Drive / Genesis", "", "Mega Drive boot ROM (TMSS)"},
	"genesis_os_rom.bin": {"Mega Drive / Genesis", "", "Genesis boot ROM (TMSS)"},
	"bios_u.sms":         {"Master System", "USA", "Master System BIOS"},
	"bios_e.sms":         {"Master System", "Europe", "Master System BIOS"},
	"bios_j.sms":         {"Master System", "Japan", "Master System BIOS"},
	"bios.gg":            {"Game Gear", "", "Game Gear BIOS"},
	"32x_g_bios.bin":     {"32X", "", "32X BIOS (68000)"},
	"32x_m_bios.bin":     {"32X", "", "32X BIOS (master SH-2)"},
	"32x_s_bios.bin":     {"32X", "", "32X BIOS (slave SH-2)"},
	"sega_101.bin":       {"Saturn", "Japan", "Saturn BIOS"},
	"mpr-17933.bin":      {"Saturn", "USA / Europe", "Saturn BIOS"},
	"saturn_bios.bin":    {"Saturn", "", "Saturn BIOS"},
	"mpr-18811-mx.ic1":   {"Saturn", "", "The King of Fighters '95 cartridge ROM"},
	"mpr-19367-mx.ic1":   {"Saturn", "", "Ultraman cartridge ROM"},
	"dc_boot.bin":        {"Dreamcast", "", "Dreamcast BIOS"},
	"dc_bios.bin":        {"Dreamcast", "", "Dreamcast BIOS"},
	"dc_flash.bin":       {"Dreamcast", "", "Dreamcast flash ROM (settings)"},
	"dc_nvmem.bin":       {"Dreamcast", "", "Dreamcast flash ROM (settings)"},
	"naomi.zip":          {"NAOMI", "", "NAOMI BIOS"},
	"naomi.bin":          {"NAOMI", "", "NAOMI BIOS"},
	"naomi2.zip":         {"NAOMI 2", "", "NAOMI 2 BIOS"},
	"hod2bios.zip":       {"NAOMI", "", "The House of the Dead 2 BIOS"},
	"f355bios.zip":       {"NAOMI", "", "Ferrari F355 Challenge BIOS"},
	"f355dlx.zip":        {"NAOMI", "", "Ferrari F355 Challenge (deluxe) BIOS"},
	"airlbios.zip":       {"NAOMI", "", "Airline Pilots BIOS"},
	"awbios.zip":         {"Atomiswave", "", "Atomiswave BIOS"},
	"stvbios.zip":        {"Sega ST-V", "", "ST-V BIOS"},
	// Nintendo
	"gba_bios.bin":          {"Game Boy Advance", "", "GBA BIOS"},
	"gb_bios.bin":           {"Game Boy", "", "Game Boy boot ROM"},
	"dmg_boot.bin":          {"Game Boy", "", "Game Boy boot ROM"},
	"gbc_bios.bin":          {"Game Boy Color", "", "Game Boy Color boot ROM"},
	"cgb_boot.bin":          {"Game Boy Color", "", "Game Boy Color boot ROM"},
	"sgb_bios.bin":          {"SNES", "", "Super Game Boy BIOS"},
	"sgb.boot.rom":          {"SNES", "", "Super Game Boy boot ROM"},
	"sgb2.boot.rom":         {"SNES", "", "Super Game Boy 2 boot ROM"},
	"bios7.bin":             {"Nintendo DS", "", "DS BIOS (ARM7)"},
	"bios9.bin":             {"Nintendo DS", "", "DS BIOS (ARM9)"},
	"biosnds7.bin":          {"Nintendo DS", "", "DS BIOS (ARM7)"},
	"biosnds9.bin":          {"Nintendo DS", "", "DS BIOS (ARM9)"},
	"firmware.bin":          {"Nintendo DS", "", "DS firmware"},
	"dsfirmware.bin":        {"Nintendo DS", "", "DS firmware"},
	"drastic_bios_arm7.bin": {"Nintendo DS", "", "DS BIOS (ARM7, for DraStic)"},
	"drastic_bios_arm9.bin": {"Nintendo DS", "", "DS BIOS (ARM9, for DraStic)"},
	"dsi_bios7.bin":         {"Nintendo DS", "", "DSi BIOS (ARM7)"},
	"dsi_bios9.bin":         {"Nintendo DS", "", "DSi BIOS (ARM9)"},
	"dsi_firmware.bin":      {"Nintendo DS", "", "DSi firmware"},
	"dsifirmware.bin":       {"Nintendo DS", "", "DSi firmware"},
	"dsi_nand.bin":          {"Nintendo DS", "", "DSi NAND"},
	"disksys.rom":           {"Famicom Disk System", "Japan", "Famicom Disk System BIOS"},
	"64dd_ipl.bin":          {"Nintendo 64", "Japan", "64DD IPL"},
	"bs-x.bin":              {"SNES", "Japan", "Satellaview BS-X BIOS"},
	"stbios.bin":            {"SNES", "Japan", "Sufami Turbo BIOS"},
	"bios.min":              {"Pokémon Mini", "", "Pokémon Mini BIOS"},
	"prod.keys":             {"Switch", "", "Switch keys (prod.keys)"},
	"title.keys":            {"Switch", "", "Switch title keys"},
	"aes_keys.txt":          {"Nintendo 3DS", "", "3DS AES keys"},
	// NEC
	"pcfx.rom":     {"PC-FX", "Japan", "PC-FX BIOS"},
	"pcfx.bios":    {"PC-FX", "Japan", "PC-FX BIOS"},
	"pcfxbios.bin": {"PC-FX", "Japan", "PC-FX BIOS"},
	"gexpress.pce": {"PC Engine CD / TurboGrafx-CD", "Japan", "Games Express card"},
	// SNK
	"neogeo.zip":  {"Neo Geo", "", "Neo Geo BIOS"},
	"neocdz.zip":  {"Neo Geo CD", "", "Neo Geo CDZ BIOS"},
	"neocd.zip":   {"Neo Geo CD", "", "Neo Geo CD BIOS"},
	"neocd.bin":   {"Neo Geo CD", "", "Neo Geo CD BIOS"},
	"neocd_z.rom": {"Neo Geo CD", "", "Neo Geo CDZ BIOS"},
	// 3DO
	"panafz1.bin":         {"3DO", "", "Panasonic FZ-1 BIOS"},
	"panafz10.bin":        {"3DO", "", "Panasonic FZ-10 BIOS"},
	"panafz10-norsa.bin":  {"3DO", "", "Panasonic FZ-10 BIOS"},
	"panafz10e-anvil.bin": {"3DO", "Europe", "Panasonic FZ-10 BIOS"},
	"panafz1-kanji.bin":   {"3DO", "Japan", "Panasonic FZ-1 Kanji ROM"},
	"goldstar.bin":        {"3DO", "", "GoldStar BIOS"},
	"sanyotry.bin":        {"3DO", "", "Sanyo TRY BIOS"},
	"3do_arcade_saot.bin": {"3DO", "", "3DO arcade BIOS"},
	// Atari
	"5200.rom":          {"Atari 5200", "", "Atari 5200 BIOS"},
	"5200a.rom":         {"Atari 5200", "", "Atari 5200 BIOS (alternate)"},
	"7800 bios (u).rom": {"Atari 7800", "USA", "Atari 7800 BIOS"},
	"7800 bios (e).rom": {"Atari 7800", "Europe", "Atari 7800 BIOS"},
	"lynxboot.img":      {"Atari Lynx", "", "Lynx boot ROM"},
	"atarixl.rom":       {"Atari 800", "", "Atari XL/XE OS"},
	"atariosa.rom":      {"Atari 800", "", "Atari 400/800 OS A"},
	"atariosb.rom":      {"Atari 800", "", "Atari 400/800 OS B"},
	"ataribas.rom":      {"Atari 800", "", "Atari BASIC"},
	"tos.img":           {"Atari ST", "", "Atari ST TOS"},
	// others
	"coleco.rom":       {"ColecoVision", "", "ColecoVision BIOS"},
	"colecovision.rom": {"ColecoVision", "", "ColecoVision BIOS"},
	"exec.bin":         {"Intellivision", "", "Intellivision Executive ROM"},
	"grom.bin":         {"Intellivision", "", "Intellivision Graphics ROM"},
	"ecs.bin":          {"Intellivision", "", "Intellivision ECS ROM"},
	"ivoice.bin":       {"Intellivision", "", "Intellivoice ROM"},
	"o2rom.bin":        {"Odyssey 2", "", "Odyssey 2 BIOS"},
	"c52.bin":          {"Odyssey 2", "Europe", "Videopac+ G7000 BIOS"},
	"g7400.bin":        {"Odyssey 2", "Europe", "Videopac+ G7400 BIOS"},
	"jopac.bin":        {"Odyssey 2", "Europe", "Jopac BIOS"},
	"sl31253.bin":      {"Channel F", "", "Channel F BIOS"},
	"sl31254.bin":      {"Channel F", "", "Channel F BIOS"},
	"sl90025.bin":      {"Channel F", "", "Channel F II BIOS"},
	"iplrom.dat":       {"X68000", "Japan", "X68000 IPL ROM"},
	"iplrom30.dat":     {"X68000", "Japan", "X68000 IPL ROM 3.0"},
	"cgrom.dat":        {"X68000", "Japan", "X68000 character ROM"},
	"mcpx_1.0.bin":     {"Xbox", "", "Xbox MCPX boot ROM"},
	"complex_4627.bin": {"Xbox", "", "Xbox BIOS (Complex 4627)"},
}

var (
	scphRE  = regexp.MustCompile(`(?i)^(scph|dtlh)[-_ ]?(\d{3,5})`)
	ps2RE   = regexp.MustCompile(`(?i)^ps2-(\d\d)(\d\d)([a-z])[a-z]?-(\d{4})(\d\d)(\d\d)`)
	kickRE  = regexp.MustCompile(`(?i)^kick(\d\d)(\d{3})\.(a\d+|cd32|cdtv)`)
	kickVRE = regexp.MustCompile(`(?i)^kick(\d)(\d)\.rom$`)
	scdRE   = regexp.MustCompile(`(?i)^(us|eu|jp)_(scd|mcd)(\d)_(\d+)\.bin$`)
	cardRE  = regexp.MustCompile(`(?i)^syscard(\d)(u?)\.pce$`)
	chipRE  = regexp.MustCompile(`(?i)^(dsp1b?|dsp[234]|cx4|st01[018])(\.(data|program))?\.rom$`)
	msxRE   = regexp.MustCompile(`(?i)^msx(2p|2|tr)?`)
	uniRE   = regexp.MustCompile(`(?i)uni-?bios`)
	tagRgRE = regexp.MustCompile(`(?i)[(\[](u|us|usa|e|eu|eur|europe|j|jp|jap|japan|w|world|k|kr|korea)[)\],]|_(u|e|j)\.[a-z0-9]+$`)
)

// kickstarts: Amiga Kickstart revisions, by the number in kickNNNNN files.
var kickstarts = map[string]string{"33180": "1.2", "33192": "1.2", "34005": "1.3", "37175": "2.04", "37350": "2.05", "39106": "3.0",
	"40060": "3.1", "40063": "3.1", "40068": "3.1"}

// sideFile: a BIOS's companions next to its main file (PCSX2 keeps the NVRAM and the MEC).
func sideFile(ext string) bool {
	switch ext {
	case ".bin", ".rom", ".rom0", ".zip", ".7z", ".img":
		return false
	}
	return true
}

// DescribeBios: what a file in a BIOS folder is. where: its folder under the BIOS folder.
func DescribeBios(where, name string, size int64) Bios {
	b := Bios{Name: name, Size: size, Where: where}
	low, ext := strings.ToLower(name), strings.ToLower(path.Ext(name))
	if k, ok := biosKnown[low]; ok {
		b.System, b.Region, b.Label, b.Known = k.system, k.region, k.label, true
	} else if m := scphRE.FindStringSubmatch(name); m != nil {
		// Sony model numbers: SCPH-5501 (PS1, 3 or 4 digits, region in the last one),
		// SCPH-39001 (PS2, 5 digits, region in the last two)
		d := m[2]
		b.Label, b.Known, b.Part = strings.ToUpper(m[1])+"-"+d, true, sideFile(ext)
		if len(d) <= 4 {
			b.System, b.Region = "PlayStation", map[byte]string{'0': "Japan", '1': "USA", '2': "Europe"}[d[len(d)-1]]
		} else {
			b.System, b.Region = "PlayStation 2", map[string]string{"00": "Japan", "01": "USA", "02": "Australia", "03": "UK", "04": "Europe"}[d[3:]]
		}
	} else if m := ps2RE.FindStringSubmatch(name); m != nil {
		major, _ := strconv.Atoi(m[1])
		b.System, b.Known, b.Part = "PlayStation 2", true, sideFile(ext)
		b.Region = map[string]string{"a": "USA", "e": "Europe", "j": "Japan", "h": "Asia", "c": "China", "r": "Russia"}[strings.ToLower(m[3])]
		b.Label = fmt.Sprintf("PS2 BIOS %d.%s (%s-%s-%s)", major, m[2], m[4], m[5], m[6])
	} else if m := kickRE.FindStringSubmatch(name); m != nil {
		model := strings.ToUpper(m[3])
		b.System, b.Known = "Amiga", true
		if model == "CD32" {
			b.System = "Amiga CD32"
		}
		rev := m[1] + "." + strings.TrimLeft(m[2], "0")
		if v := kickstarts[m[1]+m[2]]; v != "" {
			b.Label = fmt.Sprintf("Kickstart %s (%s, r%s)", v, model, rev)
		} else {
			b.Label = fmt.Sprintf("Kickstart r%s (%s)", rev, model)
		}
		b.Part = strings.Contains(low, ".ext")
	} else if m := kickVRE.FindStringSubmatch(name); m != nil {
		b.System, b.Known, b.Label = "Amiga", true, "Kickstart "+m[1]+"."+m[2]
	} else if m := scdRE.FindStringSubmatch(name); m != nil {
		b.System, b.Known = "Mega CD / Sega CD", true
		b.Region = map[string]string{"us": "USA", "eu": "Europe", "jp": "Japan"}[strings.ToLower(m[1])]
		b.Label = fmt.Sprintf("%s %s BIOS (%s)", map[bool]string{true: "Sega CD", false: "Mega CD"}[b.Region == "USA"], map[string]string{"1": "Model 1", "2": "Model 2"}[m[3]], m[4])
	} else if m := cardRE.FindStringSubmatch(name); m != nil {
		b.System, b.Known, b.Label, b.Region = "PC Engine CD / TurboGrafx-CD", true, "System Card "+m[1]+".0", "Japan"
		if m[2] != "" {
			b.Region = "USA"
		}
	} else if m := chipRE.FindStringSubmatch(name); m != nil {
		b.System, b.Known, b.Label = "SNES", true, strings.ToUpper(m[1])+" chip ROM"
	} else if uniRE.MatchString(name) {
		b.System, b.Known, b.Label = "Neo Geo", true, "Unibios"
	} else if m := msxRE.FindStringSubmatch(name); m != nil {
		b.System, b.Known = "MSX", true
		if m[1] != "" {
			b.System = "MSX2"
		}
	}
	if b.Region == "" {
		b.Region = regionIn(name, where)
	}
	if b.System == "" {
		b.System = systemIn(name, where)
	}
	return b
}

// regionWords: region names and marks in file and folder names.
var regionWords = map[string]string{"usa": "USA", "us": "USA", "ntscu": "USA", "america": "USA", "eur": "Europe", "europe": "Europe",
	"eu": "Europe", "pal": "Europe", "jap": "Japan", "japan": "Japan", "jpn": "Japan", "jp": "Japan", "ntscj": "Japan", "world": "World",
	"asia": "Asia", "korea": "Korea", "china": "China", "brazil": "Brazil", "australia": "Australia"}

func regionIn(name, where string) string {
	if m := tagRgRE.FindStringSubmatch(name); m != nil {
		t := strings.ToLower(m[1] + m[2])
		switch t[0] {
		case 'u':
			return "USA"
		case 'e':
			return "Europe"
		case 'j':
			return "Japan"
		case 'w':
			return "World"
		case 'k':
			return "Korea"
		}
	}
	for _, w := range strings.Fields(TitleKey(GameTitle(name))) {
		if r := regionWords[w]; r != "" {
			return r
		}
	}
	folders := strings.Split(where, "/")
	for i := len(folders) - 1; i >= 0; i-- { // the nearest folder first
		if r := regionWords[TitleKey(folders[i])]; r != "" {
			return r
		}
	}
	return ""
}

// emulatorSystem: emulators' folder names (a BIOS folder often has one per emulator).
var emulatorSystem = map[string]string{"pcsx2": "PlayStation 2", "aethersx2": "PlayStation 2", "nethersx2": "PlayStation 2",
	"duckstation": "PlayStation", "swanstation": "PlayStation", "pcsx": "PlayStation", "ppsspp": "PSP", "rpcs3": "PlayStation 3",
	"vita3k": "PS Vita", "dolphin": "GameCube", "cemu": "Wii U", "citra": "Nintendo 3DS", "lime3ds": "Nintendo 3DS", "azahar": "Nintendo 3DS",
	"yuzu": "Switch", "ryujinx": "Switch", "suyu": "Switch", "sudachi": "Switch", "citron": "Switch", "eden": "Switch",
	"xemu": "Xbox", "xenia": "Xbox 360", "flycast": "Dreamcast", "reicast": "Dreamcast", "redream": "Dreamcast", "dc": "Dreamcast",
	"kronos": "Saturn", "yabause": "Saturn", "yabasanshiro": "Saturn", "mupen64plus": "Nintendo 64", "melonds": "Nintendo DS",
	"desmume": "Nintendo DS", "drastic": "Nintendo DS", "mgba": "Game Boy Advance", "neocd": "Neo Geo CD", "fbneo": "Arcade",
	"fba": "Arcade", "mame": "Arcade", "mame2000": "Arcade", "mame2003": "Arcade", "mame2010": "Arcade", "mame2014": "Arcade",
	"mame2016": "Arcade", "raine": "Arcade", "ume": "Arcade", "imame4all": "Arcade", "openmsx": "MSX", "bluemsx": "MSX", "fmsx": "MSX",
	"hatari": "Atari ST", "hatarib": "Atari ST", "vice": "Commodore 64", "np2": "PC-98", "np2kai": "PC-98", "quasi88": "PC-88",
	"keropi": "X68000", "px68k": "X68000", "fmtowns": "FM Towns", "fmtownsux": "FM Towns", "same_cdi": "CD-i", "4do": "3DO", "opera": "3DO",
	"scummvm": "ScummVM", "dosbox": "DOS", "ti99": "TI-99/4A", "fm7": "FM-7", "xmil": "Sharp X1", "eka2l1": "N-Gage"}

// biosSystemWords: one word or two (from SystemName's tables, but not words too loose in
// a BIOS folder like "pc") to the console, for people.
var biosSystemWords = sync.OnceValue(func() map[string]string {
	m := map[string]string{}
	for k := range systemNames {
		m[TitleKey(k)] = SystemName(k)
	}
	for k := range sameSystem {
		m[TitleKey(k)] = SystemName(k)
	}
	for _, loose := range []string{"pc", "ports", "steam", "windows", "lightgun", "moonlight", "flash"} {
		delete(m, loose)
	}
	m["kickstart"], m["saturn"], m["gamecube"], m["wii"], m["cd32"], m["cdtv"] = "Amiga", "Saturn", "GameCube", "Wii", "Amiga CD32", "Amiga"
	return m
})

// systemIn: the console a file is for, from words in its name (two-word ones first: "sega
// cd"), then its folders, nearest first; emulators' folders count too.
func systemIn(name, where string) string {
	sys := biosSystemWords()
	look := func(text string, emus bool) string {
		w := strings.Fields(TitleKey(text))
		for i := 0; i+1 < len(w); i++ {
			if s := sys[w[i]+" "+w[i+1]]; s != "" {
				return s
			}
		}
		for _, x := range w {
			if s := sys[x]; s != "" {
				return s
			}
			if s := emulatorSystem[x]; emus && s != "" {
				return s
			}
		}
		return ""
	}
	if s := look(GameTitle(name), false); s != "" {
		return s
	}
	folders := strings.Split(where, "/")
	for i := len(folders) - 1; i >= 0; i-- {
		if s := emulatorSystem[strings.ToLower(folders[i])]; s != "" {
			if s == "GameCube" && strings.Contains(strings.ToLower(where), "/wii") {
				return "Wii"
			}
			return s
		}
		if s := look(folders[i], true); s != "" {
			return s
		}
	}
	return ""
}

// ---- searching --------------------------------------------------------------------------

// regionAliases: what people type for a region ("ps1 bios us", "… ntsc", "… pal").
var regionAliases = map[string][]string{"USA": {"usa", "us", "america", "ntsc", "ntscu", "na"}, "Europe": {"europe", "eur", "eu", "pal"},
	"Japan": {"japan", "jap", "jpn", "jp", "ntscj"}, "Australia": {"australia", "aus", "pal"}, "UK": {"uk", "pal", "europe", "eur"},
	"Asia": {"asia"}, "China": {"china"}, "Korea": {"korea"}, "Russia": {"russia"}, "World": {"world"}, "Brazil": {"brazil"}}

// extraAliases: what people call a console besides the names in SystemName's tables.
var extraAliases = map[string][]string{"PlayStation": {"psone", "ps"}, "Dreamcast": {"dc"}, "Nintendo DS": {"ds", "dsi"},
	"Mega Drive / Genesis": {"md", "gen"}, "Amiga": {"kickstart"}, "Amiga CD32": {"amiga", "cd32", "kickstart"},
	"Neo Geo CD": {"neocd"}, "PC Engine CD / TurboGrafx-CD": {"pce", "pcecd", "tgcd", "turbografx"},
	"Game Boy": {"gb", "dmg"}, "Game Boy Color": {"gbc", "cgb"}, "GameCube": {"gcn", "ngc"}, "NAOMI 2": {"naomi"},
	"Mega CD / Sega CD": {"scd"}, "Atari 800": {"xl", "xe"}, "Sega ST-V": {"stv"}}

// SystemAliases: the words a console goes by, its name's and every other name's in
// SystemName's tables ("PlayStation": playstation, psx, ps1, sony, …).
func SystemAliases(system string) []string {
	return systemAliasMap()[system]
}

var systemAliasMap = sync.OnceValue(func() map[string][]string {
	sets := map[string]map[string]bool{}
	add := func(sys, text string) {
		if sets[sys] == nil {
			sets[sys] = map[string]bool{}
		}
		for _, w := range strings.Fields(TitleKey(text)) {
			sets[sys][w] = true
		}
	}
	for k := range systemNames {
		add(SystemName(k), k)
	}
	for k := range sameSystem {
		add(SystemName(k), k)
	}
	for sys, ws := range extraAliases {
		for _, w := range ws {
			add(sys, w)
		}
	}
	out := map[string][]string{}
	for sys, set := range sets {
		for w := range set {
			out[sys] = append(out[sys], w)
		}
		sort.Strings(out[sys])
	}
	return out
})

// BiosWords: what a search can match a BIOS file by. strong: its name, what it is, its
// console (and every name that goes by), its region (likewise) and "bios"; weak: the folders
// it is in.
func BiosWords(b Bios) (strong, weak []string) {
	seen := map[string]bool{}
	add := func(ws ...string) {
		for _, w := range ws {
			if w != "" && !seen[w] {
				seen[w] = true
				strong = append(strong, w)
			}
		}
	}
	add(strings.Fields(TitleKey(b.Name))...)
	add(strings.Fields(TitleKey(GameTitle(b.Name)))...)
	add(strings.Fields(TitleKey(b.Label))...)
	if b.System != "" {
		add(strings.Fields(TitleKey(b.System))...)
		add(SystemAliases(b.System)...)
	}
	for _, r := range strings.Split(b.Region, " / ") {
		add(strings.Fields(TitleKey(r))...)
		add(regionAliases[r]...)
	}
	add("bios")
	for _, w := range strings.Fields(TitleKey(b.Where)) {
		if !seen[w] {
			seen[w] = true
			weak = append(weak, w)
		}
	}
	return strong, weak
}
