package config

import (
	"os"
	"path/filepath"
	"testing"
)

// An escaped quote run (\""") inside a multi-line basic string does
// not end it, so a roots line inside that string is never taken for the
// real one.
func TestSetRootsSkipsEscapedQuotesInMultiLineStrings(t *testing.T) {
	home := t.TempDir()
	in := "note = \"\"\"\nsay \\\"\"\"\nroots = [\"~/code\"]\n\"\"\"\nroots = [\"~/old\"]\n"
	os.WriteFile(filepath.Join(home, "config.toml"), []byte(in), 0o644)
	if err := SetRoots(home, "/h", []string{"~/code"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(home, "config.toml"))
	want := "note = \"\"\"\nsay \\\"\"\"\nroots = [\"~/code\"]\n\"\"\"\nroots = [\"~/code\"]\n"
	if string(b) != want {
		t.Fatalf("the string's text was changed instead of the setting:\n%s", b)
	}
}

// Roots are written as TOML strings, not Go strings, so
// unusual characters still make a file that parses.
func TestSetRootsQuotesForTOML(t *testing.T) {
	home := t.TempDir()
	odd := "/h/tab\there/del\x7fend"
	if err := SetRoots(home, "/h", []string{odd}); err != nil {
		t.Fatal(err)
	}
	c, err := Load(home, "/h")
	if err != nil || len(c.Roots) != 1 || c.Roots[0] != odd {
		t.Fatalf("%v %v", c, err)
	}
}
