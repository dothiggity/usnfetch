package web

import (
	"net/http"
	"os"
	"os/exec"
	"runtime"
)

// "Show in folder": opens a download's folder in the file manager of the machine running
// usnfetch, so only a browser on that machine may ask for it.

// openFolder opens dir in the file manager (Explorer, Finder, or what xdg-open picks).
func openFolder(dir string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer.exe", dir)
	case "darwin":
		cmd = exec.Command("open", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // Explorer exits 1 even when it worked: nothing to check
	return nil
}

func (s *Server) revealRoutes() {
	// the data folder: settings, catalog, jobs and share indexes (System page)
	s.api("POST /api/system/reveal-data", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if !isLoopback(r) {
			return nil, fail(403, "Folders can only be opened on the computer running usnfetch.", "not_local")
		}
		dir := s.M.DataDir
		if st, err := os.Stat(dir); dir == "" || err != nil || !st.IsDir() {
			return nil, fail(404, "The data folder isn't there.", "no_folder")
		}
		if err := openFolder(dir); err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "path": dir}, nil
	})
	s.api("POST /api/jobs/{id}/reveal", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if !isLoopback(r) {
			return nil, fail(403, "Folders can only be opened on the computer running usnfetch.", "not_local")
		}
		j := s.M.Get(r.PathValue("id"))
		if j == nil {
			return nil, fail(404, "no such job", "")
		}
		dir := j.OutDir
		if st, err := os.Stat(dir); dir == "" || err != nil || !st.IsDir() {
			return nil, fail(404, "Its folder isn't there (not started yet, or moved or deleted).", "no_folder")
		}
		if err := openFolder(dir); err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "path": dir}, nil
	})
}
