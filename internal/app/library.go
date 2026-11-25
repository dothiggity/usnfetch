package app

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"usnfetch/internal/share"
)

// Libraries: the game collections of the devices you play on (RetroBat, Batocera, ES-DE or
// EmuDeck, LaunchBox). A download can go into one instead of the download folder: each game
// into the device's own folder for its system, BIOS files into its BIOS folder.

const (
	LibRetroBat  = "retrobat"  // Windows: <RetroBat>\roms\<system>, gamelist.xml in each, <RetroBat>\bios
	LibBatocera  = "batocera"  // the share (\\BATOCERA\share): roms\<system>, gamelist.xml in each, bios\
	LibESDE      = "esde"      // ES-DE / EmuDeck: a ROMs folder of <system> folders; game lists and media in ES-DE's own folder
	LibLaunchBox = "launchbox" // <LaunchBox>\Games\<Platform>, Data\Platforms\<Platform>.xml, Images\<Platform>\…
)

// Library: one of them.
type Library struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	// Path: RetroBat's or Batocera's folder (the one holding roms and bios), ES-DE's ROMs
	// folder, or LaunchBox's folder.
	Path string `json:"path"`
	// Data: ES-DE's media folder (its downloaded_media); "" = none, games go in without pictures.
	Data string `json:"data,omitempty"`
	// Refresh: a web address usnfetch calls after adding games, so the front-end reloads its
	// game lists ("" = none). Batocera's and RetroBat's EmulationStation answer on port 1234.
	Refresh string `json:"refresh"`
}

// ValidLibraryKind: one of the kinds above.
func ValidLibraryKind(k string) bool {
	return k == LibRetroBat || k == LibBatocera || k == LibESDE || k == LibLaunchBox
}

// LibraryKindName: a kind, for people.
func LibraryKindName(k string) string {
	return map[string]string{LibRetroBat: "RetroBat", LibBatocera: "Batocera", LibESDE: "ES-DE / EmuDeck", LibLaunchBox: "LaunchBox"}[k]
}

// RomsDir: the folder holding a folder per system.
func (l Library) RomsDir() string {
	switch l.Kind {
	case LibRetroBat, LibBatocera:
		return filepath.Join(l.Path, "roms")
	case LibLaunchBox:
		return filepath.Join(l.Path, "Games")
	}
	return l.Path
}

// BiosDir: where BIOS files go ("" = this kind has no one place for them).
func (l Library) BiosDir() string {
	switch l.Kind {
	case LibRetroBat, LibBatocera:
		return filepath.Join(l.Path, "bios")
	case LibESDE: // EmuDeck: <Emulation>\roms beside <Emulation>\bios
		if strings.EqualFold(filepath.Base(l.Path), "roms") {
			return filepath.Join(filepath.Dir(l.Path), "bios")
		}
	}
	return ""
}

// LibraryByID: the library with that id.
func (s *Settings) LibraryByID(id string) (Library, bool) {
	for _, l := range s.Libraries {
		if l.ID == id {
			return l, true
		}
	}
	return Library{}, false
}

// Systems: the library's system folders by system (share.SystemName), so a game from any
// build goes into the folder the device already has for its system ("snes", or "Super
// Nintendo Entertainment System" for LaunchBox).
func (l Library) Systems() (map[string][]string, error) {
	out := map[string][]string{}
	add := func(name string) {
		sys := share.SystemName(name)
		for _, have := range out[sys] {
			if strings.EqualFold(have, name) {
				return
			}
		}
		out[sys] = append(out[sys], name)
	}
	ents, err := os.ReadDir(l.RomsDir())
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	err = nil // no system folders yet: games bring their own
	for _, e := range ents {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			add(e.Name())
		}
	}
	if l.Kind == LibLaunchBox { // platforms with no games folder yet
		if xs, _ := filepath.Glob(filepath.Join(l.Path, "Data", "Platforms", "*.xml")); xs != nil {
			for _, x := range xs {
				add(strings.TrimSuffix(filepath.Base(x), filepath.Ext(x)))
			}
		}
	}
	for _, v := range out {
		sort.Slice(v, func(i, j int) bool { return strings.ToLower(v[i]) < strings.ToLower(v[j]) })
	}
	return out, err
}

