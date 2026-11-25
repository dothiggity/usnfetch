package app

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Settings, stored as usnfetch_gui.json (the same file and format the Python apps use).
type Settings struct {
	NNTP              *NNTPSettings    `json:"nntp,omitempty"` // older single account; moved into Accounts on load
	CompleteDir       string           `json:"complete_dir"`
	Verify            bool             `json:"verify"`
	MissingRetries    int              `json:"missing_retries"`
	MaxParallelJobs   int              `json:"max_parallel_jobs"`
	Extract           bool             `json:"extract"`
	ExtractStandalone bool             `json:"extract_standalone"` // also single .zip/.7z (off: ROM/BIOS packs stay zipped)
	DeleteArchives    bool             `json:"delete_archives"`
	SevenZipPath      string           `json:"sevenzip_path"`
	UnrarPath         string           `json:"unrar_path"`
	IndexDir          string           `json:"index_dir"`
	IndexCacheGB      int              `json:"index_cache_gb"` // full share indexes kept up to this (on disk); past it, only file lists
	MinFreeGB         int              `json:"min_free_gb"`    // downloads pause when the download drive has less free than this (0 = never)
	Notify            bool             `json:"notify"`         // Windows notifications (tray) when a download finishes or fails
	Accounts          []AccountSetting `json:"accounts"`
	StreamConnections int              `json:"stream_connections"`
	Web               WebSettings      `json:"web"`
	SetupDone         bool             `json:"setup_done"`       // the first-run setup was finished or skipped
	SpeedLimit        float64          `json:"speed_limit_mbps"` // MB/s, 0 = no limit
	Schedule          []ScheduleRule   `json:"schedule"`
	Paused            bool             `json:"paused"`        // paused by hand
	PausedUntil       float64          `json:"paused_until"`  // unix time a timed pause ends, 0 = until resumed
	ScheduleSkip      float64          `json:"schedule_skip"` // scheduled pauses are skipped until then (resumed by hand)
}

// NNTPSettings: the single account older versions kept (read once, then moved into Accounts).
type NNTPSettings struct {
	Server      string `json:"server"`
	Port        int    `json:"port"`
	SSL         bool   `json:"ssl"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	Connections int    `json:"connections"`
}

// AccountSetting is one Usenet account.
type AccountSetting struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Server      string `json:"server"`
	Port        int    `json:"port"`
	SSL         bool   `json:"ssl"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	Connections int    `json:"connections"`
	Enabled     bool   `json:"enabled"`
	Priority    int    `json:"priority"`         // 0 = highest (as in SABnzbd)
	OldBackup   bool   `json:"backup,omitempty"` // older files: a backup account; read as priority 1
}

type WebSettings struct {
	Host         string `json:"host"`
	Port         int    `json:"port"`
	PasswordHash string `json:"password_hash"`
	Secret       string `json:"secret"`
}

func defaultSettings() Settings {
	home, _ := os.UserHomeDir()
	return Settings{
		CompleteDir:       filepath.Join(home, "Downloads", "usnfetch"),
		Verify:            true,
		MissingRetries:    3,
		MaxParallelJobs:   1,
		Extract:           true,
		DeleteArchives:    true,
		Accounts:          []AccountSetting{},
		StreamConnections: 8,
		IndexCacheGB:      20,
		MinFreeGB:         5,
		Notify:            true,
		Web:               WebSettings{Host: "127.0.0.1", Port: 7979},
	}
}

func RandomID(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Extract modes of a job: what the extractor does once every file is verified.
const (
	ExtractOff  = "off"  // nothing
	ExtractSets = "sets" // RAR archives and split sets (.z01, .zip.001, .7z.001); single .zip/.7z stay as they are
	ExtractAll  = "all"  // also single .zip / .7z files
)

// DefaultExtractMode: the mode a job gets when it does not choose one.
func (s *Settings) DefaultExtractMode() string {
	switch {
	case !s.Extract:
		return ExtractOff
	case s.ExtractStandalone:
		return ExtractAll
	}
	return ExtractSets
}

// ValidExtractMode: "" (the default) or one of the modes.
func ValidExtractMode(m string) bool {
	return m == "" || m == ExtractOff || m == ExtractSets || m == ExtractAll
}

// LoadSettings reads the settings file over the defaults and unseals the passwords.
func LoadSettings(path string) (*Settings, error) {
	s := defaultSettings()
	raw, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(raw, &s)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	for i := range s.Accounts {
		a := &s.Accounts[i]
		a.Password = Unseal(a.Password)
		if a.ID == "" {
			a.ID = RandomID(6)
		}
		if a.OldBackup && a.Priority == 0 {
			a.Priority = 1
		}
		a.OldBackup = false
	}
	if s.Accounts == nil {
		s.Accounts = []AccountSetting{}
	}
	if n := s.NNTP; n != nil { // the older single account becomes the first entry of the list
		if len(s.Accounts) == 0 && strings.TrimSpace(n.Server) != "" {
			port := n.Port
			if port == 0 {
				port = 563
			}
			s.Accounts = append(s.Accounts, AccountSetting{ID: RandomID(6), Name: strings.TrimSpace(n.Server), Server: strings.TrimSpace(n.Server),
				Port: port, SSL: n.SSL, Username: n.Username, Password: Unseal(n.Password), Connections: max(1, n.Connections), Enabled: true})
		}
		s.NNTP = nil
	}
	if len(s.Accounts) > 0 {
		s.SetupDone = true // set up before the wizard existed
	}
	if s.Schedule == nil {
		s.Schedule = []ScheduleRule{}
	}
	if s.Web.Port == 0 {
		s.Web.Port = 7979
	}
	if s.Web.Host == "" {
		s.Web.Host = "127.0.0.1"
	}
	return &s, nil
}

// SaveSettings seals the passwords and writes the file atomically (owner-only off Windows).
func SaveSettings(s *Settings, path string) error {
	c := *s
	c.Accounts = make([]AccountSetting, len(s.Accounts))
	for i, a := range s.Accounts {
		a.Password = Seal(a.Password)
		c.Accounts[i] = a
	}
	b, err := json.MarshalIndent(&c, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, b, runtime.GOOS != "windows")
}

func writeAtomic(path string, data []byte, private bool) error {
	tmp := path + ".tmp"
	mode := os.FileMode(0o644)
	if private {
		mode = 0o600
	}
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Seal protects a secret at rest: Windows DPAPI (current user), unchanged elsewhere.
func Seal(s string) string {
	if s == "" || runtime.GOOS != "windows" {
		return s
	}
	b, err := dpapi([]byte(s), true)
	if err != nil {
		return s
	}
	return "dpapi:" + base64.StdEncoding.EncodeToString(b)
}

// Unseal reverses Seal ("" if it was sealed by another Windows user or machine).
func Unseal(s string) string {
	if !strings.HasPrefix(s, "dpapi:") {
		return s
	}
	b, err := base64.StdEncoding.DecodeString(s[6:])
	if err != nil {
		return ""
	}
	out, err := dpapi(b, false)
	if err != nil {
		return ""
	}
	return string(out)
}
