package web

import (
	"context"
	"path"
	"regexp"
	"sort"
	"strings"

	"usnfetch/internal/share"
)

// A game's pictures, video and manual in its build, found by name beside it, so a game that
// goes into a library takes them along (app.Library.MediaDest says where they go):
//   - an EmulationStation build: images/, videos/ and manuals/ beside the games, named like
//     the game with its scraper's suffix ("Super Metroid (USA)-thumb.png"), or Skraper's
//     media/<type>/ ("media/box2dfront/Super Metroid (USA).png");
//   - an ES-DE build: downloaded_media/<system>/<type>/<game>.png;
//   - a LaunchBox build: Images/<Platform>/<Type>/[<Region>/]<Title>-01.png, Videos/<Platform>/,
//     Manuals/<Platform>/.

// role of an EmulationStation scraper's file suffix
var esSuffixRole = map[string]string{"-image": "image", "-thumb": "cover", "-thumbnail": "cover", "-marquee": "marquee", "-wheel": "marquee",
	"-fanart": "fanart", "-boxback": "back", "-box": "box3d", "-boxart": "cover", "-titleshot": "title", "-video": "video", "-manual": "manual",
	"-mix": "mix", "-cartridge": "cart", "-screenshot": "screenshot"}

// role of the folders pictures sit in: Skraper's media/<type>/, ES-DE's, and the plain ones
var folderRole = map[string]string{
	"box2dfront": "cover", "box2dback": "back", "box3d": "box3d", "screenshot": "screenshot", "screenshottitle": "title", "wheel": "marquee",
	"marquee": "marquee", "fanart": "fanart", "videos": "video", "manuals": "manual", "mixrbv1": "mix", "mixrbv2": "mix", "miximage": "mix",
	"support": "cart",
	"covers":  "cover", "backcovers": "back", "3dboxes": "box3d", "screenshots": "screenshot", "titlescreens": "title", "marquees": "marquee",
	"miximages": "mix", "physicalmedia": "cart",
	"images": "image",
}

// LaunchBox's image types
var lbTypeRole = map[string]string{"box - front": "cover", "box - back": "back", "box - 3d": "box3d", "screenshot - gameplay": "screenshot",
	"screenshot - game title": "title", "clear logo": "marquee", "fanart - background": "fanart", "cart - front": "cart"}

var lbNumbered = regexp.MustCompile(`-0*([0-9]{1,3})$`)

// mediaRoles: the roles in the order they're taken, where two would be one file in a library
// (an EmulationStation build's own main picture before a Skraper screenshot).
var mediaRoles = []string{"cover", "image", "screenshot", "mix", "marquee", "title", "fanart", "back", "box3d", "cart", "video", "manual"}

// mediaFile: one of a game's media files, and its role.
type mediaFile struct {
	role string
	file share.TreeFile
}

// mediaFinder looks up games' media in one build, listing each folder once.
type mediaFinder struct {
	ix     *share.Index
	ctx    context.Context
	listed map[string]map[string][]mediaFile // folder -> key (lower case) -> its media
	esde   *string                           // the build's downloaded_media folder ("" none), once looked for
}

func newMediaFinder(ctx context.Context, ix *share.Index) *mediaFinder {
	return &mediaFinder{ix: ix, ctx: ctx, listed: map[string]map[string][]mediaFile{}}
}

// index: the media under folder (and its subfolders), by the game they're for. key turns a
// file into the game's key and its role ("" to skip).
func (f *mediaFinder) index(folder string, key func(t share.TreeFile, rel string) (string, string)) map[string][]mediaFile {
	if m, ok := f.listed[folder]; ok {
		return m
	}
	m := map[string][]mediaFile{}
	f.listed[folder] = m
	if d, _ := f.ix.Dir(folder); d == nil {
		return m
	}
	_ = f.ix.FilesUnder(f.ctx, []string{folder}, func(t share.TreeFile) bool {
		rel := strings.TrimPrefix(t.Folder+"/", folder+"/") // the subfolders below folder, "" or "box2dfront/"
		if k, role := key(t, rel); k != "" && role != "" {
			m[k] = append(m[k], mediaFile{role, t})
		}
		return true
	})
	for _, v := range m {
		sort.Slice(v, func(i, j int) bool { return v[i].file.Name < v[j].file.Name })
	}
	return m
}

func stemOf(name string) string { return strings.TrimSuffix(name, path.Ext(name)) }

// esdeRoot: an ES-DE build's downloaded_media folder, "" if it has none.
func (f *mediaFinder) esdeRoot() string {
	if f.esde != nil {
		return *f.esde
	}
	root := ""
	if res, err := f.ix.Search(f.ctx, "downloaded_media", "", 5, share.SortName); err == nil {
		for _, d := range res.Folders {
			if strings.EqualFold(d.Name, "downloaded_media") {
				root = d.Path
				break
			}
		}
	}
	f.esde = &root
	return root
}

