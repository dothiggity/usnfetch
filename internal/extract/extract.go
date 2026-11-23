// Package extract does archive detection + extraction for finished usnfetch jobs
// (a port of usnfetch_extract.py).
//
// It finds RAR (name.partNN.rar, name.rar + .rNN/.sNN), 7z (name.7z, name.7z.NNN) and
// zip (name.zip + .zNN, name.zip.NNN) sets in a job folder, extracts each set into
// the folder it sits in with UnRAR (RAR) or 7-Zip (everything, RAR fallback), and
// deletes a set's volumes only after that set extracted cleanly. The sets are found
// once, before anything is extracted, so archives that come out of an archive are
// left as they are.
package extract

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"unicode"
)

// Result is what ExtractFolder reports; it marshals like the Python dict (the extra
// counts are left out when zero).
type Result struct {
	Sets   int               `json:"sets"`
	OK     []string          `json:"ok"`              // labels (path relative to the folder) of sets that extracted cleanly
	Failed map[string]string `json:"failed"`          // set label -> reason
	Kept   int               `json:"kept,omitempty"`  // single .zip / .7z files left as they are (Options.Standalone off)
	Empty  int               `json:"empty,omitempty"` // 0-byte files named like archives (nothing to extract)
}

// Options for Extract.
type Options struct {
	Password        string // "" = none
	DeleteArchives  bool   // delete a set's volumes after it extracted cleanly
	SevenZip, Unrar string // configured tool paths ("" or a missing file = auto-detect)
	// Standalone also extracts a single .zip or .7z (one that is not part of a split
	// set). Off, those stay as they are: ROM, BIOS and asset packs are zipped on purpose.
	// RAR archives and split sets (.z01, .zip.001, .7z.001) are always extracted.
	Standalone bool
	// Only, when not nil, limits the work to sets that include one of these files and to
	// the folders holding them, so other downloads in the same folder tree are left alone.
	Only []string
}

type archiveSet struct {
	kind    string   // "rar" | "7z" | "zip"
	first   string   // path handed to the extractor
	volumes []string // every file belonging to the set (deleted on success)
}

// standalone: a lone .zip or .7z, not a volume of a split set.
func (s *archiveSet) standalone() bool {
	if len(s.volumes) != 1 || (s.kind != "zip" && s.kind != "7z") {
		return false
	}
	return !reSplit.MatchString(filepath.Base(s.first))
}

// SetInfo describes one archive set found among a folder's file names.
type SetInfo struct {
	Kind       string // "rar" | "7z" | "zip"
	First      string // the name handed to the extractor
	Volumes    []string
	Standalone bool // a single .zip / .7z (see Options.Standalone)
}

// Sets lists the archive sets among one folder's file names (no file system access),
// as Extract would find them.
func Sets(names []string) []SetInfo {
	var out []SetInfo
	for _, s := range dirSets("", names, map[string]bool{}) {
		out = append(out, SetInfo{Kind: s.kind, First: s.first, Volumes: s.volumes, Standalone: s.standalone()})
	}
	return out
}

// Python's str-pattern \d is any Unicode decimal digit, its $ also matches just
// before a trailing newline, and under re.I the letters i, I, ı and İ all match each
// other (Go's (?i) keeps ı and İ apart); these keep the Go patterns matching the
// same names as the Python ones.
const (
	digit  = `\p{Nd}`
	eol    = `\n?$`
	anyI   = `[iIıİ]`
	zipExt = `z` + anyI + `p`
)

var (
	reRarPart = regexp.MustCompile(`(?i)^(.+)\.part(` + digit + `+)\.rar` + eol)
	reRar     = regexp.MustCompile(`(?i)^(.+)\.rar` + eol)
	re7z      = regexp.MustCompile(`(?i)^(.+)\.7z` + eol)
	reZip     = regexp.MustCompile(`(?i)^(.+)\.` + zipExt + eol)
	reSplit   = regexp.MustCompile(`(?i)^(.+\.(7z|` + zipExt + `))\.` + digit + `{3}` + eol)
)

var reVolume = regexp.MustCompile(`(?i)\.(?:rar|7z|` + zipExt + `|[r-z]` + digit + digit + `|` + digit + `{3})` + eol)

