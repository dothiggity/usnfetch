package catalog

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// testLink is test_catalog.link(n): a wire-v2 share link whose folder id is n.
func testLink(n int) string {
	canonical := fmt.Sprintf("usenet://m%04dabcdefghijklmnop@host.x/%032x/%s#%s",
		n, n, strings.Repeat("a", 64), strings.Repeat("b", 64))
	return "usn://" + base64.RawURLEncoding.EncodeToString(append([]byte{2}, canonical...))
}

func testLinks() []string {
	l := make([]string, 12)
	for i := range l {
		l[i] = testLink(i)
	}
	return l
}

func sizeStr(p *int64) string {
	if p == nil {
		return "nil"
	}
	return fmt.Sprint(*p)
}

func byLink(es []Entry) map[string]Entry {
	m := make(map[string]Entry, len(es))
	for _, e := range es {
		m[e.Link] = e
	}
	return m
}

// --- ported from test_catalog.py ---------------------------------------------

func exportedLayout(l []string) []Entry {
	return ParseText(fmt.Sprintf(`USN Copy Links - portal
Extracted from HAR capture

=====
SOURCE: shopA  (3 builds, 5 links)
=====

XBOX Master Game Collection [CCI]
SKU: XBOXDL
%[1]s

45TB RetroMAX DOWNLOAD For Windows PC - Fully Loaded - Plug and Play  [45TB]
SKU: 45TBRBDL
Drive R (Main) - 1 of 3:
%[2]s
Drive S - 2 of 3:
%[3]s

1TB Retrobat for ROG Ally  [1TB]
SKU: 1TBRB
%[4]s

=====
SOURCE: shopB  (2 builds, 2 links)
=====

1TB Retrobat for Legion Go  [1TB]
SKU: 1TBRBLG
%[4]s

All Guns Blazing v43: 420 Light Gun Games  [512GB]
Info: https://example.invalid/agb
512GB-AllGunsBlazingV43-LightGunLunatics-PC-Batocera:
%[5]s
`, l[0], l[1], l[2], l[3], l[4]), "")
}

// Exported layout: sections, SKU/Info, labelled links, non-size brackets, a link listed twice.
func TestExportedLayout(t *testing.T) {
	l := testLinks()
	es := exportedLayout(l)
	if len(es) != 5 {
		labels := make([]string, len(es))
		for i, e := range es {
			labels[i] = e.Label()
		}
		t.Fatalf("got %d entries, want 5: %q", len(es), labels)
	}
	by := byLink(es)
	x := by[l[0]]
	if x.Title != "XBOX Master Game Collection [CCI]" || x.Listed != "" || x.Size != nil || x.SKU != "XBOXDL" || x.Source != "shopA" {
		t.Errorf("xbox entry: %+v (size %s)", x, sizeStr(x.Size))
	}
	r, s := by[l[1]], by[l[2]]
	if r.Variant != "Drive R (Main) - 1 of 3" || s.Variant != "Drive S - 2 of 3" || r.Listed != "45TB" || r.Size != nil {
		t.Errorf("drive entries: %+v / %+v", r, s)
	}
	rb := by[l[3]]
	if !slices.Equal(rb.Also, []string{"1TB Retrobat for Legion Go"}) || sizeStr(rb.Size) != "1000000000000" || rb.Source != "shopA" {
		t.Errorf("retrobat entry: %+v (size %s)", rb, sizeStr(rb.Size))
	}
	agb := by[l[4]]
	if sizeStr(agb.Size) != "512000000000" || agb.Info != "https://example.invalid/agb" || agb.Source != "shopB" {
		t.Errorf("agb entry: %+v (size %s)", agb, sizeStr(agb.Size))
	}
	if agb.Title != "All Guns Blazing v43: 420 Light Gun Games" {
		t.Errorf("agb title %q", agb.Title)
	}
	for _, e := range es {
		if e.Also == nil {
			t.Errorf("%s: Also is nil", e.Label())
		}
	}
}

// Job names: long labels stand alone, short ones follow the (shortened) title, siblings stay distinct.
func TestJobNames(t *testing.T) {
	l := testLinks()
	by := byLink(exportedLayout(l))
	if got := JobName(by[l[4]], 90); got != "512GB-AllGunsBlazingV43-LightGunLunatics-PC-Batocera" {
		t.Errorf("agb job name %q", got)
	}
	nr, ns := JobName(by[l[1]], 90), JobName(by[l[2]], 90)
	if !strings.HasSuffix(nr, "- Drive R (Main) - 1 of 3") || !strings.HasSuffix(ns, "- Drive S - 2 of 3") || nr == ns {
		t.Errorf("sibling job names %q / %q", nr, ns)
	}
	if max(len([]rune(nr)), len([]rune(ns))) > 90 {
		t.Errorf("job names longer than 90: %q / %q", nr, ns)
	}
	if got := JobName(by[l[0]], 90); got != "XBOX Master Game Collection [CCI]" {
		t.Errorf("xbox job name %q", got)
	}
}

