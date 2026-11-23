package share

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// testdata/signed_manifests.json holds a v4 and a v5 manifest signed by test_rig.py's test
// publisher (a throwaway key, no real share), with the links that pin that key.
func TestSignedManifests(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "signed_manifests.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Link string
		Manifest   json.RawMessage
	}
	if err := json.Unmarshal(raw, &cases); err != nil || len(cases) != 2 {
		t.Fatalf("fixtures: %v (%d)", err, len(cases))
	}
	for _, c := range cases {
		link, err := ParseLink(c.Link)
		if err != nil {
			t.Fatal(err)
		}
		m, err := ParseManifest(c.Manifest)
		if err != nil {
			t.Fatal(err)
		}
		fp, err := VerifyManifestSignature(m, link.OwnerFP)
		if err != nil || !strings.HasPrefix(fp, link.OwnerFP) {
			t.Fatalf("%s: %v (fp %s, pin %s)", c.Name, err, fp, link.OwnerFP)
		}
		if st, err := CheckSignature(m, link); st != "verified" || err != nil {
			t.Errorf("%s: CheckSignature = %q, %v", c.Name, st, err)
		}
		if _, err := VerifyManifestSignature(m, strings.Repeat("ab", 32)); err == nil || !strings.Contains(err.Error(), "publisher key mismatch") {
			t.Errorf("%s: another publisher's pin: %v", c.Name, err)
		}
		unpinned := *link
		unpinned.OwnerFP = ""
		if _, err := CheckSignature(m, &unpinned); err == nil {
			t.Errorf("%s: a signed manifest was accepted through a link without a pin", c.Name)
		}
		tamper := map[string]func(Manifest){
			"created_at": func(m Manifest) { m["created_at"] = m.Str("created_at") + "x" },
			"access key": func(m Manifest) { m.Map("access")["folder_key_hex"] = strings.Repeat("00", 32) },
			"folder_id":  func(m Manifest) { m["folder_id"] = strings.Repeat("9", 32) },
		}
		if shards := m.List("shards"); shards != nil {
			tamper["shard digest"] = func(m Manifest) { m.List("shards")[0].(map[string]any)["digest"] = strings.Repeat("11", 32) }
		} else {
			tamper["index part"] = func(m Manifest) {
				m.Map("core_index")["parts"].([]any)[0].(map[string]any)["primary_msgid"] = "other@example"
			}
		}
		for what, f := range tamper {
			b, _ := json.Marshal(m)
			cp, _ := ParseManifest(b)
			f(cp)
			if _, err := VerifyManifestSignature(cp, link.OwnerFP); err == nil {
				t.Errorf("%s: tampered %s accepted", c.Name, what)
			}
		}
	}
}

func TestParseLink(t *testing.T) {
	fp64, fp32 := strings.Repeat("ab", 32), strings.Repeat("cd", 16)
	key, folder := strings.Repeat("0f", 32), strings.Repeat("12", 16)
	enc := func(version byte, s string) string {
		return "usn://" + base64.RawURLEncoding.EncodeToString(append([]byte{version}, s...))
	}
	cases := []struct {
		in   string
		want Link // Canonical not compared
		err  string
	}{
		{in: "usenet://m1@x+<m2@y>/" + folder + "/" + fp64 + "#" + key,
			want: Link{ManifestMsgids: []string{"m1@x", "m2@y"}, FolderID: folder, OwnerFP: fp64, KeyHex: key}},
		{in: enc(2, "usenet://alt.binaries.test/m1@x/"+folder+"/"+strings.ToUpper(fp32)),
			want: Link{Newsgroup: "alt.binaries.test", ManifestMsgids: []string{"m1@x"}, FolderID: folder, OwnerFP: fp32}},
		{in: "usenet://m1@x/" + folder + "/not-a-fingerprint#short",
			want: Link{ManifestMsgids: []string{"m1@x"}, FolderID: folder}},
		{in: "  usenet://m1@x//" + folder + "  ", want: Link{ManifestMsgids: []string{"m1@x"}, FolderID: folder}},
		{in: "https://example.com/x", err: "not a share link"},
		{in: "usn://AA", err: "too short"},
		{in: enc(2, "http://x/y"), err: "other than usenet://"},
		{in: enc(9, "usenet://x"), err: "unknown encoding"},
		{in: "usenet://", err: "empty after usenet://"},
		{in: "usenet://m1@x", err: "folder ID"},
	}
	for _, c := range cases {
		l, err := ParseLink(c.in)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("ParseLink(%q) error = %v, want %q", c.in, err, c.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseLink(%q): %v", c.in, err)
			continue
		}
		if !strings.HasPrefix(l.Canonical, "usenet://") {
			t.Errorf("ParseLink(%q).Canonical = %q", c.in, l.Canonical)
		}
		l.Canonical = ""
		if !reflect.DeepEqual(*l, c.want) {
			t.Errorf("ParseLink(%q)\n got %+v\nwant %+v", c.in, *l, c.want)
		}
	}
}

func TestPyJSON(t *testing.T) {
	d := json.NewDecoder(strings.NewReader(`{"b":"é/<>&\"\u0001😀","a":[1,2.5,true,null],"c":{"z":1,"y":"x"}}`))
	d.UseNumber()
	var v any
	_ = d.Decode(&v)
	var b bytes.Buffer
	pyJSON(&b, v)
	// as printed by Python: json.dumps(..., separators=(",", ":"), sort_keys=True)
	want := `{"a":[1,2.5,true,null],"b":"` + `\` + `u00e9/<>&\"` + `\` + `u0001` + `\` + `ud83d` + `\` + `ude00","c":{"y":"x","z":1}}`
	if b.String() != want {
		t.Fatalf("\n got %s\nwant %s", b.String(), want)
	}
}
