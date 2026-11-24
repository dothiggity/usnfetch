package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"usnfetch/internal/engine"
	"usnfetch/internal/extract"
	"usnfetch/internal/nntp"
	"usnfetch/internal/share"
)

var (
	activeStates   = map[string]bool{"queued": true, "paused": true, "planning": true, "downloading": true, "extracting": true, "stopping": true}
	runningStates  = map[string]bool{"planning": true, "downloading": true, "extracting": true, "stopping": true}
	finishedStates = map[string]bool{"completed": true, "failed": true, "stopped": true}
)

const filesKeep = 5000 // a job keeps the share's file listing (for choosing files) only up to this many

// FileInfo is one entry of a job's (small) file listing.
type FileInfo struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Segments int64  `json:"segments"`
	Packed   bool   `json:"packed,omitempty"`
}

// Job is one download.
type Job struct {
	ID              string         `json:"job_id"`
	Link            string         `json:"link"`
	Name            string         `json:"name"`
	NameAuto        bool           `json:"name_auto"`
	Category        string         `json:"category"`
	FileSel         []string       `json:"file_sel"` // share paths, "dir/" = everything under dir; nil = all
	Password        string         `json:"-"`
	ArchivePassword string         `json:"-"`
	PlanOnly        bool           `json:"plan_only"`
	State           string         `json:"state"`
	Detail          string         `json:"detail"`
	Added           float64        `json:"added"`
	Started         *float64       `json:"started"`
	Finished        *float64       `json:"finished"`
	OutDir          string         `json:"out_dir"`
	Size            int64          `json:"size"`
	Files           []FileInfo     `json:"files"`
	FileCount       int            `json:"file_count"`
	Result          map[string]any `json:"result"`
	IndexFile       string         `json:"index_file"`
	ExtractMode     string         `json:"extract_mode,omitempty"` // "" = the settings' default (see DefaultExtractMode)
	Existing        string         `json:"existing,omitempty"`     // files already on disk: "" skip if the size matches, "verify" (SHA-256), "redownload"
	// Layout "share": files keep the share's folders, minus the share's own single top
	// folder (the job folder stands in for it). "" (jobs from older versions): the top
	// folder common to the selection is dropped, so resumed jobs keep their layout.
	Layout   string   `json:"layout,omitempty"`
	Warnings []string `json:"warnings,omitempty"` // problems that did not fail the job (extraction)
	// Picked says what of the share the job takes ("" = all of it; "Disc 2", "5 items"):
	// jobs of one share share a folder name, so this is what tells them apart.
	Picked string `json:"picked,omitempty"`

	// runtime only
	mu          sync.Mutex
	task        engine.Task
	current     string
	filePos     [2]int
	bytesPrev   int64
	cancel      atomic.Bool
	deleted     bool
	removeFiles bool
	doneCount   int64
	exact       map[string]bool
	prefixes    []string
	pauseStop   bool // stopped by a pause: goes back to the queue
	holdStop    bool // paused on its own: waits in the queue, as "paused", until resumed
}

// LayoutShare: see Job.Layout.
const LayoutShare = "share"

func now() float64 { return float64(time.Now().UnixNano()) / 1e9 }

func newJob(link, name, category string) *Job {
	j := &Job{ID: "usnf_" + RandomID(6), Link: strings.TrimSpace(link), Name: name, NameAuto: name == "", Category: category,
		State: "queued", Added: now(), Layout: LayoutShare}
	if j.Category == "" {
		j.Category = "Default"
	}
	if j.Name == "" {
		if l, err := share.ParseLink(j.Link); err == nil {
			j.Name = "USN_" + l.FolderID[:min(12, len(l.FolderID))]
		} else {
			j.Name = "USN_" + RandomID(4)
		}
	}
	return j
}

type persistedJob struct {
	*Job
	Password        string `json:"password"`
	ArchivePassword string `json:"archive_password"`
	DoneCount       int64  `json:"done_count"`
}

func (j *Job) persisted() persistedJob {
	return persistedJob{Job: j, Password: Seal(j.Password), ArchivePassword: Seal(j.ArchivePassword), DoneCount: j.doneCount}
}

func restoreJob(raw []byte) (*Job, error) {
	var j Job
	if err := json.Unmarshal(raw, &j); err != nil {
		// file_sel was a plain string in very old versions
		var m map[string]any
		if json.Unmarshal(raw, &m) != nil {
			return nil, err
		}
		if s, ok := m["file_sel"].(string); ok {
			m["file_sel"] = []string{s}
		}
		b, _ := json.Marshal(m)
		if err := json.Unmarshal(b, &j); err != nil {
			return nil, err
		}
	}
	var sec struct {
		Password        *string `json:"password"`
		ArchivePassword *string `json:"archive_password"`
		NzoID           string  `json:"nzo_id"`
	}
	_ = json.Unmarshal(raw, &sec)
	if sec.Password != nil {
		j.Password = Unseal(*sec.Password)
	}
	if sec.ArchivePassword != nil {
		j.ArchivePassword = Unseal(*sec.ArchivePassword)
	}
	if j.ID == "" {
		j.ID = sec.NzoID
	}
	if j.ID == "" || j.Link == "" {
		return nil, errors.New("job without id or link")
	}
	if j.Category == "" {
		j.Category = "Default"
	}
	if runningStates[j.State] {
		j.State, j.Detail = "queued", "resumed after restart"
	}
	return &j, nil
}

// Wants: is this share path part of the job's selection?
func (j *Job) Wants(path string) bool {
	if j.FileSel == nil {
		return true
	}
	if j.exact == nil {
		j.exact = map[string]bool{}
		for _, s := range j.FileSel {
			if strings.HasSuffix(s, "/") {
				j.prefixes = append(j.prefixes, s)
			} else {
				j.exact[s] = true
			}
		}
	}
	if j.exact[path] {
		return true
	}
	p := strings.ReplaceAll(path, "\\", "/")
	for _, pre := range j.prefixes {
		if strings.HasPrefix(p, pre) {
			return true
		}
	}
	return false
}

