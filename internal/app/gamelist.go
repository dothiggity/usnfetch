package app

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Game lists: a game that goes into a library gets its entry in the front-end's list, so it
// shows with its name, description and pictures. EmulationStation (RetroBat, Batocera, ES-DE)
// keeps a gamelist.xml per system, LaunchBox an XML file per platform. A list is changed only
// where usnfetch adds or replaces its games' entries: every other entry stays exactly as it
// was, down to the byte, favourites and play counts included.

// GameEntry: one game's entry, front-end neutral. Tags are as they'll be written: for an
// EmulationStation list its tags (path, name, desc, image…), for LaunchBox the <Game> fields
// (ApplicationPath, Title, Notes…). Key is the field that names the game file (path or
// ApplicationPath), as the list writes it.
type GameEntry struct {
	Key  string      `json:"key"`
	Tags [][2]string `json:"tags"` // in order
}

// GameListEdit: entries to put into one list once the download is done.
type GameListEdit struct {
	Kind  string      `json:"kind"` // "es" (EmulationStation's gamelist.xml) or "launchbox"
	File  string      `json:"file"` // the list
	Games []GameEntry `json:"games"`
	// Needs: the game files (full paths) an entry is for: entries whose file didn't arrive are left out.
	Needs []string `json:"needs"`
}

// listElem: one top-level element of a list: its bytes as they are, and the game file it names.
type listElem struct {
	raw  []byte
	key  string          // normalised; "" for elements that aren't a game (a <folder>…) or name none
	game bool            // a game entry
	has  map[string]bool // the fields it has (lower case)
}

// splitList reads a list's top-level elements under its root (gameList / LaunchBox), keeping
// each as its own bytes. game: the element name of a game entry; key: its field naming the file.
func splitList(data []byte, root, game, key string) (head, tail []byte, elems []listElem, err error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	depth := 0
	var start int64
	rootEnd := int64(-1)
	var cur *listElem
	var inKey bool
	var keyText strings.Builder
	for {
		off := dec.InputOffset()
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			switch {
			case depth == 1:
				if !strings.EqualFold(t.Name.Local, root) {
					return nil, nil, nil, fmt.Errorf("not a %s list (its root is <%s>)", root, t.Name.Local)
				}
				head = data[:dec.InputOffset()]
			case depth == 2:
				start = off
				cur = &listElem{game: strings.EqualFold(t.Name.Local, game), has: map[string]bool{}}
			case depth == 3 && cur != nil:
				cur.has[strings.ToLower(t.Name.Local)] = true
				if cur.game && strings.EqualFold(t.Name.Local, key) { // only a game's names its file: a <folder>'s path isn't one
					inKey = true
					keyText.Reset()
				}
			}
		case xml.CharData:
			if inKey {
				keyText.Write(t)
			}
		case xml.EndElement:
			if depth == 3 && inKey {
				inKey = false
				cur.key = normKey(keyText.String())
			}
			if depth == 2 && cur != nil {
				cur.raw = data[start:dec.InputOffset()]
				elems = append(elems, *cur)
				cur = nil
			}
			if depth == 1 {
				rootEnd = off
			}
			depth--
		}
	}
	if head == nil || rootEnd < 0 {
		return nil, nil, nil, errors.New("the list isn't complete XML")
	}
	return head, data[rootEnd:], elems, nil
}

