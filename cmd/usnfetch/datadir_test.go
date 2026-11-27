package main

import (
	"path/filepath"
	"testing"
)

// Settings says what chose the data folder: --data, USNFETCH_DATA, or the usual place.
func TestDataFrom(t *testing.T) {
	env, flagDir := t.TempDir(), t.TempDir()
	t.Setenv("USNFETCH_DATA", env)
	if o := parseServe("serve", nil); o.dataFrom != "env" || o.data != env {
		t.Errorf("USNFETCH_DATA: %q from %q", o.data, o.dataFrom)
	}
	if o := parseServe("serve", []string{"--data", flagDir}); o.dataFrom != "flag" || o.data != flagDir {
		t.Errorf("--data: %q from %q", o.data, o.dataFrom)
	}
	t.Setenv("USNFETCH_DATA", "")
	if o := parseServe("serve", nil); o.dataFrom != "default" || filepath.Base(o.data) != "usnfetch" {
		t.Errorf("no setting: %q from %q", o.data, o.dataFrom)
	}
}
