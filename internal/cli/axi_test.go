package cli

// The AXI guidelines (https://axi.md) as tests. sous follows them where they
// serve a person at a terminal as well as an agent; AGENTS.md lists the
// three deliberate departures. Most checks walk the verb table, so a new
// verb is held to them without anyone remembering to add a test. If one of
// these fails, fix the verb, not the test.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// readVerbs are the verbs that show something (they take --json).
func readVerbs() []verb {
	var vs []verb
	for _, v := range verbs {
		if v.read {
			vs = append(vs, v)
		}
	}
	return vs
}

// AXI 8: content first. Bare sous is live data, not help.
func TestAXI8ContentFirst(t *testing.T) {
	f := fixture(t)
	out, _, code := f.run()
	if code != 0 || !strings.HasPrefix(out, "sous · ") || strings.Contains(out, "usage") {
		t.Fatalf("%d\n%s", code, out)
	}
}

// AXI 4 and 5: every read verb answers in an empty world, with a count or
// a plain statement in text and real JSON (never null) with --json.
func TestAXI4And5ReadVerbsAnswerWhenEmpty(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/chime", true)
	for _, v := range readVerbs() {
		out, errs, code := f.runIn(p, v.name)
		if code != 0 || strings.TrimSpace(out) == "" {
			t.Errorf("%s: %d %q %q", v.name, code, out, errs)
		}
		out, errs, code = f.runIn(p, v.name, "--json")
		var anyJSON any
		if code != 0 || json.Unmarshal([]byte(out), &anyJSON) != nil || anyJSON == nil {
			t.Errorf("%s --json: %d %q %q", v.name, code, out, errs)
		}
	}
}

// AXI 6: nothing ever waits for input. Every verb, given nothing on stdin,
// finishes on its own.
func TestAXI6NeverPrompts(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/chime", true)
	for _, v := range verbs {
		if v.args == nil || v.name == "setup" || v.name == "go" {
			continue // doors are protocol; setup writes real dotfiles; go execs
		}
		done := make(chan struct{})
		go func() { f.runIn(p, v.name); close(done) }()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			t.Fatalf("%s waited for input", v.name)
		}
	}
}

// AXI 9: output points at the next step, as a command with placeholders.
func TestAXI9NextStepHints(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/chime", true)
	f.runIn(p, "note", "-k", "me", "x")
	for _, c := range [][]string{{}, {"here"}} {
		out, _, _ := f.runIn(p, c...)
		if !strings.Contains(out, "sous ") || !strings.Contains(out, "<") && !strings.Contains(out, "\"…\"") {
			t.Errorf("sous %v gives no next step:\n%s", c, out)
		}
	}
}

// AXI 6: fail loud on unknown flags, with the verb's usage.
func TestAXI6UnknownFlagsFailLoud(t *testing.T) {
	f := fixture(t)
	for _, v := range verbs {
		if v.args == nil || v.args.raw {
			continue // internal doors speak a protocol; raw verbs take text
		}
		_, errs, code := f.run(v.name, "--frobnicate")
		if code != 2 || !strings.Contains(errs, "--frobnicate") || !strings.Contains(errs, "usage: sous "+v.name) {
			t.Errorf("%s: code=%d err=%q", v.name, code, errs)
		}
	}
}

// AXI 10: every verb has its own short help, and asking is not an error.
func TestAXI10EveryVerbHasHelp(t *testing.T) {
	f := fixture(t)
	for _, v := range verbs {
		if v.usage == "" {
			continue
		}
		for _, h := range []string{"--help", "-h"} {
			out, errs, code := f.run(v.name, h)
			if code != 0 || !strings.Contains(out, "usage: "+v.synopsis()) || errs != "" {
				t.Errorf("%s %s: %d %q %q", v.name, h, code, out, errs)
			}
		}
	}
}

// AXI 5: empty is said, never left blank; --json empty is [], not null.
func TestAXI5EmptyStatesAreDefinitive(t *testing.T) {
	f := fixture(t)
	if out, _, code := f.run("projects"); code != 0 || !strings.Contains(out, "0 projects") {
		t.Fatalf("%d %q", code, out)
	}
	if out, _, _ := f.run("projects", "--json"); strings.TrimSpace(out) != "[]" {
		t.Fatalf("%q", out)
	}
	if out, _, _ := f.run(); !strings.Contains(out, "no projects under") || !strings.Contains(out, "config.toml") {
		t.Fatalf("a fresh board must say what to do:\n%s", out)
	}
}

// AXI 7: the skill is on demand for any agent, not only the two sous
// installs hooks for. Printing it touches nothing.
func TestAXI7SkillForAnyAgent(t *testing.T) {
	f := fixture(t)
	out, _, code := f.run("setup", "--print-skill")
	if code != 0 || !strings.HasPrefix(out, "---\nname: sous") {
		t.Fatalf("%d %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(f.Home, ".claude")); err == nil {
		t.Fatal("--print-skill must not install anything")
	}
}

// A first run with no config is a welcome, not an error.
func TestFirstRunWelcomes(t *testing.T) {
	f := fixture(t)
	os.Remove(filepath.Join(f.SousHome, "config.toml"))
	out, errs, code := f.run()
	if code != 0 || !strings.Contains(out, "roots") || !strings.Contains(out, "config.toml") || errs != "" {
		t.Fatalf("%d %q %q", code, out, errs)
	}
}
