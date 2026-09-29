package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigShowSetUnset(t *testing.T) {
	f := fixture(t)
	f.mkrepo("acme/api", true)
	cfgFile := filepath.Join(f.SousHome, "config.toml")

	out, _, code := f.run("config")
	if code != 0 || !strings.Contains(out, "agent") || !strings.Contains(out, "claude (default)") || !strings.Contains(out, "roots") {
		t.Fatalf("show: %d\n%s", code, out)
	}
	if _, errs, code := f.run("config", "agent", "codex"); code != 0 {
		t.Fatalf("set agent: %d %q", code, errs)
	}
	if _, errs, code := f.run("config", "refresh_hours", "2"); code != 0 {
		t.Fatalf("set hours: %d %q", code, errs)
	}
	if _, errs, code := f.run("config", "ignore", "scratch/*", "tmp/*"); code != 0 {
		t.Fatalf("set list: %d %q", code, errs)
	}
	if _, errs, code := f.run("config", "-p", "api", "backend", "markdown"); code != 0 {
		t.Fatalf("set project by rough name: %d %q", code, errs)
	}
	if _, errs, code := f.run("config", "-p", "acme/*", "github_account", "work-account"); code != 0 {
		t.Fatalf("set org: %d %q", code, errs)
	}
	b, _ := os.ReadFile(cfgFile)
	for _, want := range []string{`agent = "codex"`, "refresh_hours = 2", `ignore = ["scratch/*", "tmp/*"]`, `[projects."acme/api"]`, `backend = "markdown"`, `[projects."acme/*"]`, `github_account = "work-account"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("config.toml lacks %s:\n%s", want, b)
		}
	}
	out, _, _ = f.run("config")
	if !strings.Contains(out, "codex") || strings.Contains(out, "codex (default)") || !strings.Contains(out, "acme/api") || !strings.Contains(out, "work-account") {
		t.Fatalf("show after: \n%s", out)
	}
	out, _, _ = f.run("config", "--json")
	var js struct {
		Settings map[string]struct {
			Value   any  `json:"value"`
			Default bool `json:"default"`
		} `json:"settings"`
		Projects map[string]map[string]string `json:"projects"`
	}
	if err := json.Unmarshal([]byte(out), &js); err != nil || js.Settings["agent"].Value != "codex" || js.Projects["acme/*"]["github_account"] != "work-account" {
		t.Fatalf("json: %v %s", err, out)
	}
	if _, _, code := f.run("config", "--unset", "agent"); code != 0 {
		t.Fatal("unset")
	}
	if _, _, code := f.run("config", "-p", "acme/*", "--unset", "github_account"); code != 0 {
		t.Fatal("unset in project")
	}
	b, _ = os.ReadFile(cfgFile)
	if strings.Contains(string(b), "agent =") || strings.Contains(string(b), "github_account") {
		t.Fatalf("%s", b)
	}
}

func TestConfigRefusesBadValues(t *testing.T) {
	f := fixture(t)
	f.mkrepo("acme/api", true)
	for _, c := range []struct {
		args []string
		says string
	}{
		{[]string{"config", "colour", "blue"}, "roots"},                  // unknown key lists the keys
		{[]string{"config", "agent", "nope"}, "claude"},                  // names the launchers
		{[]string{"config", "refresh_hours", "soon"}, "whole number"},    // not a number
		{[]string{"config", "refresh_hours", "0"}, "whole number"},       // not positive
		{[]string{"config", "agent", "a", "b"}, "one value"},             // too many
		{[]string{"config", "-p", "api", "backend", "jira"}, "markdown"}, // names the backends
		{[]string{"config", "-p", "nothing-like-it", "backend", "markdown"}, "no project"},
	} {
		if _, errs, code := f.run(c.args...); code != 2 || !strings.Contains(errs, c.says) {
			t.Errorf("%v: %d %q", c.args, code, errs)
		}
	}
	if b, err := os.ReadFile(filepath.Join(f.SousHome, "config.toml")); err == nil && strings.Contains(string(b), "nope") {
		t.Fatal("a refused value was written")
	}
}

// Review: only org/* is a pattern sous understands; anything else with a
// star is refused rather than written where it would do nothing.
func TestConfigOnlyOrgStarPatterns(t *testing.T) {
	f := fixture(t)
	f.mkrepo("acme/api", true)
	if _, errs, code := f.run("config", "-p", "acme/ap*", "backend", "markdown"); code != 2 || !strings.Contains(errs, "org/*") {
		t.Fatalf("%d %q", code, errs)
	}
}

// Review: lists can be added to and taken from; roots cannot be unset;
// -p alone shows that project; a change with --json answers in JSON.
func TestConfigListsProjectsAndJSON(t *testing.T) {
	f := fixture(t)
	f.mkrepo("acme/api", true)
	f.run("config", "ignore", "a/*")
	f.run("config", "ignore", "--add", "b/*")
	f.run("config", "ignore", "--add", "b/*") // once only
	if out, _, _ := f.run("config", "--json"); !strings.Contains(out, `"a/*",`) || strings.Count(out, `"b/*"`) != 1 {
		t.Fatalf("add: %s", out)
	}
	f.run("config", "ignore", "--remove", "a/*")
	if out, _, _ := f.run("config", "--json"); strings.Contains(out, `"a/*"`) {
		t.Fatalf("remove: %s", out)
	}
	if _, errs, code := f.run("config", "--unset", "roots"); code != 2 || !strings.Contains(errs, "sous setup") {
		t.Fatalf("roots: %d %q", code, errs)
	}
	f.run("config", "-p", "api", "backend", "markdown")
	if out, _, code := f.run("config", "-p", "api"); code != 0 || !strings.Contains(out, "backend = markdown") || strings.Contains(out, "refresh_hours") {
		t.Fatalf("-p alone: %d %s", code, out)
	}
	if out, _, code := f.run("config", "--json", "agent", "codex"); code != 0 || !strings.Contains(out, `"key": "agent"`) {
		t.Fatalf("json change: %d %s", code, out)
	}
	if _, errs, code := f.run("config", "-p", "api", "a b", "x"); code != 0 {
		t.Fatalf("a key with a space is quoted: %d %q", code, errs)
	}
}
