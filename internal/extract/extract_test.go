package extract

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// ----------------------------------------------------------------------------
// archive fixture (mirrors test_pipeline.py's make_payload / build_archives)
// ----------------------------------------------------------------------------

type fixture struct {
	sz, rar  string
	payload  map[string][]byte // "big.bin", "sub/notes.txt" -> content
	arc      string            // folder holding the built archives
	arcFiles []string          // their names
}

func findRar() string {
	pf := os.Getenv("ProgramFiles")
	if pf == "" {
		pf = `C:\Program Files`
	}
	if p := filepath.Join(pf, "WinRAR", "Rar.exe"); isFile(p) {
		return p
	}
	return which("rar")
}

func mustRun(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	fx := &fixture{sz: Find7z(), rar: findRar()}
	if fx.sz == "" {
		t.Skip("7-Zip not found")
	}
	work := t.TempDir()
	src := filepath.Join(work, "payload")
	big := make([]byte, 2_600_000)
	rand.Read(big)
	fx.payload = map[string][]byte{
		"big.bin":       big,
		"sub/notes.txt": bytes.Repeat([]byte("hello from inside the archive\n"), 500),
	}
	for rel, data := range fx.payload {
		writeFile(t, filepath.Join(src, filepath.FromSlash(rel)), data)
	}
	fx.arc = filepath.Join(work, "arc")
	os.MkdirAll(fx.arc, 0o755)
	a := func(name string) string { return filepath.Join(fx.arc, name) }
	mustRun(t, src, fx.sz, "a", "-v1m", "-mx0", a("set7z.7z"), "big.bin", "sub")
	mustRun(t, src, fx.sz, "a", "-ppw", "-mhe=on", "-mx0", a("locked.7z"), "big.bin")
	mustRun(t, src, fx.sz, "a", "-tzip", "-mx0", a("plain.zip"), "sub")
	mustRun(t, src, fx.sz, "a", "-tzip", "-v1m", "-mx0", a("split.zip"), "big.bin")
	if fx.rar != "" {
		mustRun(t, src, fx.rar, "a", "-v1m", "-m0", "-r", "-idq", a("setrar.rar"), "big.bin", "sub")
		// RAR 5+ only writes .partN.rar volumes; renamed copies make an old-style
		// .rar/.r00/.r01 set, which UnRAR and 7-Zip both still extract.
		mustRun(t, src, fx.rar, "a", "-v1m", "-m0", "-idq", a("old.rar"), "big.bin")
		for i, n := range []string{"oldrar.rar", "oldrar.r00", "oldrar.r01"} {
			if err := os.Rename(a("old.part"+string(rune('1'+i))+".rar"), a(n)); err != nil {
				t.Fatal(err)
			}
		}
		if names := dirNames(t, fx.arc); slices.ContainsFunc(names, func(n string) bool { return strings.HasPrefix(n, "old.") }) {
			t.Fatalf("old-style RAR source left more than 3 volumes: %v", names)
		}
	} else {
		t.Log("note: WinRAR Rar.exe not found, skipping the RAR sets")
	}
	fx.arcFiles = dirNames(t, fx.arc)
	return fx
}

// jobFolder lays out a finished job: every archive file, a plain nfo, and a copy of
// plain.zip one folder down.
func (fx *fixture) jobFolder(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "Release.Name")
	for _, n := range fx.arcFiles {
		writeFile(t, filepath.Join(out, n), readFile(t, filepath.Join(fx.arc, n)))
	}
	writeFile(t, filepath.Join(out, "release.nfo"), []byte("not an archive\n"))
	writeFile(t, filepath.Join(out, "nested", "plain2.zip"), readFile(t, filepath.Join(fx.arc, "plain.zip")))
	return out
}

// wantLabels is the extraction order: sorted names in the top folder, then nested/.
func (fx *fixture) wantLabels() []string {
	want := []string{"locked.7z"}
	if fx.rar != "" {
		want = append(want, "oldrar.rar")
	}
	want = append(want, "plain.zip", "set7z.7z.001")
	if fx.rar != "" {
		want = append(want, "setrar.part1.rar")
	}
	return append(want, "split.zip.001", filepath.Join("nested", "plain2.zip"))
}

func (fx *fixture) checkTree(t *testing.T, root string, rels ...string) {
	t.Helper()
	for _, rel := range rels {
		want := fx.payload[strings.TrimPrefix(rel, "nested/")]
		p := filepath.Join(root, filepath.FromSlash(rel))
		got, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("missing extracted %s: %v", p, err)
		} else if !bytes.Equal(got, want) {
			t.Errorf("content mismatch %s (%d bytes, want %d)", p, len(got), len(want))
		}
	}
}

