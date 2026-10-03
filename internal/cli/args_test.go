package cli

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseArgs(t *testing.T) {
	s := argSpec{bools: []string{"file"}, values: []string{"p", "k", "root", "a|agent"}, min: 0, max: 2}
	for _, c := range []struct {
		in   []string
		pos  []string
		want map[string][]string
	}{
		{[]string{"text", "-k", "me"}, []string{"text"}, map[string][]string{"k": {"me"}}},
		{[]string{"--k=them", "--file", "text"}, []string{"text"}, map[string][]string{"k": {"them"}, "file": {""}}},
		{[]string{"--agent", "codex", "proj"}, []string{"proj"}, map[string][]string{"a": {"codex"}}},
		{[]string{"-a=codex", "proj"}, []string{"proj"}, map[string][]string{"a": {"codex"}}},
		{[]string{"--root", "x", "--root", "y"}, nil, map[string][]string{"root": {"x", "y"}}},
		{[]string{"--", "-starts with a dash"}, []string{"-starts with a dash"}, map[string][]string{}},
	} {
		a, err := parseArgs(s, c.in)
		if err != nil {
			t.Fatalf("%v: %v", c.in, err)
		}
		if !reflect.DeepEqual(a.pos, c.pos) || !reflect.DeepEqual(a.set, c.want) {
			t.Errorf("%v: pos=%q set=%v", c.in, a.pos, a.set)
		}
	}
	for in, want := range map[string]string{
		"--bogus":    "unknown flag: --bogus",
		"-k":         "-k needs a value",
		"--file=yes": "--file takes no value",
		"a b c":      "too many arguments",
		"-x rest":    "unknown flag: -x",
	} {
		_, err := parseArgs(s, strings.Fields(in))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want %q", in, err, want)
		}
	}
	if _, err := parseArgs(argSpec{min: 1, max: 1}, nil); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Errorf("too few: %v", err)
	}
}

// Minors: text is text. -- protects --json too; edit and kind take
// no flags, so a dash-led text is just text; -k= is not a kind.
func TestTextIsNeverAFlag(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/chime", true)
	if out, errs, code := f.runIn(p, "note", "--", "--json"); code != 0 || !strings.HasPrefix(out, "noted 1 in ") {
		t.Fatalf("note -- --json: %d %q %q", code, out, errs)
	}
	if _, errs, code := f.run("edit", "1", "-2 regressions"); code != 0 {
		t.Fatalf("edit with dash text: %d %q", code, errs)
	}
	if th := threadJSON(t, f, 1); th["text"] != "-2 regressions" {
		t.Fatalf("%v", th["text"])
	}
	if _, _, code := f.runIn(p, "note", "-k=", "x"); code != 2 {
		t.Fatal("an explicit empty kind is a usage error, not an idea")
	}
}
