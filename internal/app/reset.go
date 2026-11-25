package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"usnfetch/internal/share"
)

// Resetting usnfetch, whole (System → Reset) or for one share (Forget this build), to test
// it from scratch. A reset is asked for, then done the next time usnfetch starts, before
// anything has opened the files it deletes (on Windows an open file can't be deleted).
// Downloaded files are never touched.

// ResetOptions: what a reset wipes.
type ResetOptions struct {
	Caches        bool `json:"caches"`         // share indexes and file lists, box art, game and BIOS lists
	Opened        bool `json:"opened"`         // what opening shares learned: when, their details, what's inside
	Health        bool `json:"health"`         // link checks and availability checks
	Catalog       bool `json:"catalog"`        // every link in the catalog
	Jobs          bool `json:"jobs"`           // the download queue and history
	Settings      bool `json:"settings"`       // settings back to their defaults (the web password stays)
	KeepProviders bool `json:"keep_providers"` // with Settings: keep the Usenet providers
}

// Any: whether it wipes anything.
func (o ResetOptions) Any() bool {
	return o.Caches || o.Opened || o.Health || o.Catalog || o.Jobs || o.Settings
}

func resetPath(dataDir string) string { return filepath.Join(dataDir, "reset.json") }

// RequestReset: a reset for the next start (it replaces one asked for before).
func (m *Manager) RequestReset(o ResetOptions) error {
	if !o.Any() {
		return errors.New("nothing to reset")
	}
	b, _ := json.Marshal(o)
	return os.WriteFile(resetPath(m.DataDir), b, 0o600)
}

// pendingReset: the reset asked for, if any.
func pendingReset(dataDir string) *ResetOptions {
	b, err := os.ReadFile(resetPath(dataDir))
	if err != nil {
		return nil
	}
	var o ResetOptions
	if json.Unmarshal(b, &o) != nil || !o.Any() {
		_ = os.Remove(resetPath(dataDir))
		return nil
	}
	return &o
}

// resetSettings: the settings back to their defaults, keeping how the web app is reached (its
// password, address and port: a reset never locks you out or opens it up) and, when asked,
// the Usenet providers. Without providers the first-run setup runs again.
func (m *Manager) resetSettings(keepProviders bool) error {
	s := defaultSettings()
	s.Web = m.Settings.Web
	if keepProviders {
		s.Accounts = m.Settings.Accounts
		s.SetupDone = len(s.Accounts) > 0
	}
	if err := SaveSettings(&s, m.SettingsPath); err != nil {
		return err
	}
	fresh, err := LoadSettings(m.SettingsPath)
	if err != nil {
		return err
	}
	m.Settings = fresh
	return nil
}

// applyReset does a reset asked for, at startup (the store is open, nothing else is).
// indexDirs: where the share indexes were kept, before and after the settings' reset.
func (m *Manager) applyReset(o ResetOptions, settingsErr error, indexDirs ...string) {
	var done []string
	if o.Settings && settingsErr != nil {
		done = append(done, "the settings couldn't be reset ("+settingsErr.Error()+")")
	} else if o.Settings {
		done = append(done, "settings back to their defaults"+map[bool]string{true: " (providers kept)", false: ""}[o.KeepProviders])
	}
	if o.Caches {
		n, size := 0, int64(0)
		seen := map[string]bool{}
		for _, d := range indexDirs {
			if d == "" || seen[strings.ToLower(d)] {
				continue
			}
			seen[strings.ToLower(d)] = true
			a, b := removeFiles(d, false, isCacheFile)
			n, size = n+a, size+b
		}
		a, b := removeFiles(filepath.Join(m.DataDir, "art_cache"), true, nil)
		c, d := removeFiles(filepath.Join(m.DataDir, "games"), false, func(name string) bool {
			return strings.HasSuffix(name, ".json.gz") || strings.Contains(name, ".tmp")
		})
		done = append(done, fmt.Sprintf("%d cached files deleted (%s)", n+a+c, sizeText(size+b+d)))
	}
	if o.Opened || o.Health {
		if n, err := m.Store.ForgetAllShareInfo(o.Opened, o.Health); err == nil {
			what := map[[2]bool]string{{true, false}: "what opening them learned", {false, true}: "their health checks", {true, true}: "everything about them"}[[2]bool{o.Opened, o.Health}]
			done = append(done, fmt.Sprintf("forgot %s, for %d share%s", what, n, map[bool]string{true: "", false: "s"}[n == 1]))
		}
	}
	if o.Catalog {
		if err := m.Store.ClearCatalog(); err == nil {
			done = append(done, "catalog emptied")
		}
	}
	if o.Jobs {
		if err := m.Store.ClearJobs(); err == nil {
			done = append(done, "download queue and history cleared")
		}
	}
	m.Log("reset: " + strings.Join(done, "; ") + ". Downloaded files are untouched.")
}