// JobView is what the API shows of a job.
type JobView struct {
	JobID       string         `json:"job_id"`
	Link        string         `json:"link"`
	Name        string         `json:"name"`
	NameAuto    bool           `json:"name_auto"`
	Category    string         `json:"category"`
	State       string         `json:"state"`
	Detail      string         `json:"detail"`
	Added       float64        `json:"added"`
	Started     *float64       `json:"started"`
	Finished    *float64       `json:"finished"`
	OutDir      string         `json:"out_dir"`
	Size        int64          `json:"size"`
	FileCount   *int           `json:"file_count"`
	Selected    *int           `json:"selected"`
	DoneCount   int64          `json:"done_count"`
	PlanOnly    bool           `json:"plan_only"`
	IndexFile   string         `json:"index_file"`
	Current     string         `json:"current"`
	FilePos     [2]int         `json:"file_pos"`
	Phase       string         `json:"phase"`
	Bytes       int64          `json:"bytes"`
	Rate        float64        `json:"rate"`
	ETA         *float64       `json:"eta"`
	Missing     int64          `json:"missing"`
	ConnErrors  int64          `json:"conn_errors"`
	Files       []FileInfo     `json:"files,omitempty"`
	FileSel     []string       `json:"file_sel,omitempty"`
	Result      map[string]any `json:"result,omitempty"`
	ExtractMode string         `json:"extract_mode,omitempty"`
	Warnings    []string       `json:"warnings,omitempty"`
	Picked      string         `json:"picked,omitempty"`
}

func (j *Job) View(full bool) JobView {
	j.mu.Lock()
	defer j.mu.Unlock()
	v := JobView{JobID: j.ID, Link: j.Link, Name: j.Name, NameAuto: j.NameAuto, Category: j.Category, State: j.State, Detail: j.Detail,
		Added: j.Added, Started: j.Started, Finished: j.Finished, OutDir: j.OutDir, Size: j.Size, DoneCount: j.doneCount,
		PlanOnly: j.PlanOnly, IndexFile: j.IndexFile, Current: j.current, FilePos: j.filePos, Phase: j.State, Bytes: j.bytesPrev,
		ExtractMode: j.ExtractMode, Warnings: j.Warnings, Picked: j.Picked}
	if j.FileCount > 0 || j.Files != nil {
		n := max(j.FileCount, len(j.Files))
		v.FileCount = &n
	}
	if j.FileSel != nil {
		n := len(j.FileSel)
		v.Selected = &n
	}
	if j.task != nil {
		if st, phase := j.task.Progress(); st != nil {
			v.Bytes = j.bytesPrev + st.Bytes()
			v.Rate = st.Rate()
			v.Missing, v.ConnErrors, v.Phase = st.Missing.Load(), st.Errors.Load(), phase
			if v.Rate > 0 {
				eta := float64(j.Size-v.Bytes) / v.Rate
				v.ETA = &eta
			}
		}
	} else if j.State == "completed" {
		v.Bytes = j.Size
	}
	if full {
		v.Files, v.FileSel, v.Result = j.Files, j.FileSel, j.Result
	}
	return v
}

// ---------------------------------------------------------------------------
// manager
// ---------------------------------------------------------------------------

// Manager runs the job queue.
type Manager struct {
	DataDir      string
	SettingsPath string
	Settings     *Settings
	Store        *Store
	mu           sync.Mutex
	jobs         map[string]*Job
	order        []string
	logMu        sync.Mutex
	logLines     []string
	logSeq       int64
	shuttingDown atomic.Bool
	pinned       map[string]int
	lock         *InstanceLock
	stopSched    chan struct{}
	kick         chan struct{}
	paused       atomic.Bool
	throttle     atomic.Pointer[Throttle]
	diskLow      atomic.Pointer[diskLow] // downloads paused for lack of space (throttle.go)
	// Notify shows a notification on this machine (set in tray mode): a download finished or
	// failed, the disk filled up. warn: the warning style.
	Notify func(title, body string, warn bool)
	tidy   tidyRun // upkeep of the index cache (compress.go)
}

// NewManager opens the data folder (locking it), imports the previous version's files
// once, sweeps the index cache and (start) runs the scheduler.
func NewManager(dataDir string, start bool) (*Manager, error) {
	m := &Manager{DataDir: dataDir, SettingsPath: filepath.Join(dataDir, "usnfetch_gui.json"), jobs: map[string]*Job{},
		pinned: map[string]int{}, stopSched: make(chan struct{}), kick: make(chan struct{}, 1)}
	if start {
		l, err := Lock(filepath.Join(dataDir, "usnfetch_gui_state.json.lock"))
		if err != nil {
			return nil, err
		}
		m.lock = l
	}
	s, err := LoadSettings(m.SettingsPath)
	if err != nil {
		m.lock.Release()
		return nil, err
	}
	m.Settings = s
	if m.Store, err = OpenStore(filepath.Join(dataDir, "usnfetch.db")); err != nil {
		m.lock.Release()
		return nil, err
	}
	if err := m.Store.Migrate(dataDir, m.Log); err != nil {
		m.Log("import of the previous version's files failed: " + err.Error())
	}
	jobs, err := m.Store.LoadJobs()
	if err != nil {
		return nil, err
	}
	for _, j := range jobs {
		m.jobs[j.ID] = j
		m.order = append(m.order, j.ID)
	}
	m.applyThrottle()
	if start {
		go m.governor()
		go m.scheduler()
		m.TidyIndexCache() // convert, cut down, zip and compress what nothing is using
	}
	return m, nil
}

func (m *Manager) Log(msg string) {
	line := time.Now().Format("[15:04:05] ") + msg
	m.logMu.Lock()
	m.logLines = append(m.logLines, line)
	if len(m.logLines) > 2000 {
		m.logLines = m.logLines[len(m.logLines)-2000:]
	}
	m.logSeq++
	m.logMu.Unlock()
	fmt.Fprintln(os.Stderr, line)
}