// LooksLikeVolume: whether a file (path or name) could belong to an archive set. Cheap
// and generous; the extractor decides for real.
func LooksLikeVolume(p string) bool {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		p = p[i+1:]
	}
	return reVolume.MatchString(p)
}

// quoteBase is re.escape(base) for use under (?i).
func quoteBase(base string) string {
	var sb strings.Builder
	for _, r := range base {
		switch r {
		case 'i', 'I', 'ı', 'İ':
			sb.WriteString(anyI)
		default:
			sb.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	return sb.String()
}

// pyLower is Python's str.lower() for file names: like strings.ToLower except that
// U+0130 İ becomes i + U+0307 (combining dot above). Python's final-sigma rule is
// not applied.
func pyLower(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "İ", "i̇"))
}

// Classify reports the kind ("rar", "7z" or "zip") of the archive set that name
// (a file in dir whose directory listing is names) starts or belongs to, or "" when
// name is not part of an archive set.
func Classify(dir, name string, names []string) string {
	if s := classify(dir, name, newNameIndex(names)); s != nil {
		return s.kind
	}
	return ""
}

// nameIndex finds the names of a folder that can match a `^base\.…` volume pattern
// without running the pattern over every name (a ROM folder can hold tens of thousands
// of archives, which made set detection quadratic). Names are keyed by their
// case-folded form -- the folding the (?i) patterns plus anyI apply -- and sorted; a
// pattern then runs only over the names whose key starts with the base's key.
type nameIndex struct {
	keys  []string
	names []string
}

// foldKey maps every rune to the smallest rune of its simple-fold orbit (what (?i)
// treats as equal), with ı and İ joining i's orbit as anyI does. It maps rune for
// rune, so a fold-equal prefix is a byte prefix of the keys.
func foldKey(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for _, r := range s {
		if r == 'ı' || r == 'İ' {
			r = 'i'
		}
		m := r
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			if f < m {
				m = f
			}
		}
		sb.WriteRune(m)
	}
	return sb.String()
}

func newNameIndex(names []string) *nameIndex {
	ix := &nameIndex{keys: make([]string, len(names)), names: make([]string, len(names))}
	order := make([]int, len(names))
	keys := make([]string, len(names))
	for i, n := range names {
		order[i], keys[i] = i, foldKey(n)
	}
	sort.Slice(order, func(a, b int) bool { return keys[order[a]] < keys[order[b]] })
	for i, o := range order {
		ix.keys[i], ix.names[i] = keys[o], names[o]
	}
	return ix
}

var bruteForce bool // tests: withPrefix returns every name, as before the index existed

// withPrefix: the names whose folded form starts with base's.
func (ix *nameIndex) withPrefix(base string) []string {
	if bruteForce {
		return ix.names
	}
	k := foldKey(base)
	var out []string
	for i := sort.SearchStrings(ix.keys, k); i < len(ix.keys) && strings.HasPrefix(ix.keys[i], k); i++ {
		out = append(out, ix.names[i])
	}
	return out
}

func findArchiveSets(folder string) []*archiveSet {
	var sets []*archiveSet
	claimed := map[string]bool{}
	walk(folder, func(dir string, names []string) {
		sets = append(sets, dirSets(dir, names, claimed)...)
	})
	return sets
}

// dirSets classifies one directory's file names in sorted order, skipping names
// that an earlier set already claimed as volumes (compared lower-cased).
func dirSets(dir string, names []string, claimed map[string]bool) []*archiveSet {
	var sets []*archiveSet
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	ix := newNameIndex(names)
	for _, name := range sorted {
		if claimed[pyLower(pyJoin(dir, name))] || strings.HasSuffix(name, ".usnfetch") {
			continue
		}
		if s := classify(dir, name, ix); s != nil {
			sets = append(sets, s)
			for _, v := range s.volumes {
				claimed[pyLower(v)] = true
			}
		}
	}
	return sets
}

