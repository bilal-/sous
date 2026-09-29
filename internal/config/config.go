// Package config loads ~/.sous/config.toml. A missing file is not an error:
// sous must work on a fresh machine with `sous projects --root ~/code`.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/bilal-/sous/internal/store"
)

type Config struct {
	Roots        []string                     `toml:"roots"`
	Ignore       []string                     `toml:"ignore"`
	Agent        string                       `toml:"agent"`
	RefreshHours int                          `toml:"refresh_hours"`
	Plugins      []string                     `toml:"plugins"`
	GitLabHosts  []string                     `toml:"gitlab_hosts"`
	Projects     map[string]map[string]string `toml:"projects"`
}

// ProjectConfig is what config says about one project, typed. Raw keeps
// every key so third-party plugins can read their own.
type ProjectConfig struct {
	Backend       string
	GitHubAccount string
	Raw           map[string]string
}

// Project resolves the per-project settings for "org/name": an exact
// entry's keys win, then the org's "org/*" entry fills the rest.
func (c *Config) Project(orgName string) ProjectConfig {
	raw := map[string]string{}
	if i := strings.Index(orgName, "/"); i > 0 {
		for k, v := range c.Projects[orgName[:i]+"/*"] {
			raw[k] = v
		}
	}
	for k, v := range c.Projects[orgName] {
		raw[k] = v
	}
	return ProjectConfig{Backend: raw["backend"], GitHubAccount: raw["github_account"], Raw: raw}
}

// Identities: every distinct value of key across projects, sorted — e.g.
// the GitHub accounts a signal must search as.
func (c *Config) Identities(key string) []string {
	seen := map[string]bool{}
	var out []string
	for _, pc := range c.Projects {
		if v := pc[key]; v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// Default is the configuration before config.toml says anything.
func Default() *Config { return &Config{Agent: "claude", RefreshHours: defaultRefreshHours} }

const defaultRefreshHours = 4

// NoRootsHint says what to do when no project folders are set yet.
const NoRootsHint = "no project folders yet. Run sous setup to find them, or sous setup ~/path/to/your/projects"

// Path is config.toml's place in sousHome.
func Path(sousHome string) string { return filepath.Join(sousHome, "config.toml") }

// Load reads config.toml from sousHome; ~ in paths means userHome.
func Load(sousHome, userHome string) (*Config, error) {
	c := Default()
	b, err := os.ReadFile(Path(sousHome))
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := toml.Decode(string(b), c); err != nil {
		return nil, err
	}
	for i, r := range c.Roots {
		c.Roots[i] = Expand(userHome, r)
	}
	for i, p := range c.Plugins {
		c.Plugins[i] = Expand(userHome, p)
	}
	return c, nil
}

// RefreshWindow: how old the cached board may get before the shell
// surface refreshes it. The one place the 4-hour default lives.
func (c *Config) RefreshWindow() time.Duration {
	if c.RefreshHours > 0 {
		return time.Duration(c.RefreshHours) * time.Hour
	}
	return defaultRefreshHours * time.Hour
}

// Expand turns a leading ~ into the user's home directory.
// Tilde writes a path under home as ~/..., the way people type it. It is
// the inverse of Expand.
func Tilde(home, p string) string {
	if home == "" {
		return p
	}
	if rest, ok := strings.CutPrefix(p, home); ok && (rest == "" || rest[0] == '/') {
		return "~" + rest
	}
	return p
}

// Expand turns a leading ~ into home.
func Expand(home, p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		return filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return p
}

// SetRoots writes roots into config.toml, creating it if needed. Only the
// top-level roots value changes, however it was written (one line, several
// lines, indented); comments, tables and every other setting stay as they
// were. The result is checked to parse before it is saved.
func SetRoots(home string, roots []string) error {
	quoted := make([]string, len(roots))
	for i, r := range roots {
		quoted[i] = strconv.Quote(r)
	}
	line := "roots = [" + strings.Join(quoted, ", ") + "]"
	return store.EditFile(Path(home), 0o644, func(b []byte) ([]byte, error) {
		s := string(b)
		if start, end, ok := topLevelKey(s, "roots"); ok {
			s = s[:start] + line + s[end:]
		} else {
			s = line + "\n" + s // above everything, so never inside a [table]
		}
		var check Config
		if _, err := toml.Decode(s, &check); err != nil {
			return nil, fmt.Errorf("config.toml would not parse after setting roots (%v); edit it by hand", err)
		}
		if !slices.Equal(check.Roots, roots) {
			return nil, errors.New("config.toml has roots written in a way sous cannot safely change; edit it by hand")
		}
		return []byte(s), nil
	})
}

// topLevelKey finds key = value before the first [table], and returns the
// byte span from the key to the end of its value, following a [ ... ] array
// across lines and ignoring brackets inside strings.
func topLevelKey(s, key string) (start, end int, ok bool) {
	off := 0
	for _, line := range strings.SplitAfter(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			return 0, 0, false // tables start: the rest is not top level
		}
		name, rest, isKV := strings.Cut(trimmed, "=")
		if isKV && strings.TrimSpace(name) == key {
			start = off + strings.Index(line, trimmed)
			return start, start + valueLen(s[start:], len(trimmed)-len(rest)), true
		}
		off += len(line)
	}
	return 0, 0, false
}

// valueLen is how many bytes of s (starting at the key) the key and its
// value take: to the closing bracket of an array, else to the end of line.
func valueLen(s string, eq int) int {
	depth, inStr := 0, byte(0)
	for i := eq; i < len(s); i++ {
		c := s[i]
		switch {
		case inStr != 0:
			if c == '\\' && inStr == '"' {
				i++
			} else if c == inStr {
				inStr = 0
			}
		case c == '"' || c == '\'':
			inStr = c
		case c == '[':
			depth++
		case c == ']':
			depth--
			if depth == 0 {
				return i + 1
			}
		case c == '\n' && depth == 0:
			return i
		case c == '#' && depth == 0:
			return i
		}
	}
	return len(s)
}
