package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoteCLI(t *testing.T) {
	f := fixture(t)
	w := f.mkrepo("studio/billing", true)
	sb := f.mkrepo("acme/chime", true)
	f.git(sb, "remote", "add", "origin", "git@github.com:acme/chime.git")

	out, _, code := f.runIn(w, "note", "-k", "me", "need final copy for pricing")
	if code != 0 || strings.TrimSpace(out) != "1" {
		t.Fatalf("note: %d %q", code, out)
	}
	out, _, code = f.runIn(w, "note", "-p", "chime", "notifications need context")
	if code != 0 || strings.TrimSpace(out) != "2" {
		t.Fatalf("note -p: %d %q", code, out)
	}
	t.Setenv("SOUS_SOURCE", "agent")
	f.runIn(w, "note", "-p", "chime", "from agent")
	t.Setenv("SOUS_SOURCE", "")
	b, _ := os.ReadFile(filepath.Join(f.SousHome, "threads.json"))
	for _, want := range []string{`"kind": "me"`, `"kind": "idea"`, `"source": "agent"`, `"remote": "github.com/acme/chime"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("threads.json missing %s:\n%s", want, b)
		}
	}
	for _, c := range [][]string{{"note", "-k", "urgent", "x"}, {"note", ""}, {"kind", "2", "later"}} {
		if _, _, code := f.runIn(w, c...); code != 2 {
			t.Errorf("%v should exit 2, got %d", c, code)
		}
	}
	if _, _, code := f.runIn(f.Home, "note", "x"); code != 2 {
		t.Error("outside project without -p should exit 2")
	}
	for _, c := range [][]string{{"edit", "1", "chase Friday"}, {"kind", "2", "me"}, {"snooze", "2", "3"}, {"done", "1"}} {
		if _, errs, code := f.run(c...); code != 0 {
			t.Errorf("%v: %d %s", c, code, errs)
		}
	}
	if _, _, code := f.run("done", "1"); code != 1 {
		t.Error("done twice should exit 1")
	}
	if _, _, code := f.run("done", "99"); code != 1 {
		t.Error("done missing should exit 1")
	}
}

func TestSignalFrontDoor(t *testing.T) {
	f := fixture(t)
	r := f.mkrepo("a/r", true)
	os.WriteFile(filepath.Join(r, "x"), nil, 0o644)
	out, _, code := f.runStdin(r+"\n", "signal", "git", "scan")
	if code != 0 || !strings.Contains(out, "1 files uncommitted") {
		t.Fatalf("signal git scan: %d %q", code, out)
	}
	if _, _, code := f.run("signal", "nope", "scan"); code != 2 {
		t.Fatal("unknown plugin should exit 2")
	}
}

func TestHereCLI(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("oss/ios-app", true)
	f.git(p, "checkout", "-q", "-b", "feature/widget")
	f.git(p, "commit", "-q", "--allow-empty", "-m", "Wire the home widget")
	f.runIn(p, "note", "home screen layout")
	f.runIn(p, "note", "-k", "me", "ask Sam about cert")

	out, _, code := f.run("here", p)
	if code != 0 || !strings.Contains(out, "ios-app · feature/widget ·") || !strings.Contains(out, "feature/widget has no upstream") || !strings.Contains(out, "Wire the home widget") || !strings.Contains(out, "ideas: 1") || !strings.Contains(out, "ask Sam") {
		t.Fatalf("here: %d\n%s", code, out)
	}
	if out, _, _ = f.runIn(filepath.Join(p, ".git", ".."), "here"); !strings.Contains(out, "ios-app ·") {
		t.Fatalf("here from cwd:\n%s", out)
	}
	if _, _, code = f.runIn(f.Home, "here"); code != 2 {
		t.Fatal("here outside a project should exit 2")
	}
	if out, _, _ = f.run("ios"); !strings.Contains(out, "ios-app ·") {
		t.Fatalf("sous <name>:\n%s", out)
	}
	if out, _, _ = f.run(p); !strings.Contains(out, "ios-app ·") {
		t.Fatalf("sous <repo path>:\n%s", out)
	}
	if out, _, _ = f.run("--json", "here", p); !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Fatal("--json here")
	}
}

func TestStoreFailureExitsOne(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("a/one", true)
	os.WriteFile(filepath.Join(f.SousHome, "threads.json"), []byte(`{"version":99,"threads":[]}`), 0o644)
	for _, c := range [][]string{{"note", "x"}, {"done", "1"}, {"edit", "1", "y"}, {"kind", "1", "me"}, {"snooze", "1"}} {
		if _, errs, code := f.runIn(p, c...); code != 1 || !strings.Contains(errs, "version 99") {
			t.Errorf("%v: code=%d err=%q", c, code, errs)
		}
	}
	b, _ := os.ReadFile(filepath.Join(f.SousHome, "threads.json"))
	if string(b) != `{"version":99,"threads":[]}` {
		t.Fatal("file must be untouched")
	}
}

func TestGlobalFlagsAfterVerb(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("a/one", true)
	if out, _, code := f.run("here", p, "--json"); code != 0 || !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Fatalf("here --json: %d %q", code, out)
	}
	if out, _, code := f.run("projects", "--json"); code != 0 || !strings.HasPrefix(strings.TrimSpace(out), "[") {
		t.Fatalf("projects --json: %d %q", code, out)
	}
}

func TestJSONOnWriteVerbIsAnError(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("a/r", true)
	for _, c := range [][]string{{"note", "--json", "x"}, {"--json", "done", "1"}, {"version", "--brief"}, {"--json", "setup"}} {
		if _, errs, code := f.runIn(p, c...); code != 2 || !strings.Contains(errs, "does not take") {
			t.Errorf("%v: code=%d err=%q", c, code, errs)
		}
	}
}

func TestAmbiguousProjectViaCLI(t *testing.T) {
	f := fixture(t)
	f.mkrepo("a/app-next", true)
	f.mkrepo("a/app-mobile", true)
	_, errs, code := f.runIn(f.Home, "note", "-p", "app-", "x")
	if code != 2 || !strings.Contains(errs, "matches 2 projects") || !strings.Contains(errs, "a/app-mobile") {
		t.Fatalf("code=%d err=%q", code, errs)
	}
	b, err := os.ReadFile(filepath.Join(f.SousHome, "threads.json"))
	if err == nil && strings.Contains(string(b), `"x"`) {
		t.Fatal("ambiguity must not write a note")
	}
}

func TestSousHomeDefault(t *testing.T) {
	t.Setenv("SOUS_HOME", "")
	t.Setenv("HOME", "/h")
	if got := sousHome(); got != "/h/.sous" {
		t.Fatalf("%q", got)
	}
}

// Review I2: capture must never depend on config being valid. A broken
// config.toml still lets `sous note` (no -p) save the note.
func TestNoteWorksWithBrokenConfig(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("a/r", true)
	f.writeConfig("roots = [\n")
	out, errs, code := f.runIn(p, "note", "-k", "me", "still saved")
	if code != 0 || strings.TrimSpace(out) != "1" {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	if _, errs, code := f.runIn(p, "note", "-p", "r", "needs roots"); code != 1 || !strings.Contains(errs, "config") {
		t.Fatalf("-p needs config, and says so: %d %q", code, errs)
	}
	if _, errs, code := f.run("done", "1"); code != 0 {
		t.Fatalf("closing a note needs no config either: %d %q", code, errs)
	}
}