// matching returns the names matching pattern (case-insensitively), sorted.
func matching(names []string, pattern string) []string {
	rx := regexp.MustCompile(`(?i)` + pattern)
	var out []string
	for _, n := range names {
		if rx.MatchString(n) {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

func classify(d, name string, ix *nameIndex) *archiveSet {
	j := func(ns []string) []string {
		out := make([]string, len(ns))
		for i, n := range ns {
			out[i] = pyJoin(d, n)
		}
		return out
	}
	// Python indexes vols[0] here; that only fails (IndexError) when name is not in
	// names and no other name matches, which this reports as "not an archive".
	firstOf := func(kind string, vols []string) *archiveSet {
		if len(vols) == 0 {
			return nil
		}
		return &archiveSet{kind, pyJoin(d, vols[0]), j(vols)}
	}
	if m := reRarPart.FindStringSubmatch(name); m != nil {
		vols := matching(ix.withPrefix(m[1]), `^`+quoteBase(m[1])+`\.part`+digit+`+\.rar`+eol)
		sort.SliceStable(vols, func(a, b int) bool {
			return digitsLess(reRarPart.FindStringSubmatch(vols[a])[2], reRarPart.FindStringSubmatch(vols[b])[2])
		})
		return firstOf("rar", vols)
	}
	if m := reRar.FindStringSubmatch(name); m != nil {
		vols := matching(ix.withPrefix(m[1]), `^`+quoteBase(m[1])+`\.(?:rar|[r-z]`+digit+digit+`)`+eol)
		return &archiveSet{"rar", pyJoin(d, name), j(vols)}
	}
	if m := reSplit.FindStringSubmatch(name); m != nil {
		vols := matching(ix.withPrefix(m[1]), `^`+quoteBase(m[1])+`\.`+digit+`{3}`+eol)
		return firstOf(pyLower(m[2]), vols)
	}
	if re7z.MatchString(name) {
		p := pyJoin(d, name)
		return &archiveSet{"7z", p, []string{p}}
	}
	if m := reZip.FindStringSubmatch(name); m != nil {
		vols := matching(ix.withPrefix(m[1]), `^`+quoteBase(m[1])+`\.(?:`+zipExt+`|z`+digit+digit+`)`+eol)
		return &archiveSet{"zip", pyJoin(d, name), j(vols)}
	}
	return nil
}

// digitsLess orders two runs of decimal digits numerically, like int(a) < int(b)
// in Python (any Unicode Nd digit, no size limit).
func digitsLess(a, b string) bool {
	x, y := strings.TrimLeft(asciiDigits(a), "0"), strings.TrimLeft(asciiDigits(b), "0")
	if len(x) != len(y) {
		return len(x) < len(y)
	}
	return x < y
}

func asciiDigits(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			sb.WriteRune(r)
			continue
		}
		// Unicode encodes every script's decimal digits as a contiguous run 0..9,
		// so a digit's value is its offset from the start of its run.
		n := 0
		for unicode.Is(unicode.Nd, r-rune(n)-1) {
			n++
		}
		sb.WriteByte(byte('0' + n%10))
	}
	return sb.String()
}

// ----------------------------------------------------------------------------
// paths (Python's os.walk / os.path.join / os.path.dirname semantics)
// ----------------------------------------------------------------------------

// walk calls visit(dir, fileNames) for folder and every directory below it,
// top-down in directory order, like os.walk(folder): names are entries that are not
// directories (after following symlinks), symlinked directories are not descended
// into (junctions are), and unreadable directories are skipped silently.
func walk(top string, visit func(dir string, names []string)) {
	stack := []string{top}
	for len(stack) > 0 {
		dir := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		entries, err := readDirUnsorted(dir)
		if err != nil {
			continue
		}
		var dirs, names []string
		for _, e := range entries {
			if entryIsDir(dir, e) {
				dirs = append(dirs, e.Name())
			} else {
				names = append(names, e.Name())
			}
		}
		visit(dir, names)
		for i := len(dirs) - 1; i >= 0; i-- {
			p := pyJoin(dir, dirs[i])
			if fi, err := os.Lstat(p); err != nil || fi.Mode()&fs.ModeSymlink == 0 {
				stack = append(stack, p)
			}
		}
	}
}

func readDirUnsorted(dir string) ([]fs.DirEntry, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.ReadDir(-1)
}

// entryIsDir is Python's DirEntry.is_dir(): follows symlinks, false on error.
func entryIsDir(dir string, e fs.DirEntry) bool {
	t := e.Type()
	switch {
	case t&fs.ModeSymlink != 0:
		fi, err := os.Stat(pyJoin(dir, e.Name()))
		return err == nil && fi.IsDir()
	case t.IsDir():
		return true
	case t&fs.ModeIrregular != 0:
		return reparseIsDir(e)
	}
	return false
}

// pyJoin is os.path.join(a, b) for a plain file name b.
func pyJoin(a, b string) string {
	switch {
	case a == "", os.IsPathSeparator(a[len(a)-1]):
		return a + b
	case runtime.GOOS == "windows" && strings.HasSuffix(a, ":") && filepath.VolumeName(a) == a:
		return a + b // "C:" + "x" -> "C:x"
	}
	return a + string(filepath.Separator) + b
}

// pyDirname is os.path.dirname(p): no cleaning, trailing separators stripped
// unless only the root is left.
func pyDirname(p string) string {
	vol := len(filepath.VolumeName(p))
	i := len(p)
	for i > vol && !os.IsPathSeparator(p[i-1]) {
		i--
	}
	head := p[:i]
	j := len(head)
	for j > vol && os.IsPathSeparator(head[j-1]) {
		j--
	}
	if j == vol {
		return head
	}
	return head[:j]
}

// ----------------------------------------------------------------------------
// tools
// ----------------------------------------------------------------------------

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

func firstExisting(cands ...string) string {
	for _, c := range cands {
		if c != "" && isFile(c) {
			return c
		}
	}
	return ""
}

func which(name string) string {
	p, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return p
}

func programFiles(rel ...string) []string {
	var out []string
	for _, v := range []string{"ProgramFiles", "ProgramW6432", "ProgramFiles(x86)"} {
		if p := os.Getenv(v); p != "" {
			out = append(out, filepath.Join(append([]string{p}, rel...)...))
		}
	}
	return out
}

func find7z(configured string) string {
	cands := []string{configured, which("7z")}
	if runtime.GOOS != "windows" {
		cands = append(cands, which("7zz"))
	}
	cands = append(cands, which("7za"))
	return firstExisting(append(cands, programFiles("7-Zip", "7z.exe")...)...)
}

func findUnrar(configured string) string {
	return firstExisting(append([]string{configured, which("unrar")}, programFiles("WinRAR", "UnRAR.exe")...)...)
}

// Find7z returns the path of 7-Zip (7z on PATH, or Program Files\7-Zip\7z.exe), or "".
func Find7z() string { return find7z("") }

// FindUnrar returns the path of UnRAR (unrar on PATH, or Program Files\WinRAR\UnRAR.exe), or "".
func FindUnrar() string { return findUnrar("") }

// run starts tool with stdin on the null device and no console window, waits, and
// returns its exit code plus the tail Python's error messages show:
// (stderr or stdout).strip()[-400:].
func run(tool string, args ...string) (int, string, error) {
	cmd := exec.Command(tool, args...)
	if errors.Is(cmd.Err, exec.ErrDot) {
		cmd.Err = nil // a configured bare name that exists in the current directory
	}
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	hideWindow(cmd)
	code := 0
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			return 0, "", err
		}
		code = ee.ExitCode()
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			code = -int(ws.Signal()) // Python's returncode for a signal death
		}
	}
	out := stderr.String()
	if out == "" {
		out = stdout.String()
	}
	return code, outputTail(out), nil
}

