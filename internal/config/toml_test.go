package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Review: an escaped quote run (\""") inside a multi-line basic string does
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