// SystemFolder: the library's folder for a system (share.SystemName), given what it has
// (Systems): the one named like the build's own folder src if there is one, else the usual
// name, else the first; a system it doesn't have yet gets the usual name (or src).
func (l Library) SystemFolder(system, src string, have map[string][]string) string {
	def := l.defaultFolder(system)
	if c := have[system]; len(c) > 0 {
		for _, f := range c {
			if strings.EqualFold(f, src) {
				return f
			}
		}
		for _, f := range c {
			if strings.EqualFold(f, def) {
				return f
			}
		}
		return c[0]
	}
	if def != "" {
		return def
	}
	return src
}

func (l Library) defaultFolder(system string) string {
	switch l.Kind {
	case LibLaunchBox:
		return lbPlatform[system]
	case LibESDE:
		if f, ok := esdeFolder[system]; ok {
			return f
		}
	}
	return esFolder[system]
}

// esFolder: Batocera's and RetroBat's folder for each system (share.SystemName's names).
var esFolder = map[string]string{
	"NES": "nes", "SNES": "snes", "Nintendo 64": "n64", "GameCube": "gamecube", "Wii": "wii", "Wii U": "wiiu", "Switch": "switch",
	"Nintendo DS": "nds", "Nintendo 3DS": "3ds", "Game Boy": "gb", "Game Boy Color": "gbc", "Game Boy Advance": "gba",
	"Virtual Boy": "virtualboy", "Famicom Disk System": "fds", "Mega Drive / Genesis": "megadrive", "Mega CD / Sega CD": "segacd",
	"Master System": "mastersystem", "Game Gear": "gamegear", "Saturn": "saturn", "Dreamcast": "dreamcast", "32X": "sega32x",
	"SG-1000": "sg1000", "PC Engine / TurboGrafx-16": "pcengine", "PC Engine CD / TurboGrafx-CD": "pcenginecd", "PlayStation": "psx",
	"PlayStation 2": "ps2", "PlayStation 3": "ps3", "PSP": "psp", "PS Vita": "psvita", "Neo Geo": "neogeo", "Neo Geo CD": "neogeocd",
	"Neo Geo Pocket": "ngp", "Neo Geo Pocket Color": "ngpc", "MSX": "msx1", "MSX2": "msx2", "Xbox": "xbox", "Xbox 360": "xbox360",
	"DOS": "dos", "Windows": "windows", "Commodore 64": "c64", "Amiga": "amiga500", "Amiga CD32": "amigacd32", "ZX Spectrum": "zxspectrum",
	"Amstrad CPC": "amstradcpc", "Apple II": "apple2", "3DO": "3do", "Intellivision": "intellivision", "ColecoVision": "colecovision",
	"Vectrex": "vectrex", "WonderSwan": "wswan", "WonderSwan Color": "wswanc", "Atari 2600": "atari2600", "Atari 5200": "atari5200",
	"Atari 7800": "atari7800", "Atari Lynx": "lynx", "Atari Jaguar": "jaguar", "Atari ST": "atarist", "Atari 800": "atari800",
	"PC-FX": "pcfx", "X68000": "x68000", "CD-i": "cdi", "Arcade": "mame",
}

// esdeFolder: where ES-DE's (and EmuDeck's) folder differs from esFolder.
var esdeFolder = map[string]string{
	"GameCube": "gc", "Nintendo 3DS": "n3ds", "MSX": "msx", "Amiga": "amiga", "WonderSwan": "wonderswan",
	"WonderSwan Color": "wonderswancolor", "Atari Lynx": "atarilynx", "Arcade": "arcade",
}

