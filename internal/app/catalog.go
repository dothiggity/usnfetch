package app

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"usnfetch/internal/catalog"
)

func filepathDir(p string) string { return filepath.Dir(p) }

// LinkID: the stable id of a link in the API (also the share-session id).
func LinkID(link string) string {
	h := sha1.Sum([]byte(strings.TrimSpace(link)))
	return hex.EncodeToString(h[:])[:16]
}

// Catalog: imported link-list entries, kept in the app database.
type Catalog struct {
	mu      sync.Mutex
	store   *Store
	Entries []catalog.Entry
	undo    []catalog.Entry // the entries before the last import that changed them; nil = nothing to undo
}

func LoadCatalog(s *Store) (*Catalog, error) {
	e, err := s.LoadCatalog()
	if err != nil {
		return nil, err
	}
	return &Catalog{store: s, Entries: e}, nil
}

// MergeOptions: how an import treats links the catalog already has.
type MergeOptions struct {
	Overwrite bool // replace details that differ (title, size, …); off = only fill in blank ones
	DryRun    bool // report what would happen, change nothing
}

// FieldChange is one detail of a known entry that an import replaces (or would).
type FieldChange struct {
	Field string `json:"field"`
	From  string `json:"from"`
	To    string `json:"to"`
}

// EntryChange: the details of one known entry that the import has differently.
type EntryChange struct {
	ID     string        `json:"id"`
	Title  string        `json:"title"`
	Fields []FieldChange `json:"fields"`
}

// MergeResult says what an import did (or, dry run, would do).
type MergeResult struct {
	Added   int           `json:"added"`
	Filled  int           `json:"filled"`  // known entries whose blank details were filled in
	Changed int           `json:"changed"` // known entries whose differing details were replaced (Overwrite)
	Kept    int           `json:"kept"`    // known entries whose differing details were left as they were
	Changes []EntryChange `json:"changes"` // every known entry the import has other details for
}

func copyEntries(es []catalog.Entry) []catalog.Entry {
	out := make([]catalog.Entry, len(es))
	for i, e := range es {
		e.Also = append([]string{}, e.Also...)
		out[i] = e
	}
	return out
}

// Merge adds new links. For links already there, a blank detail is never copied over a
// filled one (a pasted bare link must not wipe a title, a SKU or a source), blank details
// get filled in, and details that differ are replaced only with Overwrite.
func (c *Catalog) Merge(newEntries []catalog.Entry, o MergeOptions) (MergeResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	work := c.Entries
	if o.DryRun {
		work = copyEntries(c.Entries)
	}
	before := copyEntries(c.Entries)
	idx := map[string]int{}
	for i, e := range work {
		idx[e.Link] = i
	}
	res := MergeResult{Changes: []EntryChange{}}
	touched := false
	t := now()
	for _, n := range newEntries {
		i, ok := idx[n.Link]
		if !ok {
			n.Added = t
			if n.Also == nil {
				n.Also = []string{}
			}
			if n.Title == "" {
				n.Title = catalog.FallbackTitle(n.Link)
			}
			work = append(work, n)
			idx[n.Link] = len(work) - 1
			res.Added++
			touched = true
			continue
		}
		e := work[i]
		newTitle := n.Title
		if newTitle == catalog.FallbackTitle(n.Link) { // no title in the text: nothing to say about it
			newTitle = ""
		}
		oldTitle := e.Title
		if oldTitle == catalog.FallbackTitle(e.Link) {
			oldTitle = ""
		}
		type field struct {
			name, from, to string
			set            func()
		}
		fields := []field{
			{"title", oldTitle, newTitle, func() { e.Title = n.Title }},
			{"label", e.Variant, n.Variant, func() { e.Variant = n.Variant }},
			{"listed size", e.Listed, n.Listed, func() { e.Listed, e.Size = n.Listed, n.Size }},
			{"source", e.Source, n.Source, func() { e.Source = n.Source }},
			{"SKU", e.SKU, n.SKU, func() { e.SKU = n.SKU }},
			{"info link", e.Info, n.Info, func() { e.Info = n.Info }},
		}
		filled := false
		var diff []FieldChange
		for _, f := range fields {
			switch {
			case f.to == "" || f.to == f.from:
			case f.from == "":
				f.set()
				filled = true
			default:
				diff = append(diff, FieldChange{f.name, f.from, f.to})
				if o.Overwrite {
					f.set()
				}
			}
		}
		seen := map[string]bool{}
		also := []string{}
		for _, a := range append(append([]string{}, e.Also...), n.Also...) {
			if !seen[a] {
				seen[a] = true
				also = append(also, a)
			}
		}
		if !reflect.DeepEqual(also, e.Also) {
			e.Also = also
			touched = true
		}
		if filled {
			res.Filled++
			touched = true
		}
		if len(diff) > 0 {
			res.Changes = append(res.Changes, EntryChange{ID: LinkID(e.Link), Title: e.Label(), Fields: diff})
			if o.Overwrite {
				res.Changed++
				touched = true
			} else {
				res.Kept++
			}
		}
		work[i] = e
	}
	if o.DryRun || !touched {
		return res, nil
	}
	c.Entries, c.undo = work, before
	return res, c.store.SaveCatalog(c.Entries)
}