func TestFallbackFormats(t *testing.T) {
	l := testLinks()
	// plain list of bare links: one entry each, named from the share's folder id
	es := ParseText(strings.Join(l[5:8], "\n"), "")
	if len(es) != 3 {
		t.Fatalf("plain list: %d entries", len(es))
	}
	for i, e := range es {
		if e.Link != l[5+i] || !strings.HasPrefix(e.Title, "USN_") {
			t.Errorf("plain list entry %d: %q %q", i, e.Link, e.Title)
		}
	}
	// "title | link" rows and "title\nlink" pairs without blank lines; a "label:" line after a
	// link names another link of the same listing (like "32GB Wii SD:" in exported lists)
	es = ParseText(fmt.Sprintf("Game One [4GB] | %s\nGame Two | %s\nGame Three\n%s\nDisc 2:\n%s", l[8], l[9], l[10], l[11]), "")
	want := [][3]string{{"Game One", "4GB", ""}, {"Game Two", "", ""}, {"Game Three", "", ""}, {"Game Three", "", "Disc 2"}}
	got := make([][3]string, len(es))
	for i, e := range es {
		got[i] = [3]string{e.Title, e.Listed, e.Variant}
	}
	if !slices.Equal(got, want) {
		t.Errorf("rows: got %q, want %q", got, want)
	}
	// saved HTML
	es = ParseText(fmt.Sprintf(`<ul><li><a href="%s">Game &amp; Watch</a></li></ul>`, l[8]), "")
	if len(es) != 1 || es[0].Link != l[8] || es[0].Title != "Game & Watch" {
		t.Errorf("html: %+v", es)
	}
}

// --- golden output of the earlier Python implementation (its generator is archived) --

type goldenEntry struct {
	Link     string   `json:"link"`
	Title    string   `json:"title"`
	Variant  string   `json:"variant"`
	Listed   string   `json:"listed"`
	Size     *int64   `json:"size"`
	Source   string   `json:"source"`
	SKU      string   `json:"sku"`
	Info     string   `json:"info"`
	Also     []string `json:"also"`
	JobName  string   `json:"job_name"`
	Label    string   `json:"label"`
	Haystack string   `json:"haystack"`
}

func toGolden(e Entry) goldenEntry {
	return goldenEntry{e.Link, e.Title, e.Variant, e.Listed, e.Size, e.Source, e.SKU, e.Info, e.Also,
		JobName(e, 90), e.Label(), e.Haystack()}
}

func readJSON(t *testing.T, name string, v any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

func compareEntries(t *testing.T, got []Entry, want []goldenEntry) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("got %d entries, want %d", len(got), len(want))
	}
	for i := range min(len(got), len(want)) {
		g, w := toGolden(got[i]), want[i]
		check := func(field, gv, wv string) {
			if gv != wv {
				t.Errorf("entry %d (%s) %s:\n got %q\nwant %q", i, w.Link, field, gv, wv)
			}
		}
		check("link", g.Link, w.Link)
		check("title", g.Title, w.Title)
		check("variant", g.Variant, w.Variant)
		check("listed", g.Listed, w.Listed)
		check("size", sizeStr(g.Size), sizeStr(w.Size))
		check("source", g.Source, w.Source)
		check("sku", g.SKU, w.SKU)
		check("info", g.Info, w.Info)
		check("also", fmt.Sprintf("%q", g.Also), fmt.Sprintf("%q", w.Also))
		check("job_name", g.JobName, w.JobName)
		check("label", g.Label, w.Label)
		check("haystack", g.Haystack, w.Haystack)
		if g.Also == nil {
			t.Errorf("entry %d: Also is nil", i)
		}
	}
}

// TestGolden parses made-up link lists (a portal-style export and the fallback formats)
// exactly as Catalog.import_file reads them and compares every field.
func TestGolden(t *testing.T) {
	for _, file := range []string{"portal_export.txt", "plain_links.txt", "rows.csv", "saved_page.html",
		"mixed_layout.txt", "legacy_bytes.txt"} {
		name := strings.TrimSuffix(file, filepath.Ext(file))
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", file))
			if err != nil {
				t.Fatal(err)
			}
			var want []goldenEntry
			readJSON(t, name+".golden.json", &want)
			compareEntries(t, ParseText(DecodeListFile(data), name), want)
		})
	}
}

type strCase struct {
	In  string `json:"in"`
	Out string `json:"out"`
}

type helpersGolden struct {
	ParseSize []struct {
		In  string       `json:"in"`
		Out *json.Number `json:"out"`
	} `json:"parse_size"`
	FmtDec []struct {
		In  float64 `json:"in"`
		Out string  `json:"out"`
	} `json:"fmt_dec"`
	Unescape      []strCase `json:"unescape"`
	CleanInline   []strCase `json:"clean_inline"`
	FallbackTitle []strCase `json:"fallback_title"`
	B64           []struct {
		In  string  `json:"in"`
		Out *string `json:"out"`
	} `json:"b64"`
	JobName []struct {
		Title   string `json:"title"`
		Variant string `json:"variant"`
		Link    string `json:"link"`
		Limit   int    `json:"limit"`
		JobName string `json:"job_name"`
	} `json:"job_name"`
	Lower    []strCase `json:"lower"`
	ParseRaw []struct {
		Text    string        `json:"text"`
		Source  string        `json:"source"`
		Entries []goldenEntry `json:"entries"`
	} `json:"parse_raw"`
	Decode []struct {
		Hex  string `json:"hex"`
		Text string `json:"text"`
	} `json:"decode"`
}

