package web

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"usnfetch/internal/engine"
	"usnfetch/internal/share"
)

// A share opened from its saved file list (share/cache.go) can be browsed and searched as
// usual. Previews, streaming, checks and box art not seen before need the rest of its index,
// which is fetched in the background the first time one of them is asked for (or by "Fetch
// now"); the session then carries on with the full index.

// needFull: nil when the share's full index is open; otherwise starts fetching it and says so.
func (s *Server) needFull(ss *session) error {
	ss.mu.Lock()
	ix := ss.Index
	ss.mu.Unlock()
	if ix != nil && ix.HasPool {
		return nil
	}
	s.fetchFull(ss)
	return fail(409, "Fetching the rest of this share's index first (only its file list was saved). Big shares take a minute or two.", "fetching_index")
}

// fetchFull fetches the share's full index in the background and switches the session to it.
func (s *Server) fetchFull(ss *session) {
	ss.mu.Lock()
	if ss.Fetching || (ss.Index != nil && ss.Index.HasPool) {
		ss.mu.Unlock()
		return
	}
	ss.Fetching, ss.FetchErr = true, ""
	password := ss.password
	ss.mu.Unlock()
	go func() {
		done := func(msg string) {
			ss.mu.Lock()
			ss.Fetching, ss.FetchErr = false, msg
			ss.mu.Unlock()
			if msg != "" {
				ss.log("could not fetch the rest of the index: " + msg)
			}
		}
		ss.log("fetching the rest of the index (what downloads, previews and checks need)")
		accts, err := s.M.Accounts()
		if err != nil {
			done(err.Error())
			return
		}
		ix, err := share.LoadShare(ss.Link, share.LoadOpts{Accounts: accts.All(), Password: password, IndexDir: s.M.IndexDir(), Log: ss.log, Meter: ss.measure()})
		ss.measured()
		if err == nil {
			err = ix.EnsureTree(ss.log)
			if err != nil {
				ix.Close()
			}
		}
		if err != nil {
			done(err.Error())
			return
		}
		s.smu.Lock()
		live := s.sessions[ss.ID] == ss
		s.smu.Unlock()
		if !live { // closed meanwhile
			ix.Close()
			done("")
			return
		}
		s.M.PinIndex(ix.Path)
		ss.mu.Lock()
		old := ss.Index
		ss.Index, ss.art = ix, nil // gallery titles can be read from the share now
		ss.mu.Unlock()
		if old != nil {
			s.M.UnpinIndex(old.Path)
			time.AfterFunc(2*time.Minute, old.Close) // requests that started on it finish first
		}
		done("")
		ss.log("ready: downloads, previews and checks work now")
		if info := s.M.Store.ShareInfo(ss.Link); info != nil && needsCheck(info) {
			ss.mu.Lock()
			start := !ss.Checking
			ss.Checking = true
			ss.mu.Unlock()
			if start {
				go s.runCheck(ss, ix, 64)
			}
		}
		s.M.TidyIndexCache() // the saved file list is not needed next to the full index
	}()
}

// ---- box art on disk ----------------------------------------------------------------
//
// Pictures up to artMaxFile are kept in <data>/art_cache once shown, so a gallery seen before
// opens without going to Usenet, even when only the share's file list is saved. The cache
// upkeep (app.Manager) keeps the most recently shown within its limit.

const artMaxFile = 8 << 20

func (s *Server) artFile(ix *share.Index, fe share.FileEntry) string {
	folder, _ := ix.Info["folder_id"].(string)
	if folder == "" || strings.ContainsAny(folder, `/\.:`) {
		return ""
	}
	h := sha1.Sum([]byte(fe.Path + "|" + strconv.FormatInt(fe.Size, 10)))
	return filepath.Join(s.M.DataDir, "art_cache", folder, hex.EncodeToString(h[:12])+strings.ToLower(path.Ext(fe.Path)))
}

// serveArt serves a picture from the art cache, filling the cache from Usenet when the full
// index is open. false: not cached and the full index is not open (the caller says so).
func (s *Server) serveArt(w http.ResponseWriter, r *http.Request, ix *share.Index, fe share.FileEntry, name string) bool {
	p := s.artFile(ix, fe)
	if p == "" {
		return false
	}
	h := w.Header()
	h.Set("Cache-Control", "private, max-age=86400")
	h.Set("Content-Disposition", "inline")
	if f, err := os.Open(p); err == nil {
		defer f.Close()
		now := time.Now()
		_ = os.Chtimes(p, now, now) // recently shown
		http.ServeContent(w, r, name, time.Time{}, f)
		return true
	}
	if !ix.HasPool {
		return false
	}
	pool, sem, err := s.streamResources()
	if err != nil {
		writeErr(w, err)
		return true
	}
	rd, err := engine.NewFileReader(ix, fe, pool, s.cache, sem)
	if err != nil {
		writeErr(w, err)
		return true
	}
	var buf bytes.Buffer
	if err := rd.WriteRange(&buf, 0, fe.Size); err != nil {
		writeErr(w, err)
		return true
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err == nil {
		tmp := p + ".tmp"
		if os.WriteFile(tmp, buf.Bytes(), 0o644) == nil && os.Rename(tmp, p) != nil {
			_ = os.Remove(tmp)
		}
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(buf.Bytes()))
	return true
}
