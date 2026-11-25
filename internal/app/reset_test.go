package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"usnfetch/internal/catalog"
)

func TestReset(t *testing.T) {
	dir := t.TempDir()
	write := func(rel string) {
		p := filepath.Join(dir, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m, err := NewManager(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	m.Settings.CompleteDir = filepath.Join(dir, "my downloads")
	m.Settings.Accounts = []AccountSetting{{ID: "a1", Name: "Rig", Server: "news.example", Port: 563, Username: "u", Password: "p", Connections: 8, Enabled: true}}
	m.Settings.Web.PasswordHash = "web-secret-hash"
	m.Settings.MinFreeGB = 42
	m.Settings.SetupDone = true
	if err := m.SaveSettings(); err != nil {
		t.Fatal(err)
	}
	write("index_cache/f00d-1.usnindex")
	write("index_cache/beef-2.usnlist.gz")
	write("index_cache/notes.txt")       // not the cache's: stays
	write("index_cache/report.tmp.docx") // nor these (the index folder may be one set in Settings)
	write("index_cache/build.tmp")
	write("index_cache/f00d-1.usnindex.123-v3.tmp") // an interrupted conversion: the cache's
	write("art_cache/f00d/a.jpg")
	write("games/f00d.json.gz")
	write("games/f00d.bios.json.gz")
	write("my downloads/Some Build/game.zip") // downloads: never touched
	st := m.Store
	_ = st.SetShareInfo("usn://one", map[string]any{"opened_at": 1.0, "inside": []any{}, "link_check": map[string]any{"found": true}})
	_ = st.SetShareInfo("usn://two", map[string]any{"opened_at": 2.0})
	_ = st.SaveCatalog([]catalog.Entry{{Link: "usn://one", Title: "One"}})
	_ = st.SetMeta("migrated", "1")
	if err := m.RequestReset(ResetOptions{Caches: true, Opened: true, Settings: true, KeepProviders: true}); err != nil {
		t.Fatal(err)
	}
	st.Close()

	m, err = NewManager(dir, true) // the reset happens as it starts
	if err != nil {
		t.Fatal(err)
	}
	defer func() { m.Shutdown(time.Second); m.Store.Close() }()
	for _, gone := range []string{"index_cache/f00d-1.usnindex", "index_cache/beef-2.usnlist.gz", "art_cache/f00d/a.jpg", "games/f00d.json.gz", "games/f00d.bios.json.gz", "reset.json", "index_cache/f00d-1.usnindex.123-v3.tmp"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); err == nil {
			t.Errorf("%s is still there", gone)
		}
	}
	for _, kept := range []string{"index_cache/notes.txt", "index_cache/report.tmp.docx", "index_cache/build.tmp", "my downloads/Some Build/game.zip"} {
		if _, err := os.Stat(filepath.Join(dir, kept)); err != nil {
			t.Errorf("%s was deleted", kept)
		}
	}
	info := m.Store.AllShareInfo()
	if len(info) != 1 || info["usn://one"]["link_check"] == nil || info["usn://one"]["opened_at"] != nil {
		t.Errorf("share info after forgetting what opening learned (health kept): %v", info)
	}
	if c, _ := m.Store.LoadCatalog(); len(c) != 1 {
		t.Errorf("the catalog wasn't asked to be emptied: %v", c)
	}
	s := m.Settings
	if s.MinFreeGB == 42 || s.CompleteDir == filepath.Join(dir, "my downloads") {
		t.Errorf("settings not back to their defaults: %+v", s)
	}
	if len(s.Accounts) != 1 || s.Accounts[0].Password != "p" || !s.SetupDone || s.Web.PasswordHash != "web-secret-hash" {
		t.Errorf("providers and web access should stay: %+v %+v setup=%v", s.Accounts, s.Web, s.SetupDone)
	}
	if m.Store.Meta("migrated") != "1" {
		t.Error("the one-time import's mark must survive a reset (or it imports old files again)")
	}
}

func TestResetEverything(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	m.Settings.Accounts = []AccountSetting{{ID: "a1", Name: "Rig", Server: "news.example", Port: 563, Connections: 8, Enabled: true}}
	m.Settings.SetupDone = true
	_ = m.SaveSettings()
	_ = m.Store.SetShareInfo("usn://one", map[string]any{"opened_at": 1.0, "link_check": map[string]any{"found": true}})
	_ = m.Store.SaveCatalog([]catalog.Entry{{Link: "usn://one", Title: "One"}})
	_ = m.RequestReset(ResetOptions{Caches: true, Opened: true, Health: true, Catalog: true, Jobs: true, Settings: true})
	m.Store.Close()
	m, err = NewManager(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { m.Shutdown(time.Second); m.Store.Close() }()
	if len(m.Store.AllShareInfo()) != 0 {
		t.Error("share info left")
	}
	if c, _ := m.Store.LoadCatalog(); len(c) != 0 {
		t.Error("catalog left")
	}
	if len(m.Settings.Accounts) != 0 || m.Settings.SetupDone {
		t.Errorf("without keeping providers the first-run setup runs again: %+v", m.Settings)
	}
}

func TestForgetShareFiles(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Store.Close()
	for _, rel := range []string{"index_cache/f00d-1.usnindex", "index_cache/f00d-1.usnlist.gz", "index_cache/beef-2.usnindex", "art_cache/f00d/a.jpg"} {
		p := filepath.Join(dir, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte("xx"), 0o644)
	}
	n, size, err := m.ForgetShareFiles("f00d")
	if err != nil || n != 3 || size != 6 {
		t.Fatalf("forget: %d files, %d bytes, %v", n, size, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "index_cache/beef-2.usnindex")); err != nil {
		t.Error("another share's index went too")
	}
	m.PinIndex(filepath.Join(dir, "index_cache/beef-2.usnindex"))
	if _, _, err := m.ForgetShareFiles("beef"); err != ErrShareInUse {
		t.Errorf("a share in use: %v", err)
	}
}

func TestIsCacheFile(t *testing.T) {
	for name, want := range map[string]bool{
		"d489088eb3d1d5c696fec58ae7367770-8f57dc981148104f.usnindex": true, "f00d-1.usnlist.gz": true,
		"f00d-1.usnindex.4412-v3.tmp": true, "f00d-1.usnlist.gz.88.tmp": true,
		"report.tmp.docx": false, "build.tmp": false, "notes.txt": false, "My-Game.usnindex": false, "-x.usnindex": false,
	} {
		if got := isCacheFile(name); got != want {
			t.Errorf("isCacheFile(%q) = %v, want %v", name, got, want)
		}
	}
}
