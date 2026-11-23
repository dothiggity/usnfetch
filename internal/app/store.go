package app

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"

	"usnfetch/internal/catalog"
)

// Store is the app database (usnfetch.db): jobs, each job's finished files, the catalog
// and what is known about shares that were opened. WAL, one writer.
type Store struct {
	db       *sql.DB
	mu       sync.Mutex
	isDone   *sql.Stmt
	markDone *sql.Stmt
}

const storeDDL = `
CREATE TABLE IF NOT EXISTS meta(k TEXT PRIMARY KEY, v);
CREATE TABLE IF NOT EXISTS jobs(job_id TEXT PRIMARY KEY, ord INTEGER, data TEXT);
CREATE TABLE IF NOT EXISTS job_done(job_id TEXT, path TEXT, PRIMARY KEY(job_id, path)) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS catalog(link TEXT PRIMARY KEY, ord INTEGER, data TEXT);
CREATE TABLE IF NOT EXISTS shareinfo(link TEXT PRIMARY KEY, data TEXT);
`

func OpenStore(path string) (*Store, error) {
	abs, _ := filepath.Abs(path)
	u := url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(abs), "/")}
	db, err := sql.Open("sqlite", u.String()+"?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(30000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(storeDDL); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if s.isDone, err = db.Prepare("SELECT 1 FROM job_done WHERE job_id = ? AND path = ?"); err != nil {
		return nil, err
	}
	if s.markDone, err = db.Prepare("INSERT OR IGNORE INTO job_done VALUES (?, ?)"); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() { s.db.Close() }

func (s *Store) Meta(k string) string {
	var v sql.NullString
	_ = s.db.QueryRow("SELECT v FROM meta WHERE k = ?", k).Scan(&v)
	return v.String
}

func (s *Store) SetMeta(k, v string) error {
	_, err := s.db.Exec("INSERT OR REPLACE INTO meta VALUES (?, ?)", k, v)
	return err
}

// ---- jobs ----

func (s *Store) SaveJobs(jobs []*Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	ids := map[string]bool{}
	for i, j := range jobs {
		b, err := json.Marshal(j.persisted())
		if err != nil {
			tx.Rollback()
			return err
		}
		if _, err := tx.Exec("INSERT OR REPLACE INTO jobs VALUES (?, ?, ?)", j.ID, i, string(b)); err != nil {
			tx.Rollback()
			return err
		}
		ids[j.ID] = true
	}
	rows, err := tx.Query("SELECT job_id FROM jobs")
	if err != nil {
		tx.Rollback()
		return err
	}
	var gone []string
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		if !ids[id] {
			gone = append(gone, id)
		}
	}
	rows.Close()
	for _, id := range gone {
		_, _ = tx.Exec("DELETE FROM jobs WHERE job_id = ?", id)
		_, _ = tx.Exec("DELETE FROM job_done WHERE job_id = ?", id)
	}
	return tx.Commit()
}

func (s *Store) LoadJobs() ([]*Job, error) {
	rows, err := s.db.Query("SELECT data FROM jobs ORDER BY ord")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Job
	for rows.Next() {
		var data string
		_ = rows.Scan(&data)
		j, err := restoreJob([]byte(data))
		if err == nil {
			out = append(out, j)
		}
	}
	for _, j := range out {
		_ = s.db.QueryRow("SELECT COUNT(*) FROM job_done WHERE job_id = ?", j.ID).Scan(&j.doneCount)
	}
	return out, nil
}

func (s *Store) IsDone(jobID, path string) bool {
	var one int
	return s.isDone.QueryRow(jobID, path).Scan(&one) == nil
}

func (s *Store) MarkDone(jobID, path string) (bool, error) {
	r, err := s.markDone.Exec(jobID, path)
	if err != nil {
		return false, err
	}
	n, _ := r.RowsAffected()
	return n > 0, nil
}

// MarkDoneMany records many finished paths in one transaction; it returns how many were new.
func (s *Store) MarkDoneMany(jobID string, paths []string) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	st := tx.Stmt(s.markDone)
	var added int64
	for _, p := range paths {
		r, err := st.Exec(jobID, p)
		if err != nil {
			tx.Rollback()
			return 0, err
		}
		n, _ := r.RowsAffected()
		added += n
	}
	return added, tx.Commit()
}

func (s *Store) ResetDone(jobID string) error {
	_, err := s.db.Exec("DELETE FROM job_done WHERE job_id = ?", jobID)
	return err
}

func (s *Store) DonePaths(jobID string) ([]string, error) {
	rows, err := s.db.Query("SELECT path FROM job_done WHERE job_id = ?", jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		_ = rows.Scan(&p)
		out = append(out, p)
	}
	return out, nil
}

// ---- catalog ----