// Add puts one link in the catalog (from a share opened by its link); a link that is
// there already is left alone. Reports whether it was added.
func (c *Catalog) Add(e catalog.Entry) (bool, error) {
	if _, ok := c.ByLink(e.Link); ok {
		return false, nil
	}
	r, err := c.Merge([]catalog.Entry{e}, MergeOptions{})
	return r.Added > 0, err
}

// Undo puts the catalog back as it was before the last import that changed it.
func (c *Catalog) Undo() (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.undo == nil {
		return false, nil
	}
	c.Entries, c.undo = c.undo, nil
	return true, c.store.SaveCatalog(c.Entries)
}

func (c *Catalog) Remove(links map[string]bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var keep []catalog.Entry
	for _, e := range c.Entries {
		if !links[e.Link] {
			keep = append(keep, e)
		}
	}
	if keep == nil {
		keep = []catalog.Entry{}
	}
	c.Entries, c.undo = keep, nil
	return c.store.SaveCatalog(c.Entries)
}

func (c *Catalog) ByID(id string) (catalog.Entry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.Entries {
		if LinkID(e.Link) == id {
			return e, true
		}
	}
	return catalog.Entry{}, false
}

func (c *Catalog) ByLink(link string) (catalog.Entry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.Entries {
		if e.Link == link {
			return e, true
		}
	}
	return catalog.Entry{}, false
}

func (c *Catalog) Snapshot() []catalog.Entry {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]catalog.Entry{}, c.Entries...)
}

// ---- SABnzbd servers -> accounts ----

// SabIniDefault: SABnzbd's settings file, if found in the usual places.
func SabIniDefault() string {
	home, _ := os.UserHomeDir()
	for _, p := range []string{filepath.Join(os.Getenv("LOCALAPPDATA"), "sabnzbd", "sabnzbd.ini"), filepath.Join(home, ".sabnzbd", "sabnzbd.ini")} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

var (
	sectionRe = regexp.MustCompile(`^\[[^\[\]]+\]$`)
	serverRe  = regexp.MustCompile(`^\[\[.+\]\]$`)
)

// ImportSabServers reads the news servers in a sabnzbd.ini as accounts. SABnzbd tries a
// server only for articles its lower-numbered-priority servers lack, so every enabled
// server above the lowest priority in use becomes a backup account.
func ImportSabServers(path string) ([]AccountSetting, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var servers []map[string]string
	var cur map[string]string
	in := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case sectionRe.MatchString(line):
			in, cur = line == "[servers]", nil
		case in && serverRe.MatchString(line):
			cur = map[string]string{"_name": line[2 : len(line)-2]}
			servers = append(servers, cur)
		case cur != nil && strings.Contains(line, "="):
			k, v, _ := strings.Cut(line, "=")
			cur[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	on := func(s map[string]string) bool { v, ok := s["enable"]; return !ok || v == "1" }
	atoi := func(s string, d int) int {
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
		return d
	}
	top, any := 0, false
	for _, s := range servers {
		if on(s) {
			p := atoi(s["priority"], 0)
			if !any || p < top {
				top, any = p, true
			}
		}
	}
	var out []AccountSetting
	for _, s := range servers {
		if s["host"] == "" {
			continue
		}
		name := s["displayname"]
		if name == "" {
			name = s["_name"]
		}
		ssl := s["ssl"] != "0"
		out = append(out, AccountSetting{ID: RandomID(6), Name: name, Server: s["host"], Port: atoi(s["port"], 563), SSL: ssl,
			Username: s["username"], Password: s["password"], Connections: atoi(s["connections"], 8), Enabled: on(s),
			Priority: min(99, max(0, atoi(s["priority"], 0)))})
	}
	return out, nil
}

// MergeAccounts applies account edits from the API: an empty password keeps the stored
// one while the server stays the same (a saved password never goes to another server),
// clear_password removes it; numbers are clamped.
func (m *Manager) MergeAccounts(incoming []map[string]any) []AccountSetting {
	old := map[string]AccountSetting{}
	for _, a := range m.Settings.Accounts {
		old[a.ID] = a
	}
	str := func(v any) string {
		if v == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(v))
	}
	num := func(v any, d int) int {
		switch x := v.(type) {
		case float64:
			return int(x)
		case string:
			if n, err := strconv.Atoi(x); err == nil {
				return n
			}
		}
		return d
	}
	boolean := func(v any, d bool) bool {
		if b, ok := v.(bool); ok {
			return b
		}
		return d
	}
	var out []AccountSetting
	for _, in := range incoming {
		id := str(in["id"])
		prev := old[id]
		a := AccountSetting{ID: id, Name: str(in["name"]), Server: str(in["server"]), Port: num(in["port"], 563), SSL: boolean(in["ssl"], true),
			Username: str(in["username"]), Connections: min(200, max(1, num(in["connections"], 10))), Enabled: boolean(in["enabled"], true),
			Priority: min(99, max(0, num(in["priority"], 0)))}
		if _, given := in["priority"]; !given && boolean(in["backup"], false) {
			a.Priority = 1
		}
		pw, _ := in["password"].(string)
		switch {
		case boolean(in["clear_password"], false):
			a.Password = ""
		case pw != "":
			a.Password = pw
		case strings.EqualFold(a.Server, prev.Server):
			a.Password = prev.Password
		}
		if a.ID == "" {
			a.ID = RandomID(6)
		}
		if a.Server != "" {
			out = append(out, a)
		}
	}
	if out == nil {
		out = []AccountSetting{}
	}
	return out
}
