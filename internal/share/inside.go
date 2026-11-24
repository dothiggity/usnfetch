package share

import (
	"database/sql"
	"path"
	"sort"
	"strings"
)

// What's inside a game build: its systems and how many games each has, read from the folder
// tree. Builds keep their games under a folder named "roms" (Batocera, RetroBat, RetroPie,
// EmulationStation) or "Games" (LaunchBox), one subfolder per system; a game is a file there
// (or a folder: some systems keep each game in one), not counting artwork, game lists, notes,
// or the extra tracks of a disc game.

// System is one system of a build.
type System struct {
	Name   string `json:"name"`   // for people: "SNES", "PlayStation", "Nintendo 64"
	Folder string `json:"folder"` // its folder in the share
	Games  int64  `json:"games"`
}

// notGame: file types in a system folder that are not games.
var notGame = map[string]bool{
	".xml": true, ".txt": true, ".cfg": true, ".ini": true, ".json": true, ".nfo": true, ".html": true, ".htm": true, // not .md: Mega Drive
	".url": true, ".lnk": true, ".pdf": true, ".db": true, ".log": true, ".srm": true, ".sav": true, ".state": true, ".keep": true,
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".bmp": true, ".webp": true, ".svg": true, ".ico": true,
	".mp4": true, ".mkv": true, ".avi": true, ".webm": true, ".mov": true, ".mp3": true, ".ogg": true, ".wav": true, ".flac": true,
}

// mediaDir: subfolders of a system folder that hold artwork and the like, not games.
var mediaDir = map[string]bool{
	"images": true, "videos": true, "media": true, "manuals": true, "downloaded_images": true, "downloaded_videos": true,
	"downloaded_media": true, "snap": true, "snaps": true, "wheel": true, "marquee": true, "marquees": true, "boxart": true,
	"covers": true, "screenshots": true, "titles": true, "fanart": true, "thumbs": true, "thumbnails": true, "artwork": true,
	"music": true, "bezels": true, "overlays": true, "saves": true, "cheats": true,
}

// systemNames: EmulationStation system folders, for people (LaunchBox's are already words).
var systemNames = map[string]string{
	"3do": "3DO", "3ds": "Nintendo 3DS", "amiga": "Amiga", "amiga500": "Amiga 500", "amiga1200": "Amiga 1200", "amigacd32": "Amiga CD32",
	"amstradcpc": "Amstrad CPC", "apple2": "Apple II", "arcade": "Arcade", "atari2600": "Atari 2600", "atari5200": "Atari 5200",
	"atari7800": "Atari 7800", "atari800": "Atari 800", "atarilynx": "Atari Lynx", "lynx": "Atari Lynx", "atarist": "Atari ST",
	"atomiswave": "Atomiswave", "c64": "Commodore 64", "cavestory": "Cave Story", "coleco": "ColecoVision", "colecovision": "ColecoVision",
	"daphne": "Daphne", "laserdisc": "LaserDisc", "dos": "DOS", "pc": "PC (DOS)", "dreamcast": "Dreamcast", "fba": "FinalBurn Alpha",
	"fbneo": "FinalBurn Neo", "fds": "Famicom Disk System", "gamecube": "GameCube", "gc": "GameCube", "gamegear": "Game Gear",
	"gb": "Game Boy", "gba": "Game Boy Advance", "gbc": "Game Boy Color", "genesis": "Genesis", "megadrive": "Mega Drive",
	"intellivision": "Intellivision", "jaguar": "Atari Jaguar", "mame": "MAME", "mastersystem": "Master System", "sms": "Master System",
	"model2": "Sega Model 2", "model3": "Sega Model 3", "msx": "MSX", "msx2": "MSX2", "n64": "Nintendo 64", "n64dd": "64DD",
	"naomi": "NAOMI", "naomi2": "NAOMI 2", "nds": "Nintendo DS", "neogeo": "Neo Geo", "neogeocd": "Neo Geo CD", "ngp": "Neo Geo Pocket",
	"ngpc": "Neo Geo Pocket Color", "nes": "NES", "famicom": "Famicom", "odyssey2": "Odyssey 2", "openbor": "OpenBOR",
	"pc88": "PC-88", "pc98": "PC-98", "pcengine": "PC Engine", "pcenginecd": "PC Engine CD", "tg16": "TurboGrafx-16",
	"tg-cd": "TurboGrafx-CD", "pcfx": "PC-FX", "pico8": "PICO-8", "ps2": "PlayStation 2", "ps3": "PlayStation 3", "ps4": "PlayStation 4",
	"psp": "PSP", "psvita": "PS Vita", "psx": "PlayStation", "ps1": "PlayStation", "saturn": "Saturn", "scummvm": "ScummVM",
	"sega32x": "32X", "segacd": "Sega CD", "megacd": "Mega CD", "sg1000": "SG-1000", "snes": "SNES", "sfc": "Super Famicom",
	"satellaview": "Satellaview", "sufami": "Sufami Turbo", "switch": "Switch", "teknoparrot": "TeknoParrot", "vectrex": "Vectrex",
	"virtualboy": "Virtual Boy", "wii": "Wii", "wiiu": "Wii U", "wonderswan": "WonderSwan", "wonderswancolor": "WonderSwan Color",
	"wswan": "WonderSwan", "wswanc": "WonderSwan Color", "x68000": "X68000", "xbox": "Xbox", "xbox360": "Xbox 360", "zxspectrum": "ZX Spectrum",
	"zx81": "ZX81", "ports": "Ports", "windows": "Windows", "lightgun": "Light gun", "model1": "Sega Model 1", "triforce": "Triforce",
	"cps1": "CPS-1", "cps2": "CPS-2", "cps3": "CPS-3", "pokemini": "Pokémon Mini", "supervision": "Supervision", "channelf": "Channel F",
	"gw": "Game & Watch", "gameandwatch": "Game & Watch", "moonlight": "Moonlight", "steam": "Steam", "flash": "Flash", "uzebox": "Uzebox",
	"snes-msu": "SNES MSU-1", "snes-msu1": "SNES MSU-1", "psx-msu": "PlayStation MSU", "gb-msu": "Game Boy MSU", "segamsu": "Sega MSU",
	"atomiswave-arcade": "Atomiswave", "hikaru": "Sega Hikaru", "cdi": "CD-i", "cdimono1": "CD-i", "dreamcast-vmu": "Dreamcast VMU",
}