// outputTail decodes captured output as Python's text-mode subprocess does
// (locale encoding, errors="replace", universal newlines), strips it and keeps
// the last 400 characters.
func outputTail(raw string) string {
	s := decodeOutput([]byte(raw))
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.TrimFunc(s, func(r rune) bool { return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f) })
	if r := []rune(s); len(r) > 400 {
		s = string(r[len(r)-400:])
	}
	return s
}

// extractSet extracts one set next to its first volume and returns the tool's name.
func extractSet(s *archiveSet, password, sevenZip, unrar string) (string, error) {
	outDir := pyDirname(s.first)
	if s.kind == "rar" && unrar != "" {
		pw := "-p-"
		if password != "" {
			pw = "-p" + password
		}
		code, tail, err := run(unrar, "x", "-o+", "-y", "-idq", pw, s.first, outDir+string(os.PathSeparator))
		if err != nil {
			return "", fmt.Errorf("UnRAR could not be started: %v", err)
		}
		if code != 0 {
			return "", fmt.Errorf("UnRAR exit %d: %s", code, tail)
		}
		return "UnRAR", nil
	}
	if sevenZip == "" {
		return "", errors.New("no extractor found: install 7-Zip (or WinRAR for .rar) or set the path in Settings")
	}
	args := []string{"x"}
	if password != "" {
		args = append(args, "-p"+password)
	}
	args = append(args, "-y", "-aoa", "-bd", "-o"+outDir, s.first)
	code, tail, err := run(sevenZip, args...)
	if err != nil {
		return "", fmt.Errorf("7-Zip could not be started: %v", err)
	}
	if code != 0 { // 1 = warning (e.g. a locked file) -- not clean enough to delete the volumes
		return "", fmt.Errorf("7-Zip exit %d: %s", code, tail)
	}
	return "7-Zip", nil
}

