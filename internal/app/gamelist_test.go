package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Merging into a game list only adds: other games, their favourites and play counts, and
// anything it doesn't know stay byte for byte; a game the list has already keeps its entry,
// with only the fields it hadn't got added.
func TestApplyGameList(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "roms", "snes", "gamelist.xml")
	_ = os.MkdirAll(filepath.Dir(f), 0o755)
	orig := `<?xml version="1.0"?>
<gameList>
	<folder>
		<path>./subfolder</path>
		<name>Hacks</name>
	</folder>
	<folder>
		<path>./Mega Man X (USA).sfc</path>
		<name>a folder named like a game we add</name>
	</folder>
	<game id="123" source="ScreenScraper.fr">
		<path>./Chrono Trigger (USA).sfc</path>
		<name>Chrono Trigger</name>
		<favorite>true</favorite>
		<playcount>42</playcount>
		<unknownthing a="b">keep me</unknownthing>
	</game>
	<game>
		<path>./Super Metroid (USA).sfc</path>
		<name>my own name</name>
		<playcount>7</playcount>
	</game>
</gameList>
`
	if err := os.WriteFile(f, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	ed := GameListEdit{Kind: "es", File: f, Games: []GameEntry{
		{Key: "./Super Metroid (USA).sfc", Tags: [][2]string{{"path", "./Super Metroid (USA).sfc"}, {"name", "Super Metroid"}, {"desc", "Samus & the <baby> metroid"}, {"image", "./images/Super Metroid (USA)-image.png"}}},
		{Key: "./Mega Man X (USA).sfc", Tags: [][2]string{{"path", "./Mega Man X (USA).sfc"}, {"name", "Mega Man X"}}},
		{Key: "./Never Arrived (USA).sfc", Tags: [][2]string{{"path", "./Never Arrived (USA).sfc"}, {"name", "Never Arrived"}}},
	}, Needs: []string{"a", "b", "c"}}
	n, err := ApplyGameList(ed, func(p string) bool { return p != "c" })
	if err != nil || n != 2 {
		t.Fatalf("applied %d, %v", n, err)
	}
	got, _ := os.ReadFile(f)
	s := string(got)
	for _, keep := range []string{`<game id="123" source="ScreenScraper.fr">`, "<favorite>true</favorite>", "<playcount>42</playcount>",
		`<unknownthing a="b">keep me</unknownthing>`, "<folder>", "<path>./subfolder</path>"} {
		if !strings.Contains(s, keep) {
			t.Errorf("lost %q:\n%s", keep, s)
		}
	}
	if !strings.Contains(s, "<name>my own name</name>") || strings.Contains(s, "<name>Super Metroid</name>") || !strings.Contains(s, "<playcount>7</playcount>") ||
		strings.Count(s, "Super Metroid (USA).sfc</path>") != 1 {
		t.Errorf("Super Metroid's own entry not kept as it was:\n%s", s)
	}
	if !strings.Contains(s, "<desc>Samus &amp; the &lt;baby&gt; metroid</desc>") || !strings.Contains(s, "<image>./images/Super Metroid (USA)-image.png</image>") {
		t.Errorf("the fields Super Metroid's entry lacked weren't added:\n%s", s)
	}
	if !strings.Contains(s, "<name>Mega Man X</name>") || !strings.Contains(s, "<name>a folder named like a game we add</name>") {
		t.Errorf("a new game, beside a <folder> of the same path:\n%s", s)
	}
	if strings.Contains(s, "Never Arrived") {
		t.Error("an entry for a file that didn't arrive")
	}
	if b, _ := os.ReadFile(f + ".usnfetch-backup"); string(b) != orig {
		t.Error("no backup of the list as it was")
	}
	// again: still one entry per game
	if _, err := ApplyGameList(ed, nil); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(f)
	if c := strings.Count(string(got), "<name>Mega Man X</name>"); c != 1 || strings.Count(string(got), "<desc>") != 1 {
		t.Errorf("Mega Man X %d times after a second merge:\n%s", c, got)
	}

	// a list that isn't there yet; LaunchBox's kind
	lb := filepath.Join(dir, "Data", "Platforms", "Super Nintendo Entertainment System.xml")
	n, err = ApplyGameList(GameListEdit{Kind: "launchbox", File: lb, Games: []GameEntry{{Key: `Games\Super Nintendo Entertainment System\Super Metroid (USA).sfc`,
		Tags: [][2]string{{"ID", "x-1"}, {"ApplicationPath", `Games\Super Nintendo Entertainment System\Super Metroid (USA).sfc`}, {"Title", "Super Metroid"}}}}}, nil)
	if err != nil || n != 1 {
		t.Fatalf("new LaunchBox list: %d, %v", n, err)
	}
	got, _ = os.ReadFile(lb)
	if !strings.HasPrefix(string(got), `<?xml version="1.0" standalone="yes"?>`) || !strings.Contains(string(got), "<Title>Super Metroid</Title>") || !strings.HasSuffix(string(got), "</LaunchBox>\n") {
		t.Errorf("new LaunchBox list:\n%s", got)
	}

	// a list that isn't XML is left alone
	bad := filepath.Join(dir, "bad.xml")
	_ = os.WriteFile(bad, []byte("<notalist><game>"), 0o644)
	if _, err := ApplyGameList(GameListEdit{Kind: "es", File: bad, Games: []GameEntry{{Key: "x", Tags: [][2]string{{"path", "x"}}}}}, nil); err == nil {
		t.Error("merged into something that isn't a game list")
	}
	if b, _ := os.ReadFile(bad); string(b) != "<notalist><game>" {
		t.Error("changed a file it couldn't read")
	}
}