// platformWords: words in LaunchBox platform names ("Nintendo 64", "Sony Playstation 2", "Arcade").
var platformWords = []string{"nintendo", "sony", "playstation", "sega", "atari", "nec", "snk", "neo geo", "microsoft", "xbox",
	"commodore", "amiga", "arcade", "mame", "panasonic", "3do", "bandai", "coleco", "mattel", "philips", "sharp", "amstrad", "apple",
	"msx", "pc engine", "turbografx", "windows", "dos", "magnavox", "sinclair", "spectrum", "game boy", "wonderswan", "famicom",
	"saturn", "dreamcast", "genesis", "mega drive", "master system", "game gear", "naomi", "daphne", "pinball", "gamecube", "wii",
	"nes", "snes", "n64", "psp", "vita", "jaguar", "lynx", "vectrex", "intellivision", "odyssey", "fm towns", "x68000"}

// looksLikeSystem: a folder named like a system (an EmulationStation one, or a LaunchBox platform).
func looksLikeSystem(name string) bool {
	n := strings.ToLower(name)
	if _, ok := systemNames[n]; ok {
		return true
	}
	for _, w := range platformWords {
		if n == w || strings.HasPrefix(n, w+" ") || strings.Contains(n, " "+w) || strings.HasPrefix(n, w+"-") {
			return true
		}
	}
	return false
}

// SystemName: a system folder's name for people.
func SystemName(folder string) string {
	if n, ok := systemNames[strings.ToLower(folder)]; ok {
		return n
	}
	return folder
}

// countGames: the games among a system folder's files and subfolders.
func countGames(files, dirs []string) int64 {
	var m3u, sheets, rest int64
	for _, f := range files {
		ext := strings.ToLower(path.Ext(f))
		switch {
		case notGame[ext] || strings.EqualFold(f, "gamelist.xml"):
		case ext == ".m3u":
			m3u++
		case ext == ".cue" || ext == ".gdi" || ext == ".ccd" || ext == ".toc":
			sheets++
		default:
			rest++
		}
	}
	n := rest
	switch { // a disc game is its playlist or cue sheet, not every track and disc image
	case m3u > 0:
		n = m3u
	case sheets > 0:
		n = sheets
	}
	for _, d := range dirs {
		if !mediaDir[strings.ToLower(d)] {
			n++
		}
	}
	return n
}

// Inside: the build's systems, most games first; nil when it has no games folder. Builds
// have other folders of those names (an emulator's, full of title ids or saves; bezel or
// template sets laid out per system): of those whose subfolders are mostly named like
// systems, the build's is the one holding the most files.
func (ix *Index) Inside() ([]System, error) {
	rows, err := ix.rw.Query("SELECT did, count FROM dirs WHERE lower(name) IN ('roms', 'games') AND did != 0")
	if err != nil {
		return nil, err
	}
	type cand struct{ did, files int64 }
	var cands []cand
	for rows.Next() {
		var c cand
		_ = rows.Scan(&c.did, &c.files)
		cands = append(cands, c)
	}
	rows.Close()
	root, most := int64(-1), int64(-1)
	for _, c := range cands {
		r, err := ix.rw.Query("SELECT name FROM dirs WHERE parent = ?", c.did)
		if err != nil {
			return nil, err
		}
		score, subs := 0, 0
		for r.Next() {
			var n string
			_ = r.Scan(&n)
			subs++
			if looksLikeSystem(n) {
				score++
			}
		}
		r.Close()
		// mostly systems (a folder of games whose titles happen to name a console is not)
		if score >= 2 && score*10 >= subs*3 && c.files > most {
			root, most = c.did, c.files
		}
	}
	if root < 0 {
		return nil, nil
	}
	type dir struct {
		did        int64
		name, path string
	}
	var systems []dir
	rows, err = ix.rw.Query("SELECT did, name, path FROM dirs WHERE parent = ?", root)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var d dir
		_ = rows.Scan(&d.did, &d.name, &d.path)
		systems = append(systems, d)
	}
	rows.Close()
	names := func(q string, did int64) ([]string, error) {
		r, err := ix.rw.Query(q, did)
		if err != nil {
			return nil, err
		}
		defer r.Close()
		var out []string
		for r.Next() {
			var n sql.NullString
			_ = r.Scan(&n)
			out = append(out, n.String)
		}
		return out, r.Err()
	}
	var out []System
	for _, d := range systems {
		if mediaDir[strings.ToLower(d.name)] || strings.EqualFold(d.name, "bios") {
			continue
		}
		files, err := names("SELECT name FROM dir_files WHERE did = ?", d.did)
		if err != nil {
			return nil, err
		}
		dirs, err := names("SELECT name FROM dirs WHERE parent = ?", d.did)
		if err != nil {
			return nil, err
		}
		if n := countGames(files, dirs); n > 0 {
			out = append(out, System{Name: SystemName(d.name), Folder: d.path, Games: n})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Games != out[j].Games {
			return out[i].Games > out[j].Games
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}