// normKey: a game file's name as lists write it, to compare: ./ or not, \ or /, any case.
func normKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(s, `\`, "/")))
	return strings.TrimPrefix(s, "./")
}

// writeEntry: one entry as XML, indented as front-ends write them.
func writeEntry(b *bytes.Buffer, elem string, e GameEntry, indent string) {
	b.WriteString(indent + "<" + elem + ">\n")
	for _, t := range e.Tags {
		if t[1] == "" {
			continue
		}
		b.WriteString(indent + "\t<" + t[0] + ">")
		_ = xml.EscapeText(b, []byte(t[1]))
		b.WriteString("</" + t[0] + ">\n")
	}
	b.WriteString(indent + "</" + elem + ">")
}

// ApplyGameList merges an edit's entries into its list. A game the list has already keeps its
// entry as it is (its name, your favourite and play count, whatever you scraped), with only
// the fields it hasn't got added; a new one is added at the end; a list that isn't there yet
// is made. The list before the change is kept beside it (<list>.usnfetch-backup), and the new
// one written whole, then put in its place, so a front-end never reads half a list. present:
// whether a game file arrived (entries for files that didn't are left out).
func ApplyGameList(ed GameListEdit, present func(string) bool) (int, error) {
	root, game, key := "gameList", "game", "path"
	if ed.Kind == "launchbox" {
		root, game, key = "LaunchBox", "Game", "ApplicationPath"
	}
	var want []GameEntry
	for i, e := range ed.Games {
		if i < len(ed.Needs) && present != nil && !present(ed.Needs[i]) {
			continue
		}
		want = append(want, e)
	}
	if len(want) == 0 {
		return 0, nil
	}
	old, err := os.ReadFile(ed.File)
	if err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	var head, tail []byte
	var elems []listElem
	if len(bytes.TrimSpace(old)) > 0 {
		if head, tail, elems, err = splitList(old, root, game, key); err != nil {
			return 0, fmt.Errorf("%s: %w (left as it is)", ed.File, err)
		}
	} else {
		head = []byte(`<?xml version="1.0"?>` + "\n<" + root + ">")
		if ed.Kind == "launchbox" {
			head = []byte(`<?xml version="1.0" standalone="yes"?>` + "\n<" + root + ">")
		}
		tail = []byte("</" + root + ">\n")
	}
	keys := make([]string, 0, len(want))
	byKey := map[string]GameEntry{}
	for _, e := range want {
		k := normKey(e.Key)
		if _, dup := byKey[k]; !dup {
			keys = append(keys, k)
		}
		byKey[k] = e
	}
	done := map[string]bool{}
	n := 0
	var b bytes.Buffer
	b.Write(head)
	for _, el := range elems { // what's there, as it is; a game of ours gets the fields it lacks
		raw := bytes.TrimSpace(el.raw)
		if e, ours := byKey[el.key]; el.game && el.key != "" && ours && !done[el.key] {
			done[el.key] = true
			n++
			raw = addFields(raw, e, el.has)
		}
		b.WriteString("\n\t")
		b.Write(raw)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if done[k] {
			continue
		}
		n++
		b.WriteString("\n")
		writeEntry(&b, game, byKey[k], "\t")
	}
	b.WriteString("\n")
	b.Write(bytes.TrimLeft(tail, " \t\r\n"))
	if err := os.MkdirAll(filepath.Dir(ed.File), 0o755); err != nil {
		return 0, err
	}
	if len(old) > 0 {
		_ = os.WriteFile(ed.File+".usnfetch-backup", old, 0o644)
	}
	tmp := ed.File + ".usnfetch-new"
	if err := os.WriteFile(tmp, b.Bytes(), 0o644); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp, ed.File); err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	return n, nil
}

// addFields: an entry already in the list, with the fields of e it hasn't got put in before
// its closing tag; everything it has stays as it is.
func addFields(raw []byte, e GameEntry, has map[string]bool) []byte {
	var add bytes.Buffer
	for _, t := range e.Tags {
		if t[1] == "" || has[strings.ToLower(t[0])] {
			continue
		}
		add.WriteString("\n\t\t<" + t[0] + ">")
		_ = xml.EscapeText(&add, []byte(t[1]))
		add.WriteString("</" + t[0] + ">")
	}
	end := bytes.LastIndex(raw, []byte("</"))
	if add.Len() == 0 || end < 0 {
		return raw
	}
	j := end
	for j > 0 && (raw[j-1] == ' ' || raw[j-1] == '\t' || raw[j-1] == '\n' || raw[j-1] == '\r') {
		j--
	}
	out := append([]byte{}, raw[:j]...)
	out = append(out, add.Bytes()...)
	out = append(out, "\n\t"...)
	return append(out, raw[end:]...)
}
