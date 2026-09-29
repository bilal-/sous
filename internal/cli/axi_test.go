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

	"github.com/bilal-/sous/internal/install"
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
		if code != 0 && !(v.name == "doctor" && code == 1) || strings.TrimSpace(out) == "" { // doctor: 1 means problems found
			t.Errorf("%s: %d %q %q", v.name, code, out, errs)
		}
		out, errs, code = f.runIn(p, v.name, "--json")
		var anyJSON any
		if code != 0 && !(v.name == "doctor" && code == 1) || json.Unmarshal([]byte(out), &anyJSON) != nil || anyJSON == nil {
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
	if out, _, _ := f.run(); !strings.Contains(out, "nothing checked") {
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
	if code != 0 || !strings.Contains(out, "sous setup") || errs != "" {
		t.Fatalf("%d %q %q", code, out, errs)
	}
}

// docs/commands.md covers every command and every option in the verb
// table, so the guide cannot fall behind the code.
func TestCommandGuideCoversEveryCommandAndOption(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "commands.md"))
	if err != nil {
		t.Fatal(err)
	}
	guide := string(b)
	for _, v := range verbs {
		// The command's own section: from its "### `sous <name>" heading to
		// the next heading. Internal doors are covered in one table.
		heading := "### `sous " + v.name
		at := strings.Index(guide, heading)
		if v.args == nil {
			if !strings.Contains(guide, "`sous "+v.name) {
				t.Errorf("docs/commands.md does not cover sous %s", v.name)
			}
			continue
		}
		if at < 0 {
			t.Errorf("docs/commands.md has no section for sous %s", v.name)
			continue
		}
		section := guide[at:]
		if next := strings.Index(section[len(heading):], "\n##"); next >= 0 {
			section = section[:len(heading)+next]
		}
		for _, group := range [][]string{v.args.bools, v.args.values} {
			for _, names := range group {
				for _, n := range strings.Split(names, "|") {
					flag := "--" + n
					if len(n) == 1 {
						flag = "-" + n
					}
					if !strings.Contains(section, "`"+flag) {
						t.Errorf("the sous %s section of docs/commands.md does not cover %s", v.name, flag)
					}
				}
			}
		}
	}
	for _, mode := range []string{"--ambient", "--cached", "--refresh", "--menubar", "--json", "--brief"} {
		if !strings.Contains(guide, "`sous "+mode+"`") && !strings.Contains(guide, "**`"+mode+"`**") {
			t.Errorf("docs/commands.md does not cover %s", mode)
		}
	}
}

// With no roots yet, a new shell says once what to do, then stays quiet
// for the refresh window, instead of "building" on every shell.
func TestAmbientWithNoRootsHintsOnce(t *testing.T) {
	f := fixture(t)
	os.Remove(filepath.Join(f.SousHome, "config.toml"))
	out, _, code := f.run("--ambient")
	if code != 0 || !strings.Contains(out, "sous setup") || strings.Contains(out, "building") {
		t.Fatalf("%d %q", code, out)
	}
	if out, _, _ := f.run("--ambient"); out != "" {
		t.Fatalf("second shell: %q", out)
	}
}

// The skill only says sous exists and when to use it; the CLI teaches the
// rest. A skill that repeats commands drifts every release.
func TestSkillIsBarebonesAndHelpCarriesTheRules(t *testing.T) {
	if lines := strings.Count(install.Skill, "\n"); lines > 15 {
		t.Fatalf("the skill has %d lines; keep it to when to use sous, and point at sous help", lines)
	}
	if strings.Contains(install.Skill, "sous note -p") || strings.Contains(install.Skill, "--close") {
		t.Fatal("commands belong in sous help, not the skill")
	}
	f := fixture(t)
	out, _, _ := f.run("help")
	for _, rule := range []string{"For agents", "ask the user", "sous file", "never pick", "SOUS_SOURCE=agent", "FOLLOWUPS.md"} {
		if !strings.Contains(out, rule) {
			t.Errorf("sous help is missing %q", rule)
		}
	}
}

// AXI 10: a command's --help names every option it takes.
func TestAXI10HelpNamesEveryOption(t *testing.T) {
	for _, v := range verbs {
		if v.args == nil {
			continue
		}
		for _, group := range [][]string{v.args.bools, v.args.values} {
			for _, names := range group {
				n := strings.Split(names, "|")[0]
				flag := "--" + n
				if len(n) == 1 {
					flag = "-" + n
				}
				if !strings.Contains(v.usage, flag) {
					t.Errorf("sous %s --help does not mention %s", v.name, flag)
				}
			}
		}
	}
	f := fixture(t)
	if out, _, _ := f.run("help"); !strings.Contains(out, "--ambient") {
		t.Error("sous help does not mention --ambient")
	}
}
