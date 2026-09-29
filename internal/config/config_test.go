package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaultsWhenMissing(t *testing.T) {
	c, err := Load(t.TempDir(), "/h")
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
	c, err := Load(dir, home)
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
	if _, err := Load(dir, "/h"); err == nil {
		t.Fatal("bad TOML must error")
	}
}

func TestExpand(t *testing.T) {
	for in, want := range map[string]string{"~": "/h", "~/x": "/h/x", "/abs": "/abs", "rel": "rel", "~x": "~x"} {
		if got := Expand("/h", in); got != want {
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
	if err := SetRoots(home, "/h", []string{"~/code", "~/work"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(home, "config.toml"))
	want := "# mine\nagent = \"codex\"\nroots = [\"~/code\", \"~/work\"]\n\n[projects.\"acme/*\"]\nbackend = \"markdown\"\n"
	if string(b) != want {
		t.Fatalf("%q", b)
	}
	fresh := t.TempDir()
	SetRoots(fresh, "/h", []string{"~/code"})
	c, err := Load(fresh, "/h")
	if err != nil || len(c.Roots) != 1 {
		t.Fatal(c, err)
	}
	withTable := t.TempDir()
	os.WriteFile(filepath.Join(withTable, "config.toml"), []byte("[projects.\"acme/*\"]\nbackend = \"markdown\"\n"), 0o644)
	SetRoots(withTable, "/h", []string{"~/code"})
	if c, err := Load(withTable, "/h"); err != nil || len(c.Roots) != 1 || c.Project("acme/x").Backend != "markdown" {
		t.Fatalf("roots must go above any table: %+v %v", c, err)
	}
}

func TestTildeIsExpandsInverse(t *testing.T) {
	home := t.TempDir()
	for _, p := range []string{"~", "~/code", "~/code/acme"} {
		if got := Tilde(home, Expand(home, p)); got != p {
			t.Errorf("%q → %q", p, got)
		}
	}
	if got := Tilde(home, home+"x/code"); got != home+"x/code" {
		t.Fatalf("a sibling folder is not under home: %q", got)
	}
}

// Review: roots written as a multi-line array, or indented, are replaced
// whole; a roots key inside a table is not ours to touch; the result must
// still parse.
func TestSetRootsHandlesRealTOML(t *testing.T) {
	for name, in := range map[string]string{
		"multi-line": "roots = [\n  \"~/old\",\n  \"~/older\",\n]\nagent = \"codex\"\n",
		"indented":   "  roots = [\"~/old\"]\nagent = \"codex\"\n",
		"in a table": "agent = \"codex\"\n[projects.\"acme/*\"]\nroots = \"not ours\"\n",
	} {
		home := t.TempDir()
		os.WriteFile(filepath.Join(home, "config.toml"), []byte(in), 0o644)
		if err := SetRoots(home, "/h", []string{"~/code"}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		c, err := Load(home, "/h")
		b, _ := os.ReadFile(filepath.Join(home, "config.toml"))
		if err != nil || len(c.Roots) != 1 || c.Agent != "codex" || strings.Count(string(b), "roots") != strings.Count(in, "roots")+boolInt(!strings.Contains(strings.SplitN(in, "[projects", 2)[0], "roots")) {
			t.Fatalf("%s: %v %+v\n%s", name, err, c, b)
		}
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Review: the roots value is found by reading the file as TOML does:
// arrays of arrays, comments inside arrays, and multi-line strings holding
// text that looks like a roots line.
func TestSetRootsReadsTOMLStructure(t *testing.T) {
	for name, in := range map[string]string{
		"array of arrays":   "matrix = [\n  [1, 2],\n  [3, 4],\n]\nroots = [\"~/old\"]\n",
		"comment in array":  "roots = [\n  \"~/old\", # the ] old one\n]\nagent = \"codex\"\n",
		"multi-line string": "note = \"\"\"\nroots = [\"~/fake\"]\n\"\"\"\nroots = [\"~/old\"]\n",
		"literal multi":     "note = '''\nroots = ['~/fake']\n'''\nagent = \"codex\"\n",
	} {
		home := t.TempDir()
		os.WriteFile(filepath.Join(home, "config.toml"), []byte(in), 0o644)
		if err := SetRoots(home, "/h", []string{"~/code"}); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		c, err := Load(home, "/h")
		if err != nil || len(c.Roots) != 1 || c.Roots[0] != "/h/code" {
			b, _ := os.ReadFile(filepath.Join(home, "config.toml"))
			t.Errorf("%s: %v %v\n%s", name, err, c, b)
		}
	}
}

// Roots are stored as people write them (~/code), whatever form they come in.
func TestSetRootsStoresTildeForm(t *testing.T) {
	home := t.TempDir()
	if err := SetRoots(home, "/h", []string{"/h/code", "/elsewhere"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(home, "config.toml")); !strings.Contains(string(b), `roots = ["~/code", "/elsewhere"]`) {
		t.Fatalf("%s", b)
	}
}
