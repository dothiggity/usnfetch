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
}

func LoadCatalog(s *Store) (*Catalog, error) {
	e, err := s.LoadCatalog()
	if err != nil {
		return nil, err
	}
	return &Catalog{store: s, Entries: e}, nil
}

// Merge adds new links and refreshes the listing details of known ones. Returns (added, updated).
func (c *Catalog) Merge(newEntries []catalog.Entry) (int, int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	idx := map[string]int{}
	for i, e := range c.Entries {
		idx[e.Link] = i
	}
	added, updated := 0, 0
	t := now()
	for _, n := range newEntries {
		i, ok := idx[n.Link]
		if !ok {
			n.Added = t
			if n.Also == nil {
				n.Also = []string{}
			}
			c.Entries = append(c.Entries, n)
			idx[n.Link] = len(c.Entries) - 1
			added++
			continue
		}
		o := c.Entries[i]
		before := o
		before.Also = append([]string{}, o.Also...)
		o.Title, o.Variant, o.Listed, o.Size, o.Source, o.SKU, o.Info = n.Title, n.Variant, n.Listed, n.Size, n.Source, n.SKU, n.Info
		seen := map[string]bool{}
		var also []string
		for _, a := range append(append([]string{}, o.Also...), n.Also...) {
			if !seen[a] {
				seen[a] = true
				also = append(also, a)
			}
		}
		if also == nil {
			also = []string{}
		}
		o.Also = also
		if !reflect.DeepEqual(before, o) {
			updated++
		}
		c.Entries[i] = o
	}
	if added > 0 || updated > 0 {
		return added, updated, c.store.SaveCatalog(c.Entries)
	}
	return added, updated, nil
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
	c.Entries = keep
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
// one, clear_password removes it; numbers are clamped.
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
		default:
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