// lbPlatform: LaunchBox's platform names.
var lbPlatform = map[string]string{
	"NES": "Nintendo Entertainment System", "SNES": "Super Nintendo Entertainment System", "Nintendo 64": "Nintendo 64",
	"GameCube": "Nintendo GameCube", "Wii": "Nintendo Wii", "Wii U": "Nintendo Wii U", "Switch": "Nintendo Switch", "Nintendo DS": "Nintendo DS",
	"Nintendo 3DS": "Nintendo 3DS", "Game Boy": "Nintendo Game Boy", "Game Boy Color": "Nintendo Game Boy Color",
	"Game Boy Advance": "Nintendo Game Boy Advance", "Virtual Boy": "Nintendo Virtual Boy", "Famicom Disk System": "Nintendo Famicom Disk System",
	"Mega Drive / Genesis": "Sega Genesis", "Mega CD / Sega CD": "Sega CD", "Master System": "Sega Master System", "Game Gear": "Sega Game Gear",
	"Saturn": "Sega Saturn", "Dreamcast": "Sega Dreamcast", "32X": "Sega 32X", "SG-1000": "Sega SG-1000",
	"PC Engine / TurboGrafx-16": "NEC TurboGrafx-16", "PC Engine CD / TurboGrafx-CD": "NEC TurboGrafx-CD", "PlayStation": "Sony Playstation",
	"PlayStation 2": "Sony Playstation 2", "PlayStation 3": "Sony Playstation 3", "PSP": "Sony PSP", "PS Vita": "Sony Playstation Vita",
	"Neo Geo": "SNK Neo Geo AES", "Neo Geo CD": "SNK Neo Geo CD", "Neo Geo Pocket": "SNK Neo Geo Pocket", "Neo Geo Pocket Color": "SNK Neo Geo Pocket Color",
	"MSX": "Microsoft MSX", "MSX2": "Microsoft MSX2", "Xbox": "Microsoft Xbox", "Xbox 360": "Microsoft Xbox 360", "DOS": "MS-DOS",
	"Windows": "Windows", "Commodore 64": "Commodore 64", "Amiga": "Commodore Amiga", "Amiga CD32": "Commodore Amiga CD32",
	"ZX Spectrum": "Sinclair ZX Spectrum", "Amstrad CPC": "Amstrad CPC", "Apple II": "Apple II", "3DO": "3DO Interactive Multiplayer",
	"Intellivision": "Mattel Intellivision", "ColecoVision": "Coleco ColecoVision", "Vectrex": "GCE Vectrex", "WonderSwan": "Bandai WonderSwan",
	"WonderSwan Color": "Bandai WonderSwan Color", "Atari 2600": "Atari 2600", "Atari 5200": "Atari 5200", "Atari 7800": "Atari 7800",
	"Atari Lynx": "Atari Lynx", "Atari Jaguar": "Atari Jaguar", "Atari ST": "Atari ST", "Atari 800": "Atari 800", "PC-FX": "NEC PC-FX",
	"X68000": "Sharp X68000", "CD-i": "Philips CD-i", "Arcade": "Arcade",
}

