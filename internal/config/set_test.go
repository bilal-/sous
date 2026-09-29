package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setIn(t *testing.T, in string, table []string, key string, value any) (string, error) {
	t.Helper()
	home := t.TempDir()
	if in != "" {
		os.WriteFile(filepath.Join(home, "config.toml"), []byte(in), 0o644)
	}
	err := Set(home, table, key, value)
	b, _ := os.ReadFile(filepath.Join(home, "config.toml"))
	return string(b), err
}

var projects = func(k string) []string { return []string{"projects", k} }

func TestSet(t *testing.T) {
	for _, c := range []struct {
		name, in string
		table    []string
		key      string
		value    any
		want     string
	}{
		{"new file", "", nil, "agent", "codex", "agent = \"codex\"\n"},
		{"replace keeps comments", "# mine\nagent = \"claude\" # the default\nroots = [\"~/code\"]\n", nil, "agent", "codex", "# mine\nagent = \"codex\" # the default\nroots = [\"~/code\"]\n"},
		{"add above tables", "roots = [\"~/code\"]\n\n[projects.\"acme/*\"]\nbackend = \"markdown\"\n", nil, "refresh_hours", 2, "roots = [\"~/code\"]\nrefresh_hours = 2\n\n[projects.\"acme/*\"]\nbackend = \"markdown\"\n"},
		{"array", "", nil, "ignore", []string{"scratch/*", "tmp"}, "ignore = [\"scratch/*\", \"tmp\"]\n"},
		{"table key replaced", "[projects.\"acme/*\"]\nbackend = \"markdown\"\n[projects.\"oss/app\"]\nbackend = \"github\"\n", projects("oss/app"), "backend", "markdown", "[projects.\"acme/*\"]\nbackend = \"markdown\"\n[projects.\"oss/app\"]\nbackend = \"markdown\"\n"},
		{"table key added", "[projects.\"acme/*\"]\nbackend = \"markdown\"\n\n[projects.\"oss/app\"]\nx = \"1\"\n", projects("acme/*"), "github_account", "work", "[projects.\"acme/*\"]\nbackend = \"markdown\"\ngithub_account = \"work\"\n\n[projects.\"oss/app\"]\nx = \"1\"\n"},
		{"table created", "agent = \"codex\"\n", projects("acme/api"), "backend", "markdown", "agent = \"codex\"\n\n[projects.\"acme/api\"]\nbackend = \"markdown\"\n"},
		{"header spelled differently", "[ projects . 'acme/*' ]\nbackend = \"markdown\"\n", projects("acme/*"), "backend", "github", "[ projects . 'acme/*' ]\nbackend = \"github\"\n"},
		{"unset top", "agent = \"codex\"\nroots = [\"~/code\"]\n", nil, "agent", nil, "roots = [\"~/code\"]\n"},
		{"unset in table", "[projects.\"acme/*\"]\nbackend = \"markdown\"\ngithub_account = \"w\"\n", projects("acme/*"), "backend", nil, "[projects.\"acme/*\"]\ngithub_account = \"w\"\n"},
		{"unset absent is no change", "agent = \"codex\"\n", nil, "roots", nil, "agent = \"codex\"\n"},
		{"odd characters quoted for TOML", "", nil, "agent", "a\"b\\c\td", "agent = \"a\\\"b\\\\c\\td\"\n"},
	} {
		got, err := setIn(t, c.in, c.table, c.key, c.value)
		if err != nil || got != c.want {
			t.Errorf("%s: %v\ngot  %q\nwant %q", c.name, err, got, c.want)
		}
	}
}

// Review: an edit that would change anything but the one setting is
// refused, and the file is left as it was.
func TestSetRefusesWhatItCannotChangeSafely(t *testing.T) {
	for name, in := range map[string]string{
		"dotted key":   "projects.\"acme/*\".backend = \"markdown\"\n",
		"inline table": "projects = { \"acme/*\" = { backend = \"markdown\" } }\n",
	} {
		got, err := setIn(t, in, projects("acme/*"), "backend", "github")
		if err == nil || got != in || !strings.Contains(err.Error(), "by hand") {
			t.Errorf("%s: %v\n%s", name, err, got)
		}
	}
}