// ExtractFolder extracts every archive set under dir into the folder each set sits
// in. password "" means none; sevenZip / unrar are configured tool paths ("" or a
// missing file = auto-detect). When deleteArchives is set, the volumes of every set
// that extracted cleanly are deleted. log receives progress lines (may be nil).
func ExtractFolder(dir string, log func(string), password string, deleteArchives bool, sevenZip, unrar string) Result {
	return Extract(dir, log, Options{Password: password, DeleteArchives: deleteArchives, SevenZip: sevenZip, Unrar: unrar, Standalone: true})
}

// setsFor finds the archive sets that include one of files, looking only in the
// folders that hold them (their other volumes may come from an earlier download).
func setsFor(files []string) []*archiveSet {
	want := map[string]bool{}
	seen := map[string]bool{}
	var dirs []string
	for _, f := range files {
		want[pyLower(f)] = true
		if d := pyDirname(f); !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	sort.Strings(dirs)
	claimed := map[string]bool{}
	var sets []*archiveSet
	for _, d := range dirs {
		entries, err := readDirUnsorted(d)
		if err != nil {
			continue
		}
		var names []string
		for _, e := range entries {
			if !entryIsDir(d, e) {
				names = append(names, e.Name())
			}
		}
		for _, s := range dirSets(d, names, claimed) {
			for _, v := range s.volumes {
				if want[pyLower(v)] {
					sets = append(sets, s)
					break
				}
			}
		}
	}
	return sets
}

// Extract extracts the archive sets under dir (or those of o.Only) into the folder
// each set sits in. A 0-byte file is never an archive and is skipped.
func Extract(dir string, log func(string), o Options) Result {
	if log == nil {
		log = func(string) {}
	}
	password, deleteArchives := o.Password, o.DeleteArchives
	sz, ur := find7z(o.SevenZip), findUnrar(o.Unrar)
	var sets []*archiveSet
	if o.Only != nil {
		sets = setsFor(o.Only)
	} else {
		sets = findArchiveSets(dir)
	}
	res := Result{Sets: len(sets), OK: []string{}, Failed: map[string]string{}}
	for _, s := range sets {
		label, err := filepath.Rel(dir, s.first)
		if err != nil {
			label = s.first
		}
		if !o.Standalone && s.standalone() {
			res.Kept++
			continue
		}
		if fi, err := os.Stat(s.first); err == nil && fi.Size() == 0 {
			res.Empty++
			log(fmt.Sprintf("skipping %s: empty file, not an archive", label))
			continue
		}
		log(fmt.Sprintf("extracting %s (%d volume(s), %s)", label, len(s.volumes), s.kind))
		tool, err := extractSet(s, password, sz, ur)
		if err != nil {
			res.Failed[label] = err.Error()
			log(fmt.Sprintf("extract FAILED %s: %v", label, err))
			continue
		}
		res.OK = append(res.OK, label)
		log(fmt.Sprintf("extracted %s with %s", label, tool))
		if deleteArchives {
			for _, v := range s.volumes {
				if err := os.Remove(v); err != nil {
					log(fmt.Sprintf("could not delete %s: %v", v, err))
				}
			}
		}
	}
	if res.Kept > 0 {
		log(fmt.Sprintf("left %d single .zip/.7z file(s) as they are", res.Kept))
	}
	return res
}