func (s *Store) LoadCatalog() ([]catalog.Entry, error) {
	rows, err := s.db.Query("SELECT data FROM catalog ORDER BY ord")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []catalog.Entry{}
	for rows.Next() {
		var data string
		_ = rows.Scan(&data)
		var e catalog.Entry
		if json.Unmarshal([]byte(data), &e) == nil {
			if e.Also == nil {
				e.Also = []string{}
			}
			out = append(out, e)
		}
	}
	return out, nil
}

func (s *Store) SaveCatalog(entries []catalog.Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM catalog"); err != nil {
		tx.Rollback()
		return err
	}
	for i, e := range entries {
		b, _ := json.Marshal(e)
		if _, err := tx.Exec("INSERT OR REPLACE INTO catalog VALUES (?, ?, ?)", e.Link, i, string(b)); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// ---- share info (what opening or checking a share learned) ----

func (s *Store) ShareInfo(link string) map[string]any {
	var data string
	if s.db.QueryRow("SELECT data FROM shareinfo WHERE link = ?", link).Scan(&data) != nil {
		return nil
	}
	m := map[string]any{}
	_ = json.Unmarshal([]byte(data), &m)
	return m
}

func (s *Store) AllShareInfo() map[string]map[string]any {
	out := map[string]map[string]any{}
	rows, err := s.db.Query("SELECT link, data FROM shareinfo")
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var link, data string
		_ = rows.Scan(&link, &data)
		m := map[string]any{}
		if json.Unmarshal([]byte(data), &m) == nil {
			out[link] = m
		}
	}
	return out
}

func (s *Store) SetShareInfo(link string, m map[string]any) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.db.Exec("INSERT OR REPLACE INTO shareinfo VALUES (?, ?)", link, string(b))
	return err
}

// ---- one-time import of the Python versions' JSON files ----

// Migrate imports usnfetch_gui_state.json (+ its done logs), usnfetch_catalog.json and
// usnfetch_shareinfo.json from dir, once. The JSON files are left as they were.
func (s *Store) Migrate(dir string, log func(string)) error {
	if s.Meta("migrated") != "" {
		return nil
	}
	var notes []string
	// jobs
	statePath := filepath.Join(dir, "usnfetch_gui_state.json")
	if raw, err := os.ReadFile(statePath); err == nil {
		var doc struct{ Jobs []json.RawMessage }
		if json.Unmarshal(raw, &doc) == nil {
			var jobs []*Job
			doneDir := strings.TrimSuffix(statePath, ".json") + "_done"
			for _, r := range doc.Jobs {
				j, err := restoreJob(r)
				if err != nil {
					continue
				}
				jobs = append(jobs, j)
				var legacy struct {
					DoneFiles []string `json:"done_files"`
				}
				_ = json.Unmarshal(r, &legacy)
				paths := legacy.DoneFiles
				if f, err := os.Open(filepath.Join(doneDir, safeName(j.ID)+".txt")); err == nil {
					sc := bufio.NewScanner(f)
					sc.Buffer(make([]byte, 1<<20), 1<<20)
					for sc.Scan() {
						if p := sc.Text(); p != "" {
							paths = append(paths, p)
						}
					}
					f.Close()
				}
				for _, p := range paths {
					if _, err := s.MarkDone(j.ID, p); err != nil {
						return err
					}
				}
			}
			if err := s.SaveJobs(jobs); err != nil {
				return err
			}
			notes = append(notes, fmt.Sprintf("%d job(s)", len(jobs)))
		}
	}
	// catalog
	if raw, err := os.ReadFile(filepath.Join(dir, "usnfetch_catalog.json")); err == nil {
		var doc struct{ Entries []catalog.Entry }
		if json.Unmarshal(raw, &doc) == nil {
			if err := s.SaveCatalog(doc.Entries); err != nil {
				return err
			}
			notes = append(notes, fmt.Sprintf("%d catalog entries", len(doc.Entries)))
		}
	}
	// share info
	if raw, err := os.ReadFile(filepath.Join(dir, "usnfetch_shareinfo.json")); err == nil {
		m := map[string]map[string]any{}
		if json.Unmarshal(raw, &m) == nil {
			for link, info := range m {
				if err := s.SetShareInfo(link, info); err != nil {
					return err
				}
			}
			notes = append(notes, fmt.Sprintf("%d known shares", len(m)))
		}
	}
	if len(notes) > 0 && log != nil {
		log("imported from the previous version's files: " + strings.Join(notes, ", "))
	}
	return s.SetMeta("migrated", "1")
}

var errLocked = errors.New("usnfetch is already running with this data folder (the desktop app, the Python web app or another usnfetch); close it first")

// InstanceLock: one app per data folder. Same lock file and byte as the Python apps.
type InstanceLock struct{ f *os.File }

func Lock(path string) (*InstanceLock, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		f.Close()
		return nil, errLocked
	}
	return &InstanceLock{f}, nil
}

func (l *InstanceLock) Release() {
	if l != nil && l.f != nil {
		l.f.Close()
	}
}
