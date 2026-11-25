package web

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"usnfetch/internal/app"
)

// ---- first-run setup ------------------------------------------------------------------
//
// The browser walks a new install through its Usenet providers (the account endpoints in
// server.go, including the SABnzbd import and the connection test) and its folders (here),
// then marks setup as done. Installs that already have accounts count as set up.

func (s *Server) setupRoutes() {
	s.api("GET /api/setup", func(w http.ResponseWriter, r *http.Request) (any, error) {
		st := s.M.Settings
		return map[string]any{"setup_done": st.SetupDone, "accounts": s.settingsView()["accounts"],
			"complete_dir": st.CompleteDir, "index_dir": st.IndexDir, "index_dir_default": filepath.Join(s.M.DataDir, "index_cache"),
			"sab_ini": app.SabIniDefault(), "data_dir": s.M.DataDir}, nil
	})
	s.api("POST /api/setup/folder", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return folderCheck(str(body(r)["path"])), nil
	})
	s.api("POST /api/setup/finish", func(w http.ResponseWriter, r *http.Request) (any, error) {
		b := body(r)
		st := s.M.Settings
		if len(st.Accounts) == 0 {
			return nil, fail(400, "add a Usenet provider first", "no_account")
		}
		dirs := map[string]string{"complete_dir": strings.TrimSpace(str(b["complete_dir"])), "index_dir": strings.TrimSpace(str(b["index_dir"]))}
		if dirs["complete_dir"] == "" {
			dirs["complete_dir"] = st.CompleteDir
		}
		for key, p := range dirs {
			if p == "" { // index_dir: the default under the data folder
				continue
			}
			c := folderCheck(p)
			if c["ok"] != true {
				return nil, fail(400, str(c["error"]), "bad_folder")
			}
			if err := os.MkdirAll(str(c["path"]), 0o755); err != nil {
				return nil, fail(400, "cannot create "+str(c["path"])+": "+err.Error(), "bad_folder")
			}
			dirs[key] = str(c["path"])
		}
		st.CompleteDir, st.IndexDir, st.SetupDone = dirs["complete_dir"], dirs["index_dir"], true
		if err := s.M.SaveSettings(); err != nil {
			return nil, err
		}
		s.M.Log("setup finished: downloads go to " + st.CompleteDir)
		return s.settingsView(), nil
	})
	s.api("POST /api/setup/skip", func(w http.ResponseWriter, r *http.Request) (any, error) {
		s.M.Settings.SetupDone = true
		return map[string]any{"ok": true}, s.M.SaveSettings()
	})
	s.api("GET /api/fs/folders", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return listFolders(r.URL.Query().Get("path"))
	})
}