func writeFile(t *testing.T, p string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range es {
		names = append(names, e.Name())
	}
	return names
}

type logbuf struct{ lines []string }

func (l *logbuf) log(s string) { l.lines = append(l.lines, s) }
func (l *logbuf) has(s string) bool {
	return slices.Contains(l.lines, s)
}

// ----------------------------------------------------------------------------
// end-to-end with the real tools
// ----------------------------------------------------------------------------

func TestArchives(t *testing.T) {
	fx := newFixture(t)
	unrar := FindUnrar()

	t.Run("extract and delete volumes", func(t *testing.T) {
		out := fx.jobFolder(t)
		var lg logbuf
		res := ExtractFolder(out, lg.log, "pw", true, "", "")
		if len(res.Failed) != 0 || !reflect.DeepEqual(res.OK, fx.wantLabels()) || res.Sets != len(res.OK) {
			t.Fatalf("result %+v, want ok=%v\nlog:\n%s", res, fx.wantLabels(), strings.Join(lg.lines, "\n"))
		}
		fx.checkTree(t, out, "big.bin", "sub/notes.txt", "nested/sub/notes.txt") // last extraction wins; same content
		left := dirNames(t, out)
		for _, n := range left {
			if k := Classify(out, n, left); k != "" {
				t.Errorf("archive volume not deleted: %s (%s); left %v", n, k, left)
			}
		}
		if want := []string{"big.bin", "nested", "release.nfo", "sub"}; !reflect.DeepEqual(left, want) {
			t.Errorf("left %v, want %v", left, want)
		}
		if got := dirNames(t, filepath.Join(out, "nested")); !reflect.DeepEqual(got, []string{"sub"}) {
			t.Errorf("nested left %v", got)
		}
		if got := readFile(t, filepath.Join(out, "release.nfo")); string(got) != "not an archive\n" {
			t.Errorf("release.nfo changed: %q", got)
		}
		if !lg.has("extracting set7z.7z.001 (3 volume(s), 7z)") || !lg.has("extracted set7z.7z.001 with 7-Zip") {
			t.Errorf("log lines missing:\n%s", strings.Join(lg.lines, "\n"))
		}
		if fx.rar != "" {
			tool := "7-Zip"
			if unrar != "" {
				tool = "UnRAR"
			}
			for _, l := range []string{"extracting setrar.part1.rar (3 volume(s), rar)", "extracted setrar.part1.rar with " + tool,
				"extracting oldrar.rar (3 volume(s), rar)", "extracted oldrar.rar with " + tool} {
				if !lg.has(l) {
					t.Errorf("log line %q missing:\n%s", l, strings.Join(lg.lines, "\n"))
				}
			}
		}
	})

	t.Run("keep volumes when deleteArchives is false", func(t *testing.T) {
		out := fx.jobFolder(t)
		res := ExtractFolder(out, nil, "pw", false, "", "")
		if len(res.Failed) != 0 || !reflect.DeepEqual(res.OK, fx.wantLabels()) {
			t.Fatalf("result %+v", res)
		}
		fx.checkTree(t, out, "big.bin", "sub/notes.txt", "nested/sub/notes.txt")
		for _, n := range fx.arcFiles {
			if got := readFile(t, filepath.Join(out, n)); !bytes.Equal(got, readFile(t, filepath.Join(fx.arc, n))) {
				t.Errorf("volume %s changed", n)
			}
		}
		if !isFile(filepath.Join(out, "nested", "plain2.zip")) {
			t.Error("nested/plain2.zip deleted")
		}
	})

	t.Run("single archives kept, empty file skipped", func(t *testing.T) {
		out := fx.jobFolder(t)
		writeFile(t, filepath.Join(out, "empty.rar"), nil)
		var lg logbuf
		res := Extract(out, lg.log, Options{Password: "pw", DeleteArchives: true})
		var want []string
		if fx.rar != "" {
			want = append(want, "oldrar.rar")
		}
		want = append(want, "set7z.7z.001")
		if fx.rar != "" {
			want = append(want, "setrar.part1.rar")
		}
		want = append(want, "split.zip.001")
		if len(res.Failed) != 0 || !reflect.DeepEqual(res.OK, want) || res.Kept != 3 || res.Empty != 1 {
			t.Fatalf("result %+v, want ok=%v kept=3 empty=1\nlog:\n%s", res, want, strings.Join(lg.lines, "\n"))
		}
		for _, n := range []string{"locked.7z", "plain.zip", "empty.rar", filepath.Join("nested", "plain2.zip")} {
			if !isFile(filepath.Join(out, n)) {
				t.Errorf("%s was removed", n)
			}
		}
		if !lg.has("left 3 single .zip/.7z file(s) as they are") || !lg.has("skipping empty.rar: empty file, not an archive") {
			t.Errorf("log lines missing:\n%s", strings.Join(lg.lines, "\n"))
		}
	})

	t.Run("only the sets of the given files", func(t *testing.T) {
		out := fx.jobFolder(t)
		only := []string{filepath.Join(out, "set7z.7z.002"), filepath.Join(out, "nested", "plain2.zip"), filepath.Join(out, "release.nfo")}
		res := Extract(out, nil, Options{DeleteArchives: true, Standalone: true, Only: only})
		want := []string{"set7z.7z.001", filepath.Join("nested", "plain2.zip")}
		if len(res.Failed) != 0 || !reflect.DeepEqual(res.OK, want) || res.Sets != 2 {
			t.Fatalf("result %+v, want ok=%v", res, want)
		}
		if !isFile(filepath.Join(out, "plain.zip")) || !isFile(filepath.Join(out, "split.zip.001")) || isFile(filepath.Join(out, "set7z.7z.001")) {
			t.Errorf("wrong files touched: %v", dirNames(t, out))
		}
	})

	t.Run("wrong password fails the set and keeps its volumes", func(t *testing.T) {
		out := fx.jobFolder(t)
		var lg logbuf
		res := ExtractFolder(out, lg.log, "wrong", true, "", "")
		want := slices.DeleteFunc(fx.wantLabels(), func(s string) bool { return s == "locked.7z" })
		if !reflect.DeepEqual(res.OK, want) || len(res.Failed) != 1 || res.Sets != len(want)+1 {
			t.Fatalf("result %+v", res)
		}
		why := res.Failed["locked.7z"]
		if !strings.HasPrefix(why, "7-Zip exit 2: ") || !strings.Contains(why, "Wrong password") {
			t.Errorf("failure reason %q", why)
		}
		if !lg.has("extract FAILED locked.7z: " + why) {
			t.Errorf("log:\n%s", strings.Join(lg.lines, "\n"))
		}
		left := dirNames(t, out)
		if !slices.Contains(left, "locked.7z") {
			t.Errorf("locked.7z deleted: %v", left)
		}
		for _, n := range left {
			if k := Classify(out, n, left); k != "" && n != "locked.7z" {
				t.Errorf("volume of a clean set not deleted: %s", n)
			}
		}
		fx.checkTree(t, out, "big.bin", "sub/notes.txt")
	})

	t.Run("no password for an encrypted 7z fails without prompting", func(t *testing.T) {
		// stdin is the null device, so 7-Zip's password prompt reads EOF
		// (7-Zip 26: "7-Zip exit 255: Break signaled", same as the Python version)
		s := classify(fx.arc, "locked.7z", newNameIndex(fx.arcFiles))
		if _, err := extractSet(s, "", fx.sz, ""); err == nil || !strings.HasPrefix(err.Error(), "7-Zip exit ") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("configured tool paths", func(t *testing.T) {
		out := fx.jobFolder(t)
		var lg logbuf
		// a configured 7-Zip is used; a configured path that does not exist falls back to auto-detect
		res := ExtractFolder(out, lg.log, "pw", true, fx.sz, filepath.Join(t.TempDir(), "missing", "UnRAR.exe"))
		if len(res.Failed) != 0 || len(res.OK) != len(fx.wantLabels()) {
			t.Fatalf("result %+v", res)
		}
	})

	t.Run("no extractor", func(t *testing.T) {
		s := classify(fx.arc, "set7z.7z.002", newNameIndex(fx.arcFiles))
		_, err := extractSet(s, "", "", "")
		if err == nil || err.Error() != "no extractor found: install 7-Zip (or WinRAR for .rar) or set the path in Settings" {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("unstartable extractor", func(t *testing.T) {
		bogus := filepath.Join(t.TempDir(), "7z.exe")
		writeFile(t, bogus, []byte("not a program"))
		s := classify(fx.arc, "plain.zip", newNameIndex(fx.arcFiles))
		if _, err := extractSet(s, "", bogus, ""); err == nil || !strings.HasPrefix(err.Error(), "7-Zip could not be started: ") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("rar", func(t *testing.T) {
		if fx.rar == "" {
			t.Skip("WinRAR Rar.exe not found")
		}
		// RAR falls back to 7-Zip without UnRAR
		out := filepath.Join(t.TempDir(), "x")
		for _, n := range fx.arcFiles {
			if strings.HasPrefix(n, "setrar.") {
				writeFile(t, filepath.Join(out, n), readFile(t, filepath.Join(fx.arc, n)))
			}
		}
		s := classify(out, "setrar.part3.rar", newNameIndex(dirNames(t, out)))
		if tool, err := extractSet(s, "", fx.sz, ""); err != nil || tool != "7-Zip" {
			t.Fatalf("7-Zip on RAR: %q %v", tool, err)
		}
		fx.checkTree(t, out, "big.bin", "sub/notes.txt")

		if unrar == "" {
			t.Skip("UnRAR not found")
		}
		// encrypted headers: UnRAR with no / wrong password fails (no prompt), right one extracts
		src := filepath.Join(t.TempDir(), "src")
		writeFile(t, filepath.Join(src, "big.bin"), fx.payload["big.bin"])
		enc := filepath.Join(t.TempDir(), "enc")
		os.MkdirAll(enc, 0o755)
		mustRun(t, src, fx.rar, "a", "-hpsecret", "-m0", "-idq", filepath.Join(enc, "enc.rar"), "big.bin")
		s = classify(enc, "enc.rar", newNameIndex([]string{"enc.rar"}))
		for _, pw := range []string{"", "nope"} {
			if _, err := extractSet(s, pw, fx.sz, unrar); err == nil || !strings.HasPrefix(err.Error(), "UnRAR exit ") {
				t.Errorf("password %q: err = %v", pw, err)
			}
		}
		if tool, err := extractSet(s, "secret", fx.sz, unrar); err != nil || tool != "UnRAR" {
			t.Fatalf("UnRAR: %q %v", tool, err)
		}
		fx.checkTree(t, enc, "big.bin")
	})
}

// ----------------------------------------------------------------------------
// pure helpers
// ----------------------------------------------------------------------------

func TestClassify(t *testing.T) {
	names := []string{
		"a.part1.rar", "a.part2.rar", "A.PART10.rar",
		"b.rar", "b.r00", "b.r01", "B.S00", "b.t99", "b.r0", "b.q00",
		"c.7z.001", "c.7z.002", "C.7Z.003", "c.7z.01",
		"d.zip", "d.z01", "d.Z02", "d.zip.usnfetch",
		"e.zip.001", "e.zip.002",
		"f.7z", "notes.nfo", "x.r00", "g.7z.part1.rar",
		"h.part\u0662.rar", "h.part\u0661.rar", "h.part10.rar",
		"K\u0131rm\u0131z\u0131.part1.rar", "KIRMIZI.part2.rar", "y.Z\u0130P.001", "y.zip.002",
	}
	dir := `D:\x`
	if runtime.GOOS != "windows" {
		dir = "/x"
	}
	p := func(ns ...string) []string {
		for i, n := range ns {
			ns[i] = dir + string(filepath.Separator) + n
		}
		return ns
	}
	cases := []struct {
		name, kind string
		first      string
		volumes    []string
	}{
		{"a.part2.rar", "rar", "a.part1.rar", p("a.part1.rar", "a.part2.rar", "A.PART10.rar")},
		{"A.PART10.rar", "rar", "a.part1.rar", p("a.part1.rar", "a.part2.rar", "A.PART10.rar")},
		{"b.rar", "rar", "b.rar", p("B.S00", "b.r00", "b.r01", "b.rar", "b.t99")},
		{"b.r00", "", "", nil},
		{"x.r00", "", "", nil},
		{"c.7z.002", "7z", "C.7Z.003", p("C.7Z.003", "c.7z.001", "c.7z.002")},
		{"c.7z.01", "", "", nil},
		{"d.zip", "zip", "d.zip", p("d.Z02", "d.z01", "d.zip")},
		{"d.z01", "", "", nil},
		{"e.zip.002", "zip", "e.zip.001", p("e.zip.001", "e.zip.002")},
		{"f.7z", "7z", "f.7z", p("f.7z")},
		{"notes.nfo", "", "", nil},
		{"d.zip.usnfetch", "", "", nil},
		{"g.7z.part1.rar", "rar", "g.7z.part1.rar", p("g.7z.part1.rar")},
		// Python parity: \d is any Unicode digit and int() orders them numerically ...
		{"h.part10.rar", "rar", "h.part\u0661.rar", p("h.part\u0661.rar", "h.part\u0662.rar", "h.part10.rar")},
		// ... $ also matches before a trailing newline ...
		{"g.rar\n", "rar", "g.rar\n", nil},
		// ... re.I makes i, I, ı and İ one letter, and str.lower() turns İ into i + U+0307
		{"KIRMIZI.part2.rar", "rar", "Kırmızı.part1.rar", p("Kırmızı.part1.rar", "KIRMIZI.part2.rar")},
		{"y.zip.002", "zip", "y.ZİP.001", p("y.ZİP.001", "y.zip.002")},
		{"y.ZİP.001", "zi̇p", "y.ZİP.001", p("y.ZİP.001", "y.zip.002")},
		{"missing.part1.rar", "", "", nil}, // not in names (Python raises IndexError)
	}
	for _, c := range cases {
		if got := Classify(dir, c.name, names); got != c.kind {
			t.Errorf("Classify(%q) = %q, want %q", c.name, got, c.kind)
		}
		s := classify(dir, c.name, newNameIndex(names))
		if c.kind == "" || c.volumes == nil {
			continue
		}
		if s.first != p(c.first)[0] || !reflect.DeepEqual(s.volumes, c.volumes) {
			t.Errorf("classify(%q) = %+v, want first %s volumes %v", c.name, *s, c.first, c.volumes)
		}
	}
}

// The name index must find exactly the sets the pre-index scan over every name found,
// including names that only fold together under (?i) and anyI (ı/İ/i, K/k/U+212A, s/ſ).
func TestNameIndexMatchesBruteForce(t *testing.T) {
	names := []string{
		"a.part1.rar", "a.part2.rar", "A.PART10.rar", "b.rar", "b.r00", "B.S00", "c.7z.001", "C.7Z.002",
		"d.zip", "d.z01", "D.Z02", "e.zip.001", "e.zip.002", "f.7z", "notes.nfo",
		"Fİle.zip", "file.z01", "FILE.Z02", "fıle.z03", "ſet.rar", "set.r00", "SET.s01",
		"Kelvin.7z.001", "kelvin.7z.002", "KELVIN.7Z.003", "ı.zip", "I.z01", "i.z02",
		"x.part1.rar", "X.PART2.RAR", "xy.part1.rar", "x.y.zip", "x.zip", "x.z01",
		"日本.part1.rar", "日本.part2.rar", "日本語.zip", "日本語.z01", "rom (USA).zip", "rom (usa).z01", "rom (Europe).zip",
	}
	for i := 0; i < 300; i++ { // a ROM folder's worth of lone archives around the sets
		names = append(names, fmt.Sprintf("game %03d (USA).zip", i), fmt.Sprintf("Game %03d (Japan).7z", i))
	}
	run := func(brute bool) []string {
		bruteForce = brute
		defer func() { bruteForce = false }()
		var out []string
		for _, s := range dirSets("D", names, map[string]bool{}) {
			out = append(out, fmt.Sprintf("%s %s %v %v", s.kind, s.first, s.volumes, s.standalone()))
		}
		return out
	}
	want, got := run(true), run(false)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("indexed sets differ from the brute-force scan:\n got %v\nwant %v", got, want)
	}
	sets := Sets([]string{"a.zip", "b.zip", "b.z01", "c.7z", "c.7z.001", "d.rar", "e.zip.001"})
	var lone []string
	for _, s := range sets {
		if s.Standalone {
			lone = append(lone, s.First)
		}
	}
	if !reflect.DeepEqual(lone, []string{"a.zip", "c.7z"}) || len(sets) != 6 {
		t.Errorf("Sets = %+v; standalone %v", sets, lone)
	}
}

func TestFindArchiveSets(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.part1.rar", "a.part2.rar", "A.PART10.rar", "b.rar", "b.r00", "B.S00",
		"c.7z", "c.7z.001", "c.7z.002", "d.z01", "d.zip", "e.zip.001", "readme.txt", "sub/f.7z", "sub/deeper/g.rar"} {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(n)), nil)
	}
	// a trailing separator is kept like os.path.join does, and the label is still relative
	for _, top := range []string{dir, dir + string(filepath.Separator)} {
		var got []string
		for _, s := range findArchiveSets(top) {
			rel, _ := filepath.Rel(dir, s.first)
			got = append(got, s.kind+":"+filepath.ToSlash(rel))
			if strings.Contains(s.first, string(filepath.Separator)+string(filepath.Separator)) {
				t.Errorf("doubled separator in %q", s.first)
			}
		}
		// A.PART10.rar sorts first (upper case) but a.part1.rar is still the set's first volume;
		// b.r00 / c.7z.002 / d.z01 are claimed by their sets, not reported on their own
		want := []string{"rar:a.part1.rar", "rar:b.rar", "7z:c.7z", "7z:c.7z.001", "zip:d.zip", "zip:e.zip.001", "7z:sub/f.7z", "rar:sub/deeper/g.rar"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("findArchiveSets(%q) = %v, want %v", top, got, want)
		}
	}
	res := ExtractFolder(filepath.Join(dir, "does-not-exist"), nil, "", true, "", "")
	if b, _ := json.Marshal(res); string(b) != `{"sets":0,"ok":[],"failed":{}}` {
		t.Errorf("empty result marshals as %s", b)
	}
}

func TestFindTools(t *testing.T) {
	sz := Find7z()
	if sz == "" {
		t.Skip("7-Zip not found")
	}
	if !isFile(sz) {
		t.Fatalf("Find7z() = %q is not a file", sz)
	}
	if got := find7z(filepath.Join(t.TempDir(), "nope.exe")); got != sz {
		t.Errorf("missing configured path: %q, want fallback %q", got, sz)
	}
	own := filepath.Join(t.TempDir(), "my7z.exe")
	writeFile(t, own, nil)
	if got := find7z(own); got != own {
		t.Errorf("configured path: %q", got)
	}
	if got := findUnrar(own); got != own {
		t.Errorf("configured unrar path: %q", got)
	}
	if ur := FindUnrar(); ur != "" && !isFile(ur) {
		t.Errorf("FindUnrar() = %q is not a file", ur)
	}
}

func TestPathHelpers(t *testing.T) {
	sep := string(filepath.Separator)
	cases := [][3]string{ // dir, name, join
		{"", "a", "a"},
		{"x", "a", "x" + sep + "a"},
		{"x/", "a", "x/a"},
	}
	dirs := [][2]string{ // path, dirname
		{"a", ""},
		{"x" + sep + "a", "x"},
		{"x" + sep + sep + "a", "x"},
		{sep + "a", sep},
	}
	if runtime.GOOS == "windows" {
		cases = append(cases, [][3]string{{`C:`, "a", `C:a`}, {`C:\`, "a", `C:\a`}, {`D:\x`, "a", `D:\x\a`},
			{`\\srv\share`, "a", `\\srv\share\a`}, {`x\`, "a", `x\a`}}...)
		dirs = append(dirs, [][2]string{{`C:\a`, `C:\`}, {`C:a`, `C:`}, {`D:\x\y\a.rar`, `D:\x\y`},
			{`\\srv\share\a`, `\\srv\share\`}, {`D:/x/a`, `D:/x`}, {`D:\x\..\y\a`, `D:\x\..\y`}}...)
	}
	for _, c := range cases {
		if got := pyJoin(c[0], c[1]); got != c[2] {
			t.Errorf("pyJoin(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
	for _, c := range dirs {
		if got := pyDirname(c[0]); got != c[1] {
			t.Errorf("pyDirname(%q) = %q, want %q", c[0], got, c[1])
		}
	}
}

func TestOutputTail(t *testing.T) {
	if got := outputTail("\x1c \r\nERROR: x\r\nline2\rline3\r\n\x1f\t"); got != "ERROR: x\nline2\nline3" {
		t.Errorf("got %q", got)
	}
	long := strings.Repeat("é", 300) + strings.Repeat("ab", 100) + "\n"
	if got := []rune(outputTail(decodeOutputInverse(long))); len(got) != 400 || string(got[:200]) != strings.Repeat("é", 200) {
		t.Errorf("tail is %d runes: %q", len(got), string(got))
	}
	if digitsLess("10", "9") || !digitsLess("9", "10") || !digitsLess("0009", "10") || digitsLess("\u0662", "\u0661") ||
		!digitsLess("12345678901234567890123", "123456789012345678901234") {
		t.Error("digitsLess")
	}
}

// decodeOutputInverse encodes s the way a child process would write it, so that
// decodeOutput gives s back (ANSI code page on Windows, UTF-8 elsewhere).
func decodeOutputInverse(s string) string {
	if runtime.GOOS != "windows" {
		return s
	}
	if decodeOutput([]byte{0xe9}) == "é" { // cp1252 and friends
		return strings.ReplaceAll(s, "é", "\xe9")
	}
	return s
}
