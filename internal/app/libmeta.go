package app

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// A library job's game list entries: worked out once its files are in (the build's own list
// comes along with the job, into the data folder), then merged into the device's list
// (gamelist.go). For RetroBat and Batocera only: see Library.GameListFor.

// LibMeta: one system's games of a library job, for the device's game list.
type LibMeta struct {
	Kind  string    `json:"kind"`  // the build's own list: "es" (a gamelist.xml), "launchbox" (a platform XML), "" none
	Src   string    `json:"src"`   // where the job saves the build's list ("" = none)
	List  string    `json:"list"`  // the device's game list
	Games []LibGame `json:"games"` // its games
}

// LibGame: one game of it.
type LibGame struct {
	Key   string            `json:"key"`   // its file as the build's list names it (normKey of the path below the system folder)
	Title string            `json:"title"` // a name, when the build's list has none for it
	Rom   string            `json:"rom"`   // the game file in the library
	Path  string            `json:"path"`  // as the device's list names it: "./Super Metroid (USA).sfc"
	Media map[string]string `json:"media"` // gamelist tag -> its file in the library, written once it's there
	Rel   map[string]string `json:"rel"`   // gamelist tag -> that file as the list names it: "./images/…"
}

// personal: what a list says about how you play a game, which isn't the build's to bring.
var personal = map[string]bool{"favorite": true, "favourite": true, "hidden": true, "playcount": true, "lastplayed": true,
	"playtime": true, "kidgame": true, "path": true}

// esMediaTags: gamelist tags that name a picture, video or manual (the job's own replace them).
var esMediaTags = map[string]bool{"image": true, "thumbnail": true, "marquee": true, "video": true, "fanart": true, "titleshot": true,
	"manual": true, "magazine": true, "map": true, "bezel": true, "cartridge": true, "boxart": true, "boxback": true, "wheel": true, "mix": true}

// srcEntries: the build's list, as its entries' fields by game file (normKey of the path, and
// of the bare file name as a fallback).
func srcEntries(kind string, data []byte) map[string][][2]string {
	out := map[string][][2]string{}
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	game, key := "game", "path"
	if kind == "launchbox" {
		game, key = "Game", "ApplicationPath"
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != game {
			continue
		}
		var e struct {
			Fields []struct {
				XMLName xml.Name
				Text    string `xml:",chardata"`
			} `xml:",any"`
		}
		if dec.DecodeElement(&e, &se) != nil {
			continue
		}
		var fields [][2]string
		k := ""
		for _, f := range e.Fields {
			if f.XMLName.Local == key {
				k = f.Text
			}
			fields = append(fields, [2]string{f.XMLName.Local, strings.TrimSpace(f.Text)})
		}
		if k == "" {
			continue
		}
		if kind == "launchbox" { // Games\<Platform>\x.sfc: the part below the platform's folder
			kk := normKey(k)
			if i := strings.Index(kk, "games/"); i >= 0 {
				kk = kk[i+6:]
				if j := strings.Index(kk, "/"); j >= 0 {
					kk = kk[j+1:]
				}
			}
			out[kk] = fields
		} else {
			out[normKey(k)] = fields
		}
		out["\x00"+path.Base(normKey(k))] = fields
	}
	return out
}

// esFields: the build's fields for an EmulationStation list: as they are from one (but the
// personal ones and its picture tags), converted from LaunchBox's.
func esFields(kind string, fields [][2]string) [][2]string {
	var out [][2]string
	if kind != "launchbox" {
		for _, f := range fields {
			if !personal[f[0]] && !esMediaTags[f[0]] && f[1] != "" {
				out = append(out, f)
			}
		}
		return out
	}
	get := map[string]string{}
	for _, f := range fields {
		get[f[0]] = f[1]
	}
	add := func(tag, v string) {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, [2]string{tag, v})
		}
	}
	add("name", get["Title"])
	add("desc", get["Notes"])
	if r, err := strconv.ParseFloat(get["CommunityStarRating"], 64); err == nil && r > 0 { // 0-5 stars to 0-1
		add("rating", strconv.FormatFloat(min(1, r/5), 'f', 2, 64))
	}
	if d := get["ReleaseDate"]; len(d) >= 10 { // 1994-04-18T00:00:00-07:00 -> 19940418T000000
		add("releasedate", strings.ReplaceAll(d[:10], "-", "")+"T000000")
	}
	add("developer", get["Developer"])
	add("publisher", get["Publisher"])
	add("genre", get["Genre"])
	add("players", get["MaxPlayers"])
	return out
}

// listMu: one game list written at a time (two jobs into one system folder finishing together).
var listMu sync.Mutex

// applyLibMeta: the job's games into the device's game lists, for those whose file arrived.
// final: the job is done, so the build's list it brought along goes (a job that failed keeps
// it, for when it's resumed).
func (m *Manager) applyLibMeta(j *Job, final bool) {
	listMu.Lock()
	defer listMu.Unlock()
	for _, lm := range j.Meta {
		if lm.List == "" {
			continue
		}
		var src map[string][][2]string
		if lm.Src != "" {
			if data, err := os.ReadFile(lm.Src); err == nil {
				src = srcEntries(lm.Kind, data)
			}
		}
		ed := GameListEdit{Kind: "es", File: lm.List}
		for _, g := range lm.Games {
			if _, err := os.Stat(g.Rom); err != nil {
				continue
			}
			fields, known := src[g.Key]
			if !known {
				fields, known = src["\x00"+path.Base(g.Key)]
			}
			tags := [][2]string{{"path", g.Path}}
			fs := esFields(lm.Kind, fields)
			if !hasTag(fs, "name") {
				tags = append(tags, [2]string{"name", g.Title})
			}
			tags = append(tags, fs...)
			art := 0
			for _, t := range []string{"image", "thumbnail", "marquee", "video", "fanart", "titleshot", "boxart", "boxback", "manual", "mix", "cartridge"} {
				if f := g.Media[t]; f != "" {
					if _, err := os.Stat(f); err == nil {
						tags = append(tags, [2]string{t, g.Rel[t]})
						art++
					}
				}
			}
			if !known && art == 0 {
				continue // nothing to say the list doesn't: EmulationStation shows the file as it is
			}
			ed.Games = append(ed.Games, GameEntry{Key: g.Path, Tags: tags})
		}
		n, err := ApplyGameList(ed, nil)
		switch {
		case err != nil:
			m.Log(fmt.Sprintf("%s: game list %s not updated: %v", j.Label(), lm.List, err))
		case n > 0:
			m.Log(fmt.Sprintf("%s: %d game(s) added to %s", j.Label(), n, lm.List))
		}
		if lm.Src != "" && final {
			_ = os.Remove(lm.Src)
			_ = os.Remove(filepath.Dir(lm.Src)) // once it's empty
		}
	}
}

func hasTag(tags [][2]string, t string) bool {
	for _, x := range tags {
		if x[0] == t {
			return true
		}
	}
	return false
}