// listFolders lists the folders in one folder, for picking a folder in the browser;
// "" lists the drives (Windows) or "/", and the home folder.
func listFolders(p string) (map[string]any, error) {
	p = strings.TrimSpace(p)
	folders := []map[string]any{}
	if p == "" {
		if runtime.GOOS == "windows" {
			for c := 'C'; c <= 'Z'; c++ {
				d := string(c) + `:\`
				if _, err := os.Stat(d); err == nil {
					folders = append(folders, map[string]any{"name": d, "path": d})
				}
			}
		} else {
			folders = append(folders, map[string]any{"name": "/", "path": "/"})
		}
		if home, err := os.UserHomeDir(); err == nil {
			folders = append(folders, map[string]any{"name": "Home (" + home + ")", "path": home})
		}
		return map[string]any{"path": "", "parent": nil, "folders": folders}, nil
	}
	if !filepath.IsAbs(p) {
		return nil, fail(400, `use a full path, such as D:\Downloads`, "bad_folder")
	}
	p = filepath.Clean(p)
	ents, err := os.ReadDir(p)
	if err != nil {
		var pe *os.PathError
		if errors.As(err, &pe) {
			err = pe.Err
		}
		return nil, fail(400, "can't open "+p+": "+err.Error(), "bad_folder")
	}
	for _, e := range ents {
		name := e.Name()
		if !e.IsDir() || strings.HasPrefix(name, "$") || name == "System Volume Information" {
			continue
		}
		folders = append(folders, map[string]any{"name": name, "path": filepath.Join(p, name)})
		if len(folders) >= 3000 {
			break
		}
	}
	sort.Slice(folders, func(i, j int) bool {
		return strings.ToLower(folders[i]["name"].(string)) < strings.ToLower(folders[j]["name"].(string))
	})
	var parent any = ""
	if up := filepath.Dir(p); up != p {
		parent = up
	}
	out := map[string]any{"path": p, "parent": parent, "folders": folders}
	if free, err := app.DiskFree(p); err == nil {
		out["free"] = free
	}
	return out, nil
}

// folderCheck: whether a folder can be used for downloads -- a full path, on a drive that
// exists, writable (checked by creating a file in it, or in the nearest folder above it
// that exists), and how much space is free there.
func folderCheck(p string) map[string]any {
	p = strings.TrimSpace(p)
	out := map[string]any{"ok": false, "path": p, "exists": false}
	bad := func(msg string) map[string]any { out["error"] = msg; return out }
	if p == "" {
		return bad("choose a folder")
	}
	if !filepath.IsAbs(p) {
		return bad(`use a full path, such as D:\Downloads\usnfetch`)
	}
	p = filepath.Clean(p)
	out["path"] = p
	if why := badFolderName(p); why != "" {
		return bad(why)
	}
	if st, err := os.Stat(p); err == nil {
		if !st.IsDir() {
			return bad("that is a file, not a folder")
		}
		out["exists"] = true
	}
	probe := p
	for {
		if st, err := os.Stat(probe); err == nil && st.IsDir() {
			break
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return bad("that drive or location does not exist")
		}
		probe = parent
	}
	f, err := os.CreateTemp(probe, ".usnfetch-write-test-*")
	if err != nil {
		var pe *os.PathError
		if errors.As(err, &pe) {
			err = pe.Err
		}
		return bad("cannot write in " + probe + ": " + err.Error())
	}
	name := f.Name()
	f.Close()
	_ = os.Remove(name)
	if free, err := app.DiskFree(probe); err == nil {
		out["free"] = free
		if free < lowFree {
			out["low"] = true
			if d, dfree := roomiestDrive(); d != "" && dfree > 2*free && dfree >= lowFree && !strings.EqualFold(filepath.VolumeName(p), filepath.VolumeName(d)) {
				out["roomier"] = map[string]any{"path": filepath.Join(d, "usnfetch"), "free": dfree}
			}
		}
	}
	out["ok"] = true
	return out
}

// lowFree: below this much free space, setup warns: builds run from a hundred GB to several TB.
const lowFree = 100e9

// badFolderName: why Windows can't make a folder of that name ("" if it can): a character
// it doesn't allow, a name it keeps for devices (CON, NUL, COM1…), or one ending in a dot or
// space. Elsewhere only a NUL byte is refused.
func badFolderName(p string) string {
	if strings.ContainsRune(p, 0) {
		return "that name has a character folders can't have"
	}
	if runtime.GOOS != "windows" {
		return ""
	}
	rest := strings.TrimPrefix(p, filepath.VolumeName(p))
	for _, part := range strings.FieldsFunc(rest, func(r rune) bool { return r == '\\' || r == '/' }) {
		if i := strings.IndexFunc(part, func(r rune) bool { return strings.ContainsRune(`<>:"|?*`, r) || r < 0x20 }); i >= 0 {
			return fmt.Sprintf("folder names can't have %q in them (%s)", part[i:i+1], part)
		}
		if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return fmt.Sprintf("folder names can't end in a dot or a space (%q)", part)
		}
		base, _, _ := strings.Cut(strings.ToUpper(part), ".")
		switch strings.TrimRight(base, " ") {
		case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
			"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
			return fmt.Sprintf("Windows keeps the name %q for a device: pick another", part)
		}
	}
	return ""
}

// roomiestDrive: the local disk with the most free space (Windows: C: to Z:, not network,
// cloud or removable drives), "" elsewhere.
func roomiestDrive() (string, int64) {
	if runtime.GOOS != "windows" {
		return "", 0
	}
	best, most := "", int64(0)
	for c := 'C'; c <= 'Z'; c++ {
		d := string(c) + `:\`
		if !app.LocalDrive(d) {
			continue
		}
		if free, err := app.DiskFree(d); err == nil && free > most {
			best, most = d, free
		}
	}
	return best, most
}