// gameMedia: the build's media for a game: role -> file (the first of each role).
// sysFolder: its system folder; romRel: the game below it; title: its name.
func (f *mediaFinder) gameMedia(sysFolder, romRel, title string) map[string]share.TreeFile {
	out := map[string]share.TreeFile{}
	take := func(ms []mediaFile) {
		for _, x := range ms {
			if _, have := out[x.role]; !have {
				out[x.role] = x.file
			}
		}
	}
	stem := strings.ToLower(stemOf(path.Base(romRel)))
	dir := sysFolder
	if d := path.Dir(romRel); d != "." {
		dir += "/" + d
	}
	// EmulationStation and Skraper: beside the games
	for _, sub := range []string{"images", "videos", "manuals", "media"} {
		take(f.index(dir+"/"+sub, func(t share.TreeFile, rel string) (string, string) {
			s := strings.ToLower(stemOf(t.Name))
			role := folderRole[sub]
			if sub == "media" { // Skraper: media/<type>/<game>.ext
				role = folderRole[strings.ToLower(strings.TrimSuffix(rel, "/"))]
			}
			for suf, r := range esSuffixRole {
				if strings.HasSuffix(s, suf) {
					return strings.TrimSuffix(s, suf), r
				}
			}
			return s, role
		})[stem])
	}
	// ES-DE: downloaded_media/<system>/<type>/<game>.ext
	if root := f.esdeRoot(); root != "" {
		relStem := strings.ToLower(stemOf(romRel))
		take(f.index(root+"/"+path.Base(sysFolder), func(t share.TreeFile, rel string) (string, string) {
			typ, below, _ := strings.Cut(rel, "/")
			return strings.ToLower(strings.TrimSuffix(below, "/") + stemOf(t.Name)), folderRole[strings.ToLower(typ)]
		})[relStem])
	}
	// LaunchBox: <root>/Games/<Platform>/… with Images/<Platform>/<Type>/[<Region>/]<Title>-01.ext
	if up := path.Dir(sysFolder); strings.EqualFold(path.Base(up), "games") && title != "" {
		root, platform := path.Dir(up), path.Base(sysFolder)
		if root == "." {
			root = ""
		}
		at := func(p string) string {
			if root == "" {
				return p
			}
			return root + "/" + p
		}
		key := strings.ToLower(lbFileName(title))
		lbKey := func(t share.TreeFile) string { return strings.ToLower(lbNumbered.ReplaceAllString(stemOf(t.Name), "")) }
		take(f.index(at("Images/"+platform), func(t share.TreeFile, rel string) (string, string) {
			typ, _, _ := strings.Cut(rel, "/")
			return lbKey(t), lbTypeRole[strings.ToLower(typ)]
		})[key])
		take(f.index(at("Videos/"+platform), func(t share.TreeFile, rel string) (string, string) { return lbKey(t), "video" })[key])
		take(f.index(at("Manuals/"+platform), func(t share.TreeFile, rel string) (string, string) { return lbKey(t), "manual" })[key])
	}
	return out
}

// lbFileName: a title as LaunchBox names its media (as app.lbName).
func lbFileName(title string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*'`, r) || r < 0x20 {
			return '_'
		}
		return r
	}, strings.TrimSpace(title))
}

// srcList: a system folder's own game list in the build, and its kind: an EmulationStation
// gamelist.xml beside the games, an ES-DE build's gamelists/<system>/gamelist.xml, or a
// LaunchBox build's Data/Platforms/<Platform>.xml. "" when it has none.
func (f *mediaFinder) srcList(sysFolder string) (string, string) {
	if fe, _ := f.ix.FileByPath(sysFolder + "/gamelist.xml"); fe != nil {
		return fe.Path, "es"
	}
	if root := f.esdeRoot(); root != "" {
		p := path.Dir(root) + "/gamelists/" + path.Base(sysFolder) + "/gamelist.xml"
		if path.Dir(root) == "." {
			p = "gamelists/" + path.Base(sysFolder) + "/gamelist.xml"
		}
		if fe, _ := f.ix.FileByPath(p); fe != nil {
			return fe.Path, "es"
		}
	}
	if up := path.Dir(sysFolder); strings.EqualFold(path.Base(up), "games") {
		p := "Data/Platforms/" + path.Base(sysFolder) + ".xml"
		if r := path.Dir(up); r != "." {
			p = r + "/" + p
		}
		if fe, _ := f.ix.FileByPath(p); fe != nil {
			return fe.Path, "launchbox"
		}
	}
	return "", ""
}