// Logs returns the current sequence number and the lines after `after`.
func (m *Manager) Logs(after int64) (int64, []string) {
	m.logMu.Lock()
	defer m.logMu.Unlock()
	n := int64(len(m.logLines))
	if after > 0 {
		n = min(max(0, m.logSeq-after), int64(len(m.logLines)))
	} else {
		n = min(n, 500)
	}
	return m.logSeq, append([]string{}, m.logLines[int64(len(m.logLines))-n:]...)
}

func (m *Manager) SaveSettings() error { return SaveSettings(m.Settings, m.SettingsPath) }

func (m *Manager) SaveState() {
	m.mu.Lock()
	jobs := make([]*Job, 0, len(m.order))
	for _, id := range m.order {
		jobs = append(jobs, m.jobs[id])
	}
	m.mu.Unlock()
	if err := m.Store.SaveJobs(jobs); err != nil {
		m.Log("saving job state failed: " + err.Error())
	}
}

func (m *Manager) IndexDir() string {
	if m.Settings.IndexDir != "" {
		return m.Settings.IndexDir
	}
	return filepath.Join(m.DataDir, "index_cache")
}

// Accounts: the enabled accounts, ordered by priority.
func (m *Manager) Accounts() (*engine.AccountSet, error) {
	var accts []*nntp.Account
	for _, a := range m.Settings.Accounts {
		if a.Enabled && strings.TrimSpace(a.Server) != "" {
			accts = append(accts, AccountFrom(a))
		}
	}
	if len(accts) == 0 {
		if len(m.Settings.Accounts) > 0 {
			return nil, errors.New("every Usenet provider is disabled (Settings → Usenet providers)")
		}
		return nil, errors.New("no Usenet provider yet: add one in Settings → Usenet providers")
	}
	return engine.NewAccountSet(accts)
}

func AccountFrom(a AccountSetting) *nntp.Account {
	port := a.Port
	if port == 0 {
		port = 563
	}
	return &nntp.Account{Name: a.Name, Server: strings.TrimSpace(a.Server), Port: port, SSL: a.SSL, User: a.Username,
		Password: a.Password, Connections: max(1, a.Connections), Priority: max(0, a.Priority)}
}

// ---- index cache (upkeep in compress.go) ----

func (m *Manager) PinIndex(path string) {
	m.mu.Lock()
	m.pinned[strings.ToLower(path)]++
	m.mu.Unlock()
}

func (m *Manager) UnpinIndex(path string) {
	m.mu.Lock()
	k := strings.ToLower(path)
	if m.pinned[k] > 1 {
		m.pinned[k]--
	} else {
		delete(m.pinned, k)
	}
	m.mu.Unlock()
}

// dropIndex: the job is done with its index. It stays in the cache (browsing the share, or
// downloading more of it, reuses it), which keeps the most recently used within its limit.
func (m *Manager) dropIndex(j *Job) {
	if j.IndexFile == "" {
		return
	}
	j.IndexFile = ""
	m.TidyIndexCache()
}

// ---- queue operations ----

func (m *Manager) Add(link, name, category, password, archivePassword string, planOnly bool, fileSel []string) (*Job, error) {
	return m.AddJob(link, AddOptions{Name: name, Category: category, Password: password, ArchivePassword: archivePassword,
		PlanOnly: planOnly, FileSel: fileSel})
}

// AddOptions for AddJob.
type AddOptions struct {
	Name, Category            string
	Password, ArchivePassword string
	PlanOnly                  bool
	FileSel                   []string // nil = the whole share
	ExtractMode               string   // "" = the settings' default
	Existing                  string   // "" (skip same-size files), ExistingVerify, ExistingRedownload
	Picked                    string   // what of the share this is, for telling jobs apart (see Job.Picked)
	Size                      int64    // the planned size, shown while it waits (0 = not known yet)
}

