package share

import (
	"database/sql"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode"
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
	".pcm": true, // MSU-1 soundtracks (the game is its .sfc and .msu)
	".old": true, ".bak": true,
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
// One name per console however a build names its folder: an EmulationStation short name
// ("megadrive", "genesis"), or a LaunchBox platform ("Sega Genesis"). Consoles sold under two
// names are one system ("Mega Drive / Genesis"); a hack set stays apart ("… (Hacks)").
func SystemName(folder string) string {
	base, tag := folder, ""
	if i := strings.Index(folder, " ("); i > 0 && strings.HasSuffix(folder, ")") {
		base, tag = folder[:i], " ("+strings.ToLower(folder[i+2:len(folder)-1])+")"
	}
	k := strings.ToLower(strings.TrimSpace(base))
	if n, ok := sameSystem[k]; ok {
		return n + tag
	}
	if n, ok := systemNames[k]; ok {
		return n + tag
	}
	return folder
}

// sameSystem: names a console goes by in other builds, to one name (lower case keys).
var sameSystem = map[string]string{
	"nes": "NES", "famicom": "NES", "nintendo entertainment system": "NES", "nintendo famicom": "NES",
	"snes": "SNES", "sfc": "SNES", "super nintendo entertainment system": "SNES", "super nintendo": "SNES", "nintendo super famicom": "SNES",
	"genesis": "Mega Drive / Genesis", "megadrive": "Mega Drive / Genesis", "sega genesis": "Mega Drive / Genesis", "sega mega drive": "Mega Drive / Genesis",
	"segacd": "Mega CD / Sega CD", "megacd": "Mega CD / Sega CD", "sega cd": "Mega CD / Sega CD", "sega mega-cd": "Mega CD / Sega CD",
	"sms": "Master System", "mastersystem": "Master System", "sega master system": "Master System",
	"gamegear": "Game Gear", "sega game gear": "Game Gear", "saturn": "Saturn", "sega saturn": "Saturn",
	"dreamcast": "Dreamcast", "sega dreamcast": "Dreamcast", "sega32x": "32X", "sega 32x": "32X", "sg1000": "SG-1000", "sega sg-1000": "SG-1000",
	"pcengine": "PC Engine / TurboGrafx-16", "tg16": "PC Engine / TurboGrafx-16", "nec turbografx-16": "PC Engine / TurboGrafx-16", "nec pc engine": "PC Engine / TurboGrafx-16",
	"pcenginecd": "PC Engine CD / TurboGrafx-CD", "tg-cd": "PC Engine CD / TurboGrafx-CD", "nec turbografx-cd": "PC Engine CD / TurboGrafx-CD",
	"psx": "PlayStation", "ps1": "PlayStation", "sony playstation": "PlayStation", "ps2": "PlayStation 2", "sony playstation 2": "PlayStation 2",
	"ps3": "PlayStation 3", "sony playstation 3": "PlayStation 3", "psp": "PSP", "sony psp": "PSP", "psvita": "PS Vita", "sony playstation vita": "PS Vita",
	"n64": "Nintendo 64", "nintendo 64": "Nintendo 64", "gc": "GameCube", "gamecube": "GameCube", "nintendo gamecube": "GameCube",
	"wii": "Wii", "nintendo wii": "Wii", "wiiu": "Wii U", "nintendo wii u": "Wii U", "switch": "Switch", "nintendo switch": "Switch",
	"nds": "Nintendo DS", "nintendo ds": "Nintendo DS", "3ds": "Nintendo 3DS", "nintendo 3ds": "Nintendo 3DS",
	"gb": "Game Boy", "nintendo game boy": "Game Boy", "gbc": "Game Boy Color", "nintendo game boy color": "Game Boy Color",
	"gba": "Game Boy Advance", "nintendo game boy advance": "Game Boy Advance", "virtualboy": "Virtual Boy", "nintendo virtual boy": "Virtual Boy",
	"fds": "Famicom Disk System", "nintendo famicom disk system": "Famicom Disk System",
	"neogeo": "Neo Geo", "snk neo geo aes": "Neo Geo", "snk neo geo mvs": "Neo Geo", "neogeocd": "Neo Geo CD", "snk neo geo cd": "Neo Geo CD",
	"ngp": "Neo Geo Pocket", "snk neo geo pocket": "Neo Geo Pocket", "ngpc": "Neo Geo Pocket Color", "snk neo geo pocket color": "Neo Geo Pocket Color",
	"msx": "MSX", "msx1": "MSX", "microsoft msx": "MSX", "msx2": "MSX2", "microsoft msx2": "MSX2",
	"xbox": "Xbox", "microsoft xbox": "Xbox", "xbox360": "Xbox 360", "microsoft xbox 360": "Xbox 360",
	"dos": "DOS", "pc": "DOS", "ms-dos": "DOS", "microsoft ms-dos": "DOS", "windows": "Windows", "microsoft windows": "Windows",
	"c64": "Commodore 64", "commodore 64": "Commodore 64", "amiga": "Amiga", "commodore amiga": "Amiga", "amigacd32": "Amiga CD32", "commodore amiga cd32": "Amiga CD32",
	"zxspectrum": "ZX Spectrum", "sinclair zx spectrum": "ZX Spectrum", "amstradcpc": "Amstrad CPC", "amstrad cpc": "Amstrad CPC",
	"apple2": "Apple II", "apple ii": "Apple II", "3do": "3DO", "3do interactive multiplayer": "3DO", "panasonic 3do": "3DO",
	"intellivision": "Intellivision", "mattel intellivision": "Intellivision", "coleco": "ColecoVision", "colecovision": "ColecoVision", "coleco colecovision": "ColecoVision",
	"vectrex": "Vectrex", "gce vectrex": "Vectrex", "wonderswan": "WonderSwan", "wswan": "WonderSwan", "bandai wonderswan": "WonderSwan",
	"wonderswancolor": "WonderSwan Color", "wswanc": "WonderSwan Color", "bandai wonderswan color": "WonderSwan Color",
	"atari2600": "Atari 2600", "atari 2600": "Atari 2600", "atari5200": "Atari 5200", "atari 5200": "Atari 5200", "atari7800": "Atari 7800", "atari 7800": "Atari 7800",
	"atarilynx": "Atari Lynx", "lynx": "Atari Lynx", "atari lynx": "Atari Lynx", "jaguar": "Atari Jaguar", "atari jaguar": "Atari Jaguar",
	"atarist": "Atari ST", "atari st": "Atari ST", "atari800": "Atari 800", "atari 800": "Atari 800",
	"pcfx": "PC-FX", "nec pc-fx": "PC-FX", "pc88": "PC-88", "nec pc-8801": "PC-88", "pc98": "PC-98", "nec pc-9801": "PC-98",
	"x68000": "X68000", "sharp x68000": "X68000", "cdi": "CD-i", "philips cd-i": "CD-i", "arcade": "Arcade",
	"flash": "Flash", "flash games": "Flash", "adobe flash": "Flash", "easyrpg": "EasyRPG",
	"fpinball": "Future Pinball", "future pinball": "Future Pinball", "megadrive-msu": "Mega Drive MSU-1", "md-msu": "Mega Drive MSU-1",
}

// Game is one game of a build: the files of one title in one system folder (its disc
// tracks, discs, regions and revisions together).
type Game struct {
	Title  string `json:"t"` // as people know it: "Super Mario Bros. 3"
	System string `json:"s"` // its system, for people
	Folder string `json:"f"` // its system folder in the share
	Name   string `json:"n"` // its first file (or folder)
	Size   int64  `json:"z"` // all its files
	Count  int64  `json:"c"` // its files (a folder's count)
}

var tagRE = regexp.MustCompile(`\s*[\(\[][^\)\]]*[\)\]]`)

// GameTitle: a ROM file's (or game folder's) name as people know the game: without its file
// type and the tags dump sets add ("(USA)", "(Rev 1)", "(Disc 2)", "(Track 3)", "[!]").
func GameTitle(name string) string {
	if ext := path.Ext(name); ext != "" && (len(ext) <= 6 && !strings.ContainsAny(ext, " ()[]") || len(ext) <= 9 && alnum(ext[1:])) {
		name = strings.TrimSuffix(name, ext) // ".easyrpg" too
	}
	if !strings.Contains(name, " ") && strings.Count(name, ".")+strings.Count(name, "_") >= 2 {
		// a scene-style name ("Super.Mario.3D.World.USA"): its words, without the region
		w := strings.FieldsFunc(name, func(r rune) bool { return r == '.' || r == '_' })
		for len(w) > 1 && sceneRegion[strings.ToLower(w[len(w)-1])] {
			w = w[:len(w)-1]
		}
		name = strings.Join(w, " ")
	}
	t := strings.Join(strings.Fields(tagRE.ReplaceAllString(name, "")), " ")
	if t == "" {
		return name
	}
	return t
}

var sceneRegion = map[string]bool{"usa": true, "eur": true, "europe": true, "jpn": true, "japan": true, "pal": true, "ntsc": true}

func alnum(s string) bool {
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return s != ""
}

// TitleKey: a title folded for comparing and searching: lower case, no accents, "&" as "and",
// only letters and digits between single spaces.
func TitleKey(t string) string {
	t = strings.ToLower(strings.ReplaceAll(t, "&", " and "))
	var b strings.Builder
	space := true
	for _, r := range t {
		if f, ok := unaccent[r]; ok {
			r = f
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
		} else if r != '\'' && r != '’' && !space { // "Mario's" stays one word
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

var unaccent = map[rune]rune{'á': 'a', 'à': 'a', 'â': 'a', 'ä': 'a', 'ã': 'a', 'å': 'a', 'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i', 'ó': 'o', 'ò': 'o', 'ô': 'o', 'ö': 'o', 'õ': 'o', 'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u',
	'ñ': 'n', 'ç': 'c', 'ÿ': 'y'}

// entry: a file or folder in a system folder, with its size.
type entry struct {
	name        string
	size, count int64
}

// gamesIn: the games of one system folder: its files grouped by title (not artwork, game
// lists or notes), and each game folder in it (not artwork folders).
func gamesIn(system, folder string, files, dirs []entry) []Game {
	idx := map[string]int{}
	var out []Game
	add := func(e entry, dir bool) {
		k := gameKey(e.name, dir)
		if k == "" {
			return
		}
		if i, ok := idx[k]; ok {
			out[i].Size += e.size
			out[i].Count += e.count
			return
		}
		idx[k] = len(out)
		out = append(out, Game{Title: GameTitle(e.name), System: system, Folder: folder, Name: e.name, Size: e.size, Count: e.count})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	for _, f := range files {
		add(f, false)
	}
	for _, d := range dirs {
		add(d, true)
	}
	return out
}

// gameKey: which game a file (or folder) of a system folder is part of, as its TitleKey; ""
// when it is none (artwork, a game list, notes).
func gameKey(name string, dir bool) string {
	if dir && mediaDir[strings.ToLower(name)] || !dir && (notGame[strings.ToLower(path.Ext(name))] || strings.EqualFold(name, "gamelist.xml")) {
		return ""
	}
	return TitleKey(GameTitle(name))
}

// IsGame: a file (or folder) of a system folder that's a game, not artwork, a game list or notes.
func IsGame(name string, dir bool) bool { return gameKey(name, dir) != "" }

// MediaFolder: a subfolder of a system folder that holds artwork and the like, not games.
func MediaFolder(name string) bool { return mediaDir[strings.ToLower(name)] }

// GameFile is one file, or game folder, of a game.
type GameFile struct {
	FID    int64  `json:"fid,omitempty"` // a file's, to stream it (Index.File)
	Path   string `json:"path"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Count  int64  `json:"count,omitempty"` // a folder's files
	Folder bool   `json:"folder,omitempty"`
}

// GameFiles: everything of one game in a system folder, as Games counts it: its discs, disc
// tracks, regions and revisions (or its folder). name is any of its files, as Game.Name. None
// when the folder isn't there or has nothing of that game.
func (ix *Index) GameFiles(folder, name string) ([]GameFile, error) {
	key := TitleKey(GameTitle(name))
	d, err := ix.Dir(folder)
	if err != nil || d == nil || key == "" {
		return nil, err
	}
	var out []GameFile
	rows, err := ix.rw.Query("SELECT f.fid, df.name, f.path, f.size FROM dir_files df JOIN files f ON f.fid = df.fid WHERE df.did = ? ORDER BY df.name", d.DID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var g GameFile
		var size sql.NullInt64
		if err := rows.Scan(&g.FID, &g.Name, &g.Path, &size); err != nil {
			rows.Close()
			return nil, err
		}
		if g.Size = size.Int64; gameKey(g.Name, false) == key {
			out = append(out, g)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = ix.rw.Query("SELECT name, path, size, count FROM dirs WHERE parent = ? ORDER BY name", d.DID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		g := GameFile{Folder: true}
		var size, count sql.NullInt64
		if err := rows.Scan(&g.Name, &g.Path, &size, &count); err != nil {
			return nil, err
		}
		if g.Size, g.Count = size.Int64, count.Int64; gameKey(g.Name, true) == key {
			out = append(out, g)
		}
	}
	return out, rows.Err()
}

// Inside: the build's systems, most games first; nil when it has no games folder. Builds
// have other folders of those names (an emulator's, full of title ids or saves; bezel or
// template sets laid out per system): of those whose subfolders are mostly named like
// systems, the build's is the one holding the most files.
func (ix *Index) Inside() ([]System, error) {
	sys, _, err := ix.Games()
	return sys, err
}

// Games: the build's systems (as Inside) and every game in them.
func (ix *Index) Games() ([]System, []Game, error) {
	rows, err := ix.rw.Query("SELECT did, count FROM dirs WHERE lower(name) IN ('roms', 'games') AND did != 0")
	if err != nil {
		return nil, nil, err
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
			return nil, nil, err
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
		// mostly systems (a folder of games whose titles happen to name a console is not); a
		// roms folder holding just one folder, named like a system, is a one-system build's
		if (score >= 2 && score*10 >= subs*3 || score == 1 && subs == 1) && c.files > most {
			root, most = c.did, c.files
		}
	}
	if root < 0 {
		return nil, nil, nil
	}
	type dir struct {
		did        int64
		name, path string
	}
	var systems []dir
	rows, err = ix.rw.Query("SELECT did, name, path FROM dirs WHERE parent = ?", root)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var d dir
		_ = rows.Scan(&d.did, &d.name, &d.path)
		systems = append(systems, d)
	}
	rows.Close()
	entries := func(q string, did int64) ([]entry, error) {
		r, err := ix.rw.Query(q, did)
		if err != nil {
			return nil, err
		}
		defer r.Close()
		var out []entry
		for r.Next() {
			var n sql.NullString
			var z, c sql.NullInt64
			_ = r.Scan(&n, &z, &c)
			out = append(out, entry{n.String, z.Int64, c.Int64})
		}
		return out, r.Err()
	}
	var out []System
	var games []Game
	for _, d := range systems {
		if mediaDir[strings.ToLower(d.name)] || strings.EqualFold(d.name, "bios") {
			continue
		}
		files, err := entries("SELECT df.name, f.size, 1 FROM dir_files df JOIN files f ON f.fid = df.fid WHERE df.did = ?", d.did)
		if err != nil {
			return nil, nil, err
		}
		dirs, err := entries("SELECT name, size, count FROM dirs WHERE parent = ?", d.did)
		if err != nil {
			return nil, nil, err
		}
		name := SystemName(d.name)
		if g := gamesIn(name, d.path, files, dirs); len(g) > 0 {
			out = append(out, System{Name: name, Folder: d.path, Games: int64(len(g))})
			games = append(games, g...)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Games != out[j].Games {
			return out[i].Games > out[j].Games
		}
		return out[i].Name < out[j].Name
	})
	return out, games, nil
}