// DefaultRefresh: the address that makes the front-end reload its game lists, as far as
// usnfetch can tell from the library's folder: Batocera's EmulationStation on its own machine
// (the host of \\HOST\share), RetroBat's on this computer. "" for the others.
func DefaultRefresh(kind, path string) string {
	switch kind {
	case LibRetroBat:
		return "http://127.0.0.1:1234/reloadgames"
	case LibBatocera:
		host := "batocera"
		if strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, "//") { // \\HOST\share: Batocera's own machine
			if h, _, _ := strings.Cut(strings.ReplaceAll(path[2:], `\`, "/"), "/"); h != "" {
				host = h
			}
		}
		return "http://" + host + ":1234/reloadgames"
	}
	return ""
}

// CheckLibrary: whether a library's folders are there and look like its kind: its system
// folders and how many, the BIOS folder, and the problems ("" ok).
func CheckLibrary(l Library) map[string]any {
	out := map[string]any{"ok": false, "roms": l.RomsDir(), "bios": l.BiosDir()}
	if !ValidLibraryKind(l.Kind) {
		out["error"] = "pick what kind of library it is"
		return out
	}
	if strings.TrimSpace(l.Path) == "" || !filepath.IsAbs(l.Path) {
		out["error"] = `give the full path of its folder, such as D:\RetroBat`
		return out
	}
	if st, err := os.Stat(l.Path); err != nil || !st.IsDir() {
		out["error"] = "that folder isn't there (is the device on and its share reachable?)"
		return out
	}
	sys, err := l.Systems()
	if err != nil {
		out["error"] = "can't read " + l.RomsDir() + ": " + err.Error()
		return out
	}
	n := 0
	for _, v := range sys {
		n += len(v)
	}
	out["systems"] = n
	if n == 0 {
		what := map[string]string{LibRetroBat: "a roms folder with a folder per system", LibBatocera: "a roms folder with a folder per system",
			LibESDE: "a folder per system", LibLaunchBox: "a Games folder or Data\\Platforms"}[l.Kind]
		out["warn"] = fmt.Sprintf("no system folders found in %s: a %s library has %s. Games for a new system get a folder of their own.", l.RomsDir(), LibraryKindName(l.Kind), what)
	}
	if l.Kind == LibESDE {
		if l.Data == "" {
			out["media_guess"] = MediaGuess(l.Path)
		} else if st, err := os.Stat(l.Data); err != nil || !st.IsDir() {
			out["error"] = "ES-DE's media folder isn't there: " + l.Data
			return out
		}
	}
	if l.Refresh != "" {
		if u, err := url.Parse(l.Refresh); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			out["error"] = "the refresh address has to be a web address (http://…)"
			return out
		}
	}
	out["ok"] = true
	return out
}

// refreshLibrary: after games went into a library, ask its front-end to reload its game
// lists (Library.Refresh). In the background, and only logged when it can't be reached: the
// front-end may simply not be running.
func (m *Manager) refreshLibrary(lib Library) {
	if lib.Refresh == "" {
		return
	}
	go func() {
		c := &http.Client{Timeout: 8 * time.Second}
		resp, err := c.Get(lib.Refresh)
		if err != nil {
			m.Log(fmt.Sprintf("%s: couldn't ask it to reload its game lists (%s): %v", lib.Name, lib.Refresh, err))
			return
		}
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			m.Log(fmt.Sprintf("%s: asked it to reload its game lists, it said %s", lib.Name, resp.Status))
			return
		}
		m.Log(fmt.Sprintf("%s: asked it to reload its game lists", lib.Name))
	}()
}

// ---- a game's pictures, video and manual in a library -------------------------------------
//
// Roles, front-end neutral: cover (box front), back, box3d, screenshot, title (title screen),
// marquee (logo, wheel), fanart, video, manual, mix (a mix image), cart, image (an
// EmulationStation build's main picture, whatever its scraper put there).

// esMedia: Batocera's and RetroBat's way (their scraper's file names): the gamelist tag, the
// file's suffix and its folder beside the games.
var esMedia = map[string][3]string{
	"image": {"image", "-image", "images"}, "screenshot": {"image", "-image", "images"}, "cover": {"thumbnail", "-thumb", "images"},
	"marquee": {"marquee", "-marquee", "images"}, "fanart": {"fanart", "-fanart", "images"}, "back": {"boxback", "-boxback", "images"},
	"box3d": {"boxart", "-box", "images"}, "title": {"titleshot", "-titleshot", "images"}, "mix": {"mix", "-mix", "images"},
	"cart": {"cartridge", "-cartridge", "images"}, "video": {"video", "-video", "videos"}, "manual": {"manual", "-manual", "manuals"},
}

// esdeMedia: ES-DE's folder per role, in downloaded_media/<system>/.
var esdeMedia = map[string]string{
	"cover": "covers", "back": "backcovers", "box3d": "3dboxes", "screenshot": "screenshots", "image": "screenshots", "title": "titlescreens",
	"marquee": "marquees", "fanart": "fanart", "mix": "miximages", "cart": "physicalmedia", "video": "videos", "manual": "manuals",
}

// lbMedia: LaunchBox's image type per role (videos and manuals have folders of their own).
var lbMedia = map[string]string{
	"cover": "Box - Front", "back": "Box - Back", "box3d": "Box - 3D", "screenshot": "Screenshot - Gameplay", "image": "Screenshot - Gameplay",
	"title": "Screenshot - Game Title", "marquee": "Clear Logo", "fanart": "Fanart - Background", "cart": "Cart - Front",
}

// lbName: a title as LaunchBox names its media files: characters file names can't have, and
// the apostrophe, become _ ("Disney's 102 Dalmatians: Puppies…" -> "Disney_s 102 Dalmatians_ Puppies…").
func lbName(title string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*'`, r) || r < 0x20 {
			return '_'
		}
		return r
	}, strings.TrimSpace(title))
}