func sameSel(a, b []string) bool {
	if (a == nil) != (b == nil) || len(a) != len(b) {
		return false
	}
	x, y := append([]string{}, a...), append([]string{}, b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// AddJob queues a download. The same selection of a share that is already queued or
// running is not queued twice; a different selection of it is a job of its own (the
// scheduler runs one job per share at a time, so they never write the same folder at once).
func (m *Manager) AddJob(link string, o AddOptions) (*Job, error) {
	link = strings.TrimSpace(link)
	if _, err := share.ParseLink(link); err != nil {
		return nil, err
	}
	if !ValidExtractMode(o.ExtractMode) {
		return nil, fmt.Errorf("unknown extract mode %q", o.ExtractMode)
	}
	m.mu.Lock()
	for _, id := range m.order {
		if j := m.jobs[id]; j.Link == link && activeStates[j.State] && sameSel(j.FileSel, o.FileSel) {
			m.mu.Unlock()
			m.Log(fmt.Sprintf("not adding duplicate of %s (%s) for the same link and selection", j.ID, j.State))
			return j, nil
		}
	}
	j := newJob(link, o.Name, o.Category)
	if o.Existing != "" && o.Existing != ExistingSkip && o.Existing != ExistingVerify && o.Existing != ExistingRedownload {
		m.mu.Unlock()
		return nil, fmt.Errorf("unknown handling %q for files already on disk", o.Existing)
	}
	if o.Existing == ExistingSkip {
		o.Existing = ""
	}
	j.Password, j.ArchivePassword, j.PlanOnly, j.ExtractMode, j.Existing = o.Password, o.ArchivePassword, o.PlanOnly, o.ExtractMode, o.Existing
	j.Picked, j.Size = o.Picked, max(0, o.Size)
	if o.FileSel != nil {
		j.FileSel = append([]string{}, o.FileSel...)
	}
	m.jobs[j.ID] = j
	m.order = append(m.order, j.ID)
	m.mu.Unlock()
	m.SaveState()
	m.Log(fmt.Sprintf("queued %s (%s)", j.Label(), j.ID))
	return j, nil
}

// jobLog is the log for one job's downloads: lines say which job they are about, and
// the progress lines (every couple of seconds from the engine) are kept to one in 30 s.
func (m *Manager) jobLog(j *Job) func(string) {
	var last time.Time
	var mu sync.Mutex
	return func(s string) {
		if progressLine.MatchString(s) {
			mu.Lock()
			skip := time.Since(last) < 30*time.Second
			if !skip {
				last = time.Now()
			}
			mu.Unlock()
			if skip {
				return
			}
		}
		m.Log(j.Label() + ": " + strings.TrimSpace(s))
	}
}

var progressLine = regexp.MustCompile(`^\s*\d+\.\d+%`)

// Label is the job as people see it: its name, and what of the share it takes.
func (j *Job) Label() string {
	if j.Picked != "" {
		return j.Name + " — " + j.Picked
	}
	return j.Name
}

func (m *Manager) Get(id string) *Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.jobs[id]
}

func (m *Manager) Stop(id string) bool {
	j := m.Get(id)
	if j == nil {
		return false
	}
	j.mu.Lock()
	defer func() { m.SaveState() }()
	defer j.mu.Unlock()
	switch {
	case runningStates[j.State]:
		j.cancel.Store(true)
		if j.task != nil {
			j.task.Stop()
		}
		j.State = "stopping"
		return true
	case j.State == "queued" || j.State == "paused":
		j.State, j.Detail = "stopped", "stopped before start"
		return true
	}
	return false
}

// PauseJob holds one job in the queue: a running one stops (keeping what it has) and
// waits as "paused", like a queued one, until it is resumed.
func (m *Manager) PauseJob(id string) bool {
	j := m.Get(id)
	if j == nil {
		return false
	}
	j.mu.Lock()
	defer func() { m.SaveState() }()
	defer j.mu.Unlock()
	switch {
	case runningStates[j.State] && j.State != "stopping":
		j.holdStop = true
		j.cancel.Store(true)
		if j.task != nil {
			j.task.Stop()
		}
		j.State = "stopping"
		return true
	case j.State == "queued":
		j.State, j.Detail = "paused", "paused"
		return true
	}
	return false
}

func (m *Manager) Resume(id string, redownload bool) bool {
	j := m.Get(id)
	if j != nil && j.State == "paused" {
		j.mu.Lock()
		j.State, j.Detail = "queued", ""
		j.cancel.Store(false)
		j.mu.Unlock()
		m.SaveState()
		return true
	}
	if j == nil || !finishedStates[j.State] {
		return false
	}
	if redownload {
		_ = m.Store.ResetDone(j.ID)
		j.doneCount = 0
	}
	j.mu.Lock()
	j.State, j.Detail, j.PlanOnly = "queued", "", false
	j.cancel.Store(false)
	j.mu.Unlock()
	m.SaveState()
	return true
}

func (m *Manager) SetFiles(id string, paths []string) bool {
	j := m.Get(id)
	if j == nil || runningStates[j.State] {
		return false
	}
	j.mu.Lock()
	j.FileSel, j.exact, j.prefixes = nil, nil, nil
	if paths != nil {
		j.FileSel = append([]string{}, paths...)
	}
	j.mu.Unlock()
	m.SaveState()
	return true
}

func (m *Manager) Delete(id string, removeFiles bool) bool {
	m.mu.Lock()
	j := m.jobs[id]
	if j == nil {
		m.mu.Unlock()
		return false
	}
	delete(m.jobs, id)
	for i, o := range m.order {
		if o == id {
			m.order = append(m.order[:i:i], m.order[i+1:]...)
			break
		}
	}
	m.mu.Unlock()
	j.mu.Lock()
	j.deleted = true
	j.cancel.Store(true)
	running := j.task != nil || runningStates[j.State]
	if j.task != nil {
		j.task.Stop()
	}
	j.removeFiles = removeFiles
	j.mu.Unlock()
	if !running {
		if removeFiles && j.OutDir != "" {
			m.removeDir(j)
		}
		m.dropIndex(j)
	}
	m.SaveState() // also deletes its done records
	return true
}

func (m *Manager) removeDir(j *Job) {
	root, _ := filepath.Abs(m.Settings.CompleteDir)
	d, _ := filepath.Abs(j.OutDir)
	if !strings.HasPrefix(strings.ToLower(d), strings.ToLower(root)+string(os.PathSeparator)) {
		m.Log("refusing to delete " + d + ": not under the download folder")
		return
	}
	_ = os.RemoveAll(d)
	m.Log("deleted " + d)
}

// Shutdown stops running jobs so their checkpoints flush; they re-queue on next start.
func (m *Manager) Shutdown(timeout time.Duration) {
	m.shuttingDown.Store(true)
	close(m.stopSched)
	m.mu.Lock()
	var running []*Job
	for _, j := range m.jobs {
		if runningStates[j.State] {
			running = append(running, j)
		}
	}
	m.mu.Unlock()
	for _, j := range running {
		j.cancel.Store(true)
		j.mu.Lock()
		if j.task != nil {
			j.task.Stop()
		}
		j.mu.Unlock()
	}
	end := time.Now().Add(timeout)
	for time.Now().Before(end) {
		busy := false
		for _, j := range running {
			if runningStates[j.State] {
				busy = true
			}
		}
		if !busy {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	m.SaveState()
	m.Store.Close()
	m.lock.Release()
}

func (m *Manager) Queue() []JobView   { return m.views(true) }
func (m *Manager) History() []JobView { return m.views(false) } // newest first

func (m *Manager) views(active bool) []JobView {
	m.mu.Lock()
	jobs := make([]*Job, 0, len(m.order))
	for _, id := range m.order {
		jobs = append(jobs, m.jobs[id])
	}
	m.mu.Unlock()
	var out []JobView
	if active {
		for _, j := range jobs {
			if activeStates[j.State] {
				out = append(out, j.View(false))
			}
		}
	} else {
		for i := len(jobs) - 1; i >= 0; i-- {
			if finishedStates[jobs[i].State] {
				out = append(out, jobs[i].View(false))
			}
		}
	}
	if out == nil {
		out = []JobView{}
	}
	return out
}

// ---- running jobs ----

func (m *Manager) scheduler() {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-m.stopSched:
			return
		case <-t.C:
		}
		if m.paused.Load() {
			continue
		}
		m.mu.Lock()
		running := 0
		for _, j := range m.jobs {
			if runningStates[j.State] {
				running++
			}
		}
		var next *Job
		if running < max(1, m.Settings.MaxParallelJobs) {
			busy := map[string]bool{} // shares with a running job: their jobs share a folder
			for _, j := range m.jobs {
				if runningStates[j.State] {
					busy[j.Link] = true
				}
			}
			for _, id := range m.order {
				if j := m.jobs[id]; j.State == "queued" && !busy[j.Link] {
					next = j
					break
				}
			}
		}
		if next != nil {
			s := now()
			next.mu.Lock()
			next.State, next.Started, next.Detail, next.bytesPrev, next.current = "planning", &s, "", 0, ""
			next.Warnings = nil
			next.cancel.Store(false)
			next.mu.Unlock()
		}
		m.mu.Unlock()
		if next != nil {
			go m.run(next)
		}
	}
}

func (m *Manager) finish(j *Job, state, detail string) {
	j.mu.Lock()
	if j.cancel.Load() && state != "completed" {
		if m.shuttingDown.Load() {
			state, detail = "queued", "resumes on next start"
			if j.holdStop {
				state, detail = "paused", "paused"
			}
		} else if j.holdStop && !j.deleted {
			state, detail = "paused", "paused: carries on from where it was when resumed"
		} else if j.pauseStop && !j.deleted {
			state, detail = "queued", "paused"
		} else if !j.deleted {
			if state != "stopped" {
				detail = "stopped (resumable)"
			}
			state = "stopped"
		}
	}
	t := now()
	j.pauseStop, j.holdStop = false, false
	j.State, j.Detail, j.Finished, j.task, j.current = state, detail, &t, nil, ""
	deleted := j.deleted
	j.mu.Unlock()
	if !deleted {
		m.SaveState()
	}
	msg := fmt.Sprintf("%s: %s", j.Label(), state)
	if detail != "" {
		msg += " -- " + detail
	}
	if !deleted {
		switch state {
		case "completed":
			m.notify("Download finished", j.Label(), false)
		case "failed":
			m.notify("Download failed", j.Label()+": "+detail, true)
		}
	}
	msg += " (" + j.ID + ")"
	m.Log(msg)
}

func (m *Manager) run(j *Job) {
	defer func() {
		if r := recover(); r != nil {
			m.Log(fmt.Sprintf("%v\n%s", r, debug.Stack()))
			m.finish(j, "failed", fmt.Sprint(r))
		}
		if j.deleted {
			m.dropIndex(j)
			if j.removeFiles && j.OutDir != "" {
				m.removeDir(j)
			}
		}
	}()
	accts, err := m.Accounts()
	if err != nil {
		m.finish(j, "failed", "RuntimeError: "+err.Error())
		return
	}
	ix, err := share.LoadShare(j.Link, share.LoadOpts{Accounts: accts.All(), Password: j.Password, IndexDir: m.IndexDir(), Log: m.Log})
	if err != nil {
		if errors.Is(err, share.ErrPassword) {
			m.finish(j, "failed", "this share is password protected: set the share password on the job")
		} else {
			m.finish(j, "failed", err.Error())
		}
		return
	}
	j.mu.Lock()
	j.IndexFile = ix.Path
	j.mu.Unlock()
	state, detail := m.download(j, ix, accts)
	ix.Close()
	ReleaseMemory() // a big share's index build or file list: give it back now, not over minutes
	if state == "completed" {
		m.dropIndex(j)
	}
	m.finish(j, state, detail)
}

func safeName(part string) string {
	part = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"|?*`, r) || r < 0x20 {
			return '_'
		}
		return r
	}, part)
	part = strings.TrimRight(part, " .")
	if part == "" {
		part = "_"
	}
	base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
	switch base {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return "_" + part
	}
	return part
}