func TestHelpersGolden(t *testing.T) {
	var h helpersGolden
	readJSON(t, "helpers.golden.json", &h)

	t.Run("parse_size", func(t *testing.T) {
		for _, c := range h.ParseSize {
			want := "nil"
			if c.Out != nil {
				want = c.Out.String()
				// Python's int is unbounded; ParseSize clamps to int64.
				if v, ok := new(big.Int).SetString(want, 10); ok && !v.IsInt64() {
					want = fmt.Sprint(int64(math.MaxInt64))
				}
			}
			if got := sizeStr(ParseSize(c.In)); got != want {
				t.Errorf("ParseSize(%q) = %s, want %s", c.In, got, want)
			}
		}
	})
	t.Run("fmt_dec", func(t *testing.T) {
		for _, c := range h.FmtDec {
			if got := FmtDec(c.In); got != c.Out {
				t.Errorf("FmtDec(%v) = %q, want %q", c.In, got, c.Out)
			}
		}
	})
	strFuncs := []struct {
		name  string
		cases []strCase
		fn    func(string) string
	}{
		{"unescape", h.Unescape, pyUnescape},
		{"clean_inline", h.CleanInline, cleanInline},
		{"fallback_title", h.FallbackTitle, fallbackTitle},
		{"lower", h.Lower, pyLower},
	}
	for _, f := range strFuncs {
		t.Run(f.name, func(t *testing.T) {
			if len(f.cases) == 0 {
				t.Fatal("no cases")
			}
			for _, c := range f.cases {
				if got := f.fn(c.In); got != c.Out {
					t.Errorf("%s(%q) = %q, want %q", f.name, c.In, got, c.Out)
				}
			}
		})
	}
	t.Run("b64", func(t *testing.T) {
		for _, c := range h.B64 {
			got := "error"
			if b, ok := pyB64Decode(c.In); ok {
				got = hex.EncodeToString(b)
			}
			want := "error"
			if c.Out != nil {
				want = *c.Out
			}
			if got != want {
				t.Errorf("urlsafe_b64decode(%q) = %s, want %s", c.In, got, want)
			}
		}
	})
	t.Run("job_name", func(t *testing.T) {
		for _, c := range h.JobName {
			e := Entry{Link: c.Link, Title: c.Title, Variant: c.Variant}
			if got := JobName(e, c.Limit); got != c.JobName {
				t.Errorf("JobName(%q, %q, %d) = %q, want %q", c.Title, c.Variant, c.Limit, got, c.JobName)
			}
		}
	})
	t.Run("parse_raw", func(t *testing.T) {
		for i, c := range h.ParseRaw {
			t.Run(fmt.Sprint(i), func(t *testing.T) { compareEntries(t, ParseText(c.Text, c.Source), c.Entries) })
		}
	})
	t.Run("decode", func(t *testing.T) {
		for _, c := range h.Decode {
			b, _ := hex.DecodeString(c.Hex)
			if got := DecodeListFile(b); got != c.Text {
				t.Errorf("DecodeListFile(%s) = %q, want %q", c.Hex, got, c.Text)
			}
		}
	})
}

// Values JSON cannot carry; expectations checked against Python's fmt_dec.
func TestFmtDecSpecial(t *testing.T) {
	for _, c := range []struct {
		in   float64
		want string
	}{{math.NaN(), "nan PB"}, {math.Inf(1), "inf PB"}, {math.Copysign(0, -1), "0 B"}} {
		if got := FmtDec(c.in); got != c.want {
			t.Errorf("FmtDec(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEntryJSON(t *testing.T) {
	es := ParseText(testLink(1), "src")
	b, err := json.Marshal(es[0])
	if err != nil {
		t.Fatal(err)
	}
	want := `{"link":"` + testLink(1) + `","title":"USN_000000000000","variant":"","listed":"","size":null,` +
		`"source":"src","sku":"","info":"","also":[],"added":0}`
	if string(b) != want {
		t.Errorf("JSON:\n got %s\nwant %s", b, want)
	}
	if got := ParseText("", ""); got == nil || len(got) != 0 {
		t.Errorf("ParseText(\"\") = %#v, want empty non-nil slice", got)
	}
	if got := (Entry{Title: "T"}).Label(); got != "T" {
		t.Errorf("Label() = %q", got)
	}
	if got := (Entry{Title: "T", Variant: "V"}).Label(); got != "T \u2014 V" {
		t.Errorf("Label() = %q", got)
	}
}
