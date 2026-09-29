package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaultsWhenMissing(t *testing.T) {
	c, err := Load(t.TempDir())
	if err != nil || c.Agent != "claude" || c.RefreshHours != 4 || len(c.Roots) != 0 {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestLoadExpandsAndParsesProjects(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte(`
roots = ["~/code", "/abs"]
ignore = ["scratch/*"]
agent = "codex"
refresh_hours = 2
plugins = ["~/.sous/plugins/sous-signal-jira"]

[projects."studio/billing"]
backend = "jira"
jira_project = "WAS"
`), 0o644)
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Roots[0] != filepath.Join(home, "code") || c.Roots[1] != "/abs" {
		t.Errorf("roots: %v", c.Roots)
	}
	if !strings.HasPrefix(c.Plugins[0], home) {
		t.Errorf("plugins not expanded: %v", c.Plugins)
	}
	if c.Agent != "codex" || c.RefreshHours != 2 || c.Ignore[0] != "scratch/*" {
		t.Errorf("%+v", c)
	}
	if c.Projects["studio/billing"]["backend"] != "jira" {
		t.Errorf("projects: %v", c.Projects)
	}
}

func TestLoadBadTOML(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte("roots = [\n"), 0o644)
	if _, err := Load(dir); err == nil {
		t.Fatal("bad TOML must error")
	}
}

func TestExpand(t *testing.T) {
	t.Setenv("HOME", "/h")
	for in, want := range map[string]string{"~": "/h", "~/x": "/h/x", "/abs": "/abs", "rel": "rel", "~x": "~x"} {
		if got := Expand(in); got != want {
			t.Errorf("Expand(%q)=%q want %q", in, got, want)
		}
	}
}

func TestProjectTypedAndIdentities(t *testing.T) {
	c := &Config{Projects: map[string]map[string]string{
		"acme/*":    {"github_account": "work-account", "backend": "github"},
		"acme/sous": {"backend": "markdown", "jira_project": "SOUS"},
		"studio/*":  {"github_account": "personal-account"},
	}}
	pc := c.Project("acme/sous")
	if pc.Backend != "markdown" || pc.GitHubAccount != "work-account" || pc.Raw["jira_project"] != "SOUS" {
		t.Fatalf("%+v", pc)
	}
	if c.Project("acme/other").Backend != "github" || c.Project("x/y").GitHubAccount != "" {
		t.Fatal("glob fallback")
	}
	if got := c.Identities("github_account"); strings.Join(got, ",") != "personal-account,work-account" {
		t.Fatalf("%v", got)
	}
}

func TestSetRootsKeepsTheRestOfTheFile(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, "config.toml"), []byte("# mine\nagent = \"codex\"\nroots = [\"~/old\"]\n\n[projects.\"acme/*\"]\nbackend = \"markdown\"\n"), 0o644)
	if err := SetRoots(home, []string{"~/code", "~/work"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(home, "config.toml"))
	want := "# mine\nagent = \"codex\"\nroots = [\"~/code\", \"~/work\"]\n\n[projects.\"acme/*\"]\nbackend = \"markdown\"\n"
	if string(b) != want {
		t.Fatalf("%q", b)
	}
	fresh := t.TempDir()
	SetRoots(fresh, []string{"~/code"})
	c, err := Load(fresh)
	if err != nil || len(c.Roots) != 1 {
		t.Fatal(c, err)
	}
	withTable := t.TempDir()
	os.WriteFile(filepath.Join(withTable, "config.toml"), []byte("[projects.\"acme/*\"]\nbackend = \"markdown\"\n"), 0o644)
	SetRoots(withTable, []string{"~/code"})
	if c, err := Load(withTable); err != nil || len(c.Roots) != 1 || c.Project("acme/x").Backend != "markdown" {
		t.Fatalf("roots must go above any table: %+v %v", c, err)
	}
}