// MediaDest: where a game's picture, video or manual of one role goes in the library, and the
// gamelist tag that names it (EmulationStation libraries; "" otherwise). sysDir: the library's
// folder for the game's system; romRel: the game file below it ("Super Metroid (USA).sfc");
// title: the game's name; ext: the media file's extension. "" when the library has no place
// for that role.
func (l Library) MediaDest(sysDir, romRel, title, role, ext string) (dest, tag string) {
	ext = strings.ToLower(ext)
	stem := strings.TrimSuffix(romRel, filepath.Ext(romRel))
	switch l.Kind {
	case LibRetroBat, LibBatocera:
		m, ok := esMedia[role]
		if !ok {
			return "", ""
		}
		base := filepath.Base(stem)
		return filepath.Join(sysDir, filepath.Dir(romRel), m[2], base+m[1]+ext), m[0]
	case LibESDE:
		folder, ok := esdeMedia[role]
		if !ok || l.Data == "" {
			return "", ""
		}
		return filepath.Join(l.Data, filepath.Base(sysDir), folder, filepath.FromSlash(stem)+ext), ""
	case LibLaunchBox:
		platform := filepath.Base(sysDir)
		name := lbName(title) + "-01" + ext
		switch role {
		case "video":
			return filepath.Join(l.Path, "Videos", platform, name), ""
		case "manual":
			return filepath.Join(l.Path, "Manuals", platform, name), ""
		}
		if t, ok := lbMedia[role]; ok {
			return filepath.Join(l.Path, "Images", platform, t, name), ""
		}
	}
	return "", ""
}

// GameListFor: the EmulationStation game list games of a system folder go into: the one
// beside the games when it's there; else the one EmulationStation keeps in its own settings
// folder when that's there (it reads that one when the system folder has none); else a new
// one beside the games. "" for libraries whose list usnfetch doesn't edit: ES-DE and LaunchBox
// keep theirs open while they run, so their games are added by their own import or rescan.
func (l Library) GameListFor(sysDir string) string {
	if l.Kind != LibRetroBat && l.Kind != LibBatocera {
		return ""
	}
	beside := filepath.Join(sysDir, "gamelist.xml")
	if _, err := os.Stat(beside); err == nil {
		return beside
	}
	own := filepath.Join(l.Path, "emulationstation", ".emulationstation", "gamelists", filepath.Base(sysDir), "gamelist.xml") // RetroBat
	if l.Kind == LibBatocera {
		own = filepath.Join(l.Path, "system", "configs", "emulationstation", "gamelists", filepath.Base(sysDir), "gamelist.xml")
	}
	if _, err := os.Stat(own); err == nil {
		return own
	}
	return beside
}

// MediaGuess: where ES-DE (or EmuDeck) keeps its downloaded_media, near the ROMs folder, if
// it's there ("" if none found): EmuDeck's Emulation\tools or Emulation\storage, a portable
// ES-DE beside its ROMs, or ES-DE in the home folder.
func MediaGuess(roms string) string {
	up := filepath.Dir(roms)
	home, _ := os.UserHomeDir()
	for _, c := range []string{filepath.Join(up, "tools", "downloaded_media"), filepath.Join(up, "storage", "downloaded_media"),
		filepath.Join(up, "ES-DE", "downloaded_media"), filepath.Join(home, "ES-DE", "downloaded_media")} {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c
		}
	}
	return ""
}
