package app

import "testing"

// topFolder works out the same folder as commonTop, a path at a time.
func TestTopFolderMatchesCommonTop(t *testing.T) {
	cases := [][]string{
		{},
		{"a/b.txt"},
		{"a/b.txt", "a/c/d.txt"},
		{"a/b.txt", "b/c.txt"},
		{"x.txt"},
		{"a/b.txt", "x.txt"},
		{"x.txt", "a/b.txt"},
		{`a\b.txt`, "a/c.txt"},
		{"/a/b.txt", "a//c.txt"},
		{"a/"},
		{""},
		{"Retro Game Pack/Games/N64/Mario.z64", "Retro Game Pack/BIOS/bios.zip"},
	}
	for _, c := range cases {
		var tf topFolder
		for _, p := range c {
			tf.add(p)
		}
		if got, want := tf.top(), commonTop(c); got != want {
			t.Errorf("%q: topFolder %q, commonTop %q", c, got, want)
		}
	}
}