// SafeRelPath: a share-supplied path as a relative path that cannot leave the job folder.
func SafeRelPath(p string) string {
	var parts []string
	for _, x := range pathSep.Split(p, -1) {
		if x != "" && x != "." && x != ".." {
			parts = append(parts, safeName(x))
		}
	}
	if len(parts) == 0 {
		return "file.bin"
	}
	return filepath.Join(parts...)
}

var pathSep = regexp.MustCompile(`[\\/]+`)

// JobOutDir: where a job's files land.
func JobOutDir(s *Settings, category, name string) string {
	if category == "" {
		category = "Default"
	}
	return filepath.Join(s.CompleteDir, safeName(category), safeName(name))
}

// StripTop drops the folder top (if the share path is inside it) from a share path.
func StripTop(p, top string) string {
	rel := strings.Trim(strings.ReplaceAll(p, "\\", "/"), "/")
	if top != "" && strings.HasPrefix(rel, top+"/") {
		rel = rel[len(top)+1:]
	}
	return rel
}

func commonTop(paths []string) string {
	first := ""
	for i, p := range paths {
		s := pathSep.Split(strings.Trim(p, `/\`), -1)
		if len(s) < 2 {
			return ""
		}
		if i == 0 {
			first = s[0]
		} else if s[0] != first {
			return ""
		}
	}
	return first
}

// topFolder is commonTop worked out one path at a time, without keeping the paths.
type topFolder struct {
	first string
	n     int
	none  bool
}

func (t *topFolder) add(p string) {
	if t.none {
		return
	}
	s := strings.Trim(p, `/\`)
	i := strings.IndexAny(s, `/\`)
	if i < 0 { // a file at the top: no common folder
		t.none = true
		return
	}
	if t.n == 0 {
		t.first = s[:i]
	} else if s[:i] != t.first {
		t.none = true
	}
	t.n++
}

func (t *topFolder) top() string {
	if t.none {
		return ""
	}
	return t.first
}

func (m *Manager) setTask(j *Job, t engine.Task) {
	j.mu.Lock()
	j.task = t
	j.mu.Unlock()
}

// download plans, downloads + verifies the selected files and extracts archives.
func (m *Manager) download(j *Job, ix *share.Index, accts *engine.AccountSet) (string, string) {
	s := m.Settings
	j.mu.Lock()
	planOnly := j.PlanOnly
	j.mu.Unlock()
	// One pass over the share's files that keeps only what the job needs: how many there
	// are, a listing for small shares, the picked files and the share's top folder.
	// (Loading a share of 2.7 million files whole took 1-2 GB for as long as the job ran.)
	var (
		count    int
		listing  = []FileInfo{} // nil once there are more than filesKeep
		chosen   []share.FileEntry
		paths    []string
		shareTop topFolder
	)
	err := ix.EachFile(func(f share.FileEntry) error {
		if !f.Downloadable() {
			return nil
		}
		count++
		if listing != nil {
			if count > filesKeep {
				listing = nil
			} else {
				listing = append(listing, FileInfo{f.Path, f.Size, f.SegCount, f.Packed()})
			}
		}
		shareTop.add(f.Path)
		if !planOnly && j.Wants(f.Path) {
			chosen = append(chosen, f)
			paths = append(paths, f.Path)
		}
		return nil
	})
	if err != nil {
		return "failed", err.Error()
	}
	j.mu.Lock()
	j.FileCount, j.Files, j.PlanOnly = count, listing, false
	j.mu.Unlock()
	if planOnly {
		return "stopped", fmt.Sprintf("%d file(s) listed: choose files, then Resume", count)
	}
	if len(chosen) == 0 {
		return "failed", "no files selected"
	}
	// the folder the job folder stands in for (see Job.Layout)
	top := commonTop(paths)
	if j.Layout == LayoutShare {
		top = shareTop.top()
	}
	j.mu.Lock()
	if j.NameAuto && top != "" {
		j.Name, j.NameAuto = safeName(top), false
	}
	j.OutDir = JobOutDir(s, j.Category, j.Name)
	j.Size = 0
	for _, f := range chosen {
		j.Size += f.Size
	}
	j.mu.Unlock()
	m.mu.Lock()
	for _, o := range m.jobs {
		if o != j && o.Link != j.Link && o.OutDir != "" && strings.EqualFold(o.OutDir, j.OutDir) {
			lnk, _ := share.ParseLink(j.Link)
			j.Name = fmt.Sprintf("%s [%s]", j.Name, lnk.FolderID[:8])
			j.OutDir = filepath.Join(s.CompleteDir, safeName(j.Category), safeName(j.Name))
			break
		}
	}
	var clash *Job
	for _, o := range m.jobs {
		if o != j && runningStates[o.State] && o.OutDir != "" && strings.EqualFold(o.OutDir, j.OutDir) {
			clash = o
		}
	}
	m.mu.Unlock()
	if clash != nil {
		return "failed", "output folder in use by running job " + clash.ID
	}
	if err := os.MkdirAll(j.OutDir, 0o755); err != nil {
		return "failed", err.Error()
	}
	if j.cancel.Load() {
		return "stopped", "stopped while planning"
	}
	outPath := func(f share.FileEntry) string {
		return filepath.Join(j.OutDir, SafeRelPath(StripTop(f.Path, top)))
	}
	// files already in place (an earlier download, a copy): checked, then counted as done
	existing := map[string]bool{}
	if j.Existing != ExistingRedownload {
		n, bytes, err := m.skipExisting(j, chosen, outPath, j.Existing == ExistingVerify, existing)
		if err != nil {
			return "stopped", "stopped while checking files already on disk"
		}
		if n > 0 {
			m.Log(fmt.Sprintf("%s: %d file(s), %.2f GB already on disk, not downloaded again", j.Label(), n, float64(bytes)/1e9))
		}
	}
	// outputs are sparse, so a full disk would otherwise only show up mid-download
	var need int64
	for _, f := range chosen {
		if !m.Store.IsDone(j.ID, f.Path) {
			need += f.Size
		}
	}
	if free, err := DiskFree(j.OutDir); err == nil && need > free {
		msg := fmt.Sprintf("needs %.2f GiB, only %.2f GiB free in %s", float64(need)/(1<<30), float64(free)/(1<<30), j.OutDir)
		partial := false
		_ = filepath.WalkDir(j.OutDir, func(p string, d os.DirEntry, err error) error {
			if err == nil && strings.HasSuffix(p, ".usnfetch") {
				partial = true
				return filepath.SkipAll
			}
			return nil
		})
		if !partial {
			return "failed", msg
		}
		m.Log("warning: " + msg + " (partial files already on disk, continuing)")
	}
	markDone := func(path string) {
		if added, err := m.Store.MarkDone(j.ID, path); err == nil && added {
			j.mu.Lock()
			j.doneCount++
			j.mu.Unlock()
		}
	}
	j.mu.Lock()
	j.State = "downloading"
	j.mu.Unlock()
	m.SaveState()
	pool := engine.NewPool(accts)
	pool.Bulk = true // all running downloads share each provider's connections, and give way to previews and checks
	defer pool.Close()
	jlog := m.jobLog(j)
	type bad struct{ name, why string }
	var bads []bad
	results := map[string]map[string]any{}
	segN := 0
	var segmented, whole []share.FileEntry
	for _, f := range chosen {
		if f.SegCount > 0 {
			segmented = append(segmented, f)
		} else {
			whole = append(whole, f)
		}
	}
	for i, f := range segmented {
		j.mu.Lock()
		j.filePos = [2]int{i + 1, len(chosen)}
		j.mu.Unlock()
		if m.Store.IsDone(j.ID, f.Path) {
			j.bytesPrev += f.Size
			continue
		}
		if j.cancel.Load() {
			break
		}
		out := outPath(f)
		_ = os.MkdirAll(filepath.Dir(out), 0o755)
		rel, _ := filepath.Rel(j.OutDir, out)
		j.mu.Lock()
		j.current = rel
		j.mu.Unlock()
		plan, err := ix.Plan(f)
		if err != nil {
			bads = append(bads, bad{rel, err.Error()})
			continue
		}
		eng, err := engine.NewEngine(plan, out, pool, jlog)
		if err != nil {
			bads = append(bads, bad{rel, err.Error()})
			continue
		}
		eng.MissingRetries, eng.Verify = s.MissingRetries, s.Verify
		m.setTask(j, eng)
		if j.cancel.Load() {
			eng.Stop()
		}
		res := eng.Run()
		m.setTask(j, nil)
		segN++
		r := map[string]any{"done": res.Done, "segments": res.Segments, "missing": res.Missing, "avg_MiBps": res.AvgMiBps}
		if res.SHA256OK != nil {
			r["sha256_ok"] = *res.SHA256OK
		}
		ok := res.Complete && (res.SHA256OK == nil || *res.SHA256OK)
		if !ok && len(results) < 200 {
			results[f.Path] = r
		}
		if ok {
			markDone(f.Path)
			j.bytesPrev += f.Size
			m.SaveState()
		} else if !eng.Stopped() {
			why := "incomplete"
			switch {
			case len(res.Missing) > 0:
				why = fmt.Sprintf("%d segment(s) missing on every copy", len(res.Missing))
			case len(res.Failed) > 0:
				why = fmt.Sprintf("%d segment(s) failed decrypt", len(res.Failed))
			case res.SHA256OK != nil && !*res.SHA256OK:
				why = "sha256 mismatch"
			}
			bads = append(bads, bad{rel, why})
		}
		if eng.Stopped() {
			break
		}
	}
	var packedRes *engine.PackResult
	if !j.cancel.Load() {
		var todo []share.FileEntry
		for _, f := range whole {
			if m.Store.IsDone(j.ID, f.Path) {
				j.bytesPrev += f.Size
			} else {
				todo = append(todo, f)
			}
		}
		var wanted []engine.Wanted
		for _, f := range todo {
			if f.Pack == nil { // 0-byte file: nothing to fetch
				if err := engine.WriteWhole(outPath(f), nil); err == nil {
					markDone(f.Path)
				}
			} else {
				wanted = append(wanted, engine.Wanted{File: f, Out: outPath(f)})
			}
		}
		if len(wanted) > 0 {
			j.mu.Lock()
			j.filePos = [2]int{len(segmented) + 1, len(chosen)}
			j.current = fmt.Sprintf("%d small file(s)", len(wanted))
			j.mu.Unlock()
			pf, err := engine.NewPackFetcher(ix, wanted, pool, jlog)
			if err != nil {
				return "failed", err.Error()
			}
			pf.MissingRetries = s.MissingRetries
			last := time.Now()
			pf.OnFile = func(fe share.FileEntry, out string) {
				markDone(fe.Path)
				if time.Since(last) > 5*time.Second {
					last = time.Now()
					m.SaveState()
				}
			}
			m.setTask(j, pf)
			if j.cancel.Load() {
				pf.Stop()
			}
			r := pf.Run()
			packedRes = &r
			m.setTask(j, nil)
			landed := map[string]bool{}
			for _, p := range r.Done {
				landed[p] = true
			}
			for _, w := range wanted {
				if landed[w.File.Path] {
					j.bytesPrev += w.File.Size
				}
			}
			m.SaveState()
			if !pf.Stopped() {
				keys := func(mm map[string]string) []string {
					var ks []string
					for k := range mm {
						ks = append(ks, k)
					}
					sort.Strings(ks)
					return ks
				}
				for _, p := range keys(r.Missing) {
					bads = append(bads, bad{p, r.Missing[p]})
				}
				for _, p := range keys(r.Failed) {
					bads = append(bads, bad{p, r.Failed[p]})
				}
			}
		}
	}
	res := map[string]any{"files": results, "segmented_files": segN}
	if packedRes != nil {
		res["packed"] = map[string]any{"files": packedRes.Files, "done": len(packedRes.Done), "missing": len(packedRes.Missing), "failed": len(packedRes.Failed)}
	}
	j.mu.Lock()
	j.Result = res
	j.mu.Unlock()
	if j.cancel.Load() {
		done := 0
		for _, f := range chosen {
			if m.Store.IsDone(j.ID, f.Path) {
				done++
			}
		}
		return "stopped", fmt.Sprintf("stopped: %d/%d file(s) done (resumable)", done, len(chosen))
	}
	if len(bads) > 0 {
		return "failed", fmt.Sprintf("%d file(s) incomplete; %s: %s", len(bads), bads[0].name, bads[0].why)
	}
	mode := j.ExtractMode
	if mode == "" {
		mode = s.DefaultExtractMode()
	}
	if mode == ExtractOff {
		if len(existing) > 0 {
			return "completed", fmt.Sprintf("%d file(s) (%d already on disk)", len(chosen), len(existing))
		}
		return "completed", fmt.Sprintf("%d file(s)", len(chosen))
	}
	j.mu.Lock()
	j.State, j.current = "extracting", ""
	j.mu.Unlock()
	pw := j.ArchivePassword
	if pw == "" {
		pw = j.Password
	}
	// only this job's files: other downloads of the same share share the folder
	var written []string
	for _, f := range chosen {
		if extract.LooksLikeVolume(f.Path) { // files already on disk too: the selection asked for extraction
			written = append(written, outPath(f))
		}
	}
	xr := extract.Extract(j.OutDir, m.Log, extract.Options{Password: pw, DeleteArchives: s.DeleteArchives, SevenZip: s.SevenZipPath,
		Unrar: s.UnrarPath, Standalone: mode == ExtractAll, Only: append([]string{}, written...)})
	j.mu.Lock()
	j.Result["extract"] = xr
	j.mu.Unlock()
	detail := fmt.Sprintf("%d file(s)", len(chosen))
	if len(existing) > 0 {
		detail += fmt.Sprintf(" (%d already on disk)", len(existing))
	}
	switch {
	case len(xr.OK) > 0:
		detail += fmt.Sprintf(", %d archive set(s) extracted", len(xr.OK))
	case len(xr.Failed) == 0 && xr.Kept == 0:
		detail += ", no archives"
	}
	if xr.Kept > 0 {
		detail += fmt.Sprintf(", %d single .zip/.7z kept as they are", xr.Kept)
	}
	if len(xr.Failed) > 0 { // every file is downloaded and verified: the job is done, with a warning
		var names []string
		for k := range xr.Failed {
			names = append(names, k)
		}
		sort.Strings(names)
		var warn []string
		for _, n := range names {
			warn = append(warn, fmt.Sprintf("extract failed: %s: %s", n, xr.Failed[n]))
		}
		j.mu.Lock()
		j.Warnings = warn
		j.mu.Unlock()
		detail += fmt.Sprintf(", extract failed for %d set(s) (%s)", len(names), names[0])
	}
	return "completed", detail
}

// Handling of files already at a job's output path.
const (
	ExistingSkip       = "skip"       // same size, no checkpoint beside it: counted as done (the default)
	ExistingVerify     = "verify"     // ... and the SHA-256 must match the share's
	ExistingRedownload = "redownload" // download everything again
)

// skipExisting marks as done the chosen files already complete at their output path
// (one directory listing per folder; verify also hashes them). existing gets their paths.
func (m *Manager) skipExisting(j *Job, chosen []share.FileEntry, outPath func(share.FileEntry) string, verify bool, existing map[string]bool) (int, int64, error) {
	byDir := map[string][]int{}
	var dirs []string
	for i, f := range chosen {
		if m.Store.IsDone(j.ID, f.Path) {
			continue
		}
		d := filepath.Dir(outPath(f))
		if _, ok := byDir[d]; !ok {
			dirs = append(dirs, d)
		}
		byDir[d] = append(byDir[d], i)
	}
	var paths []string
	var bytes int64
	checked, total := 0, 0
	for _, d := range dirs {
		total += len(byDir[d])
	}
	for _, d := range dirs {
		if j.cancel.Load() {
			return 0, 0, errors.New("stopped")
		}
		ents, err := os.ReadDir(d)
		checked += len(byDir[d])
		if err != nil {
			continue
		}
		sizes := map[string]int64{}
		for _, e := range ents {
			if e.Type().IsRegular() {
				if info, err := e.Info(); err == nil {
					sizes[strings.ToLower(e.Name())] = info.Size()
				}
			}
		}
		for _, i := range byDir[d] {
			f := chosen[i]
			name := strings.ToLower(filepath.Base(outPath(f)))
			sz, ok := sizes[name]
			if !ok || sz != f.Size {
				continue
			}
			if _, partial := sizes[name+".usnfetch"]; partial {
				continue
			}
			if verify && f.SHA256 != "" {
				j.mu.Lock()
				j.current = "verifying " + filepath.Base(outPath(f))
				j.mu.Unlock()
				if sum, err := fileSHA256(outPath(f)); err != nil || !strings.EqualFold(sum, f.SHA256) {
					continue
				}
				if j.cancel.Load() {
					return 0, 0, errors.New("stopped")
				}
			}
			paths = append(paths, f.Path)
			existing[f.Path] = true
			bytes += f.Size
		}
		j.mu.Lock()
		if !verify {
			j.current = fmt.Sprintf("checking files already on disk: %d/%d", checked, total)
		}
		j.mu.Unlock()
	}
	if len(paths) > 0 {
		added, err := m.Store.MarkDoneMany(j.ID, paths)
		if err != nil {
			return 0, 0, err
		}
		j.mu.Lock()
		j.doneCount += added
		j.current = ""
		j.mu.Unlock()
	}
	return len(paths), bytes, nil
}

func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// IsDone (for views): whether a job finished a share path.
func (m *Manager) IsDone(jobID, path string) bool { return m.Store.IsDone(jobID, path) }

// Jobs returns every job (for catalog status).
func (m *Manager) Jobs() []*Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Job, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, m.jobs[id])
	}
	return out
}

// OutputRoot is a folder a job of a share wrote into, and the share folder it stands in
// for (dropped from share paths there; "" = none).
type OutputRoot struct {
	Dir, Strip string
}

// OutputRoots: where the jobs of a share put its files.
func (m *Manager) OutputRoots(link, shareTop string) []OutputRoot {
	var out []OutputRoot
	for _, j := range m.Jobs() {
		j.mu.Lock()
		if j.Link == link && j.OutDir != "" {
			strip := shareTop
			if j.Layout != LayoutShare {
				strip = legacyTop(j.FileSel, shareTop)
			}
			out = append(out, OutputRoot{j.OutDir, strip})
		}
		j.mu.Unlock()
	}
	return out
}

// legacyTop: the folder older versions dropped for a selection (the one its files share).
func legacyTop(sel []string, shareTop string) string {
	if sel == nil {
		return shareTop
	}
	first := ""
	for i, p := range sel {
		parts := pathSep.Split(strings.Trim(p, `/\`), -1)
		if len(parts) < 2 && !strings.HasSuffix(p, "/") {
			return ""
		}
		if i == 0 {
			first = parts[0]
		} else if parts[0] != first {
			return ""
		}
	}
	return first
}