// isCacheFile: a share index cache's own files: named after a share's folder id ("<hex>-…"),
// full indexes, file lists (zipped or not) and the leftovers of interrupted builds of them.
// Nothing else is ever deleted there: the folder may be one set in Settings.
func isCacheFile(name string) bool {
	i := strings.IndexByte(name, '-')
	if i <= 0 {
		return false
	}
	for _, r := range name[:i] {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return strings.Contains(name, ".usnindex") || strings.Contains(name, ".usnlist")
}

// removeFiles deletes what is in dir (subfolders too when all; else the files keep says),
// leaving dir itself: how many files, and their size.
func removeFiles(dir string, all bool, keep func(string) bool) (n int, size int64) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	for _, e := range ents {
		p := filepath.Join(dir, e.Name())
		if e.IsDir() {
			if all {
				_ = filepath.WalkDir(p, func(_ string, d os.DirEntry, err error) error {
					if err == nil && !d.IsDir() {
						if info, err := d.Info(); err == nil {
							n++
							size += info.Size()
						}
					}
					return nil
				})
				_ = os.RemoveAll(p)
			}
			continue
		}
		if !all && keep != nil && !keep(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err == nil && os.Remove(p) == nil {
			n++
			size += info.Size()
		}
	}
	return n, size
}

// ErrShareInUse: the share's index is open for something else (a download, a search across
// shares).
var ErrShareInUse = errors.New("this build is in use right now (by a download, or a search across builds): try again when that's done")

// ForgetShareFiles deletes what the cache keeps of one share (its index or file list, and
// its box art): how many files, and their size. The caller closes the share first; a
// download using it is left alone (ErrShareInUse).
func (m *Manager) ForgetShareFiles(folderID string) (int, int64, error) {
	if folderID == "" || strings.ContainsAny(folderID, `/\.:`) {
		return 0, 0, errors.New("no such share")
	}
	share.CacheMu.Lock() // not while the cache's upkeep or LoadShare is at one of them: list them under it too
	defer share.CacheMu.Unlock()
	dir := m.IndexDir()
	var files []string
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), folderID+"-") && isCacheFile(e.Name()) {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	for _, p := range files {
		if m.indexInUse(p) {
			return 0, 0, ErrShareInUse
		}
	}
	n, size := 0, int64(0)
	for _, p := range files {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		// just closed: Windows may hold on to it for a moment
		for try := 0; ; try++ {
			if err = os.Remove(p); err == nil || os.IsNotExist(err) || try == 10 {
				break
			}
			time.Sleep(150 * time.Millisecond)
		}
		if err != nil && !os.IsNotExist(err) {
			return n, size, fmt.Errorf("couldn't delete %s: %w", filepath.Base(p), err)
		}
		n++
		size += info.Size()
	}
	a, b := removeFiles(filepath.Join(m.DataDir, "art_cache", folderID), true, nil)
	_ = os.Remove(filepath.Join(m.DataDir, "art_cache", folderID))
	return n + a, size + b, nil
}

// JobCount: how many downloads the queue and history hold.
func (m *Manager) JobCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.jobs)
}

// Running: whether a job in this state is at work (planning, downloading, extracting, stopping).
func Running(state string) bool { return runningStates[state] }

// Downloading: whether a download is running now (planning, downloading, extracting).
func (m *Manager) Downloading() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		j.mu.Lock()
		run := runningStates[j.State]
		j.mu.Unlock()
		if run {
			return true
		}
	}
	return false
}
