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
//
// Roots are stored as people write them: under userHome as ~/..., others as
// given.
func SetRoots(home, userHome string, roots []string) error {
	quoted := make([]string, len(roots))
	for i, r := range roots {
		quoted[i] = strconv.Quote(Tilde(userHome, r))
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
		want := make([]string, len(roots))
		for i, r := range roots {
			want[i] = Tilde(userHome, r)
		}
		if !slices.Equal(check.Roots, want) {
			return nil, errors.New("config.toml has roots written in a way sous cannot safely change; edit it by hand")
		}
		return []byte(s), nil
	})
}

// topLevelKey finds `key = value` among the top-level settings (before the
// first [table]) and returns the byte span from the key to the end of its
// value. It reads the file once, as TOML does: strings of every kind
// (including multi-line ones), comments, and arrays across lines, so text
// that only looks like the key is never taken for it.
func topLevelKey(s, key string) (start, end int, ok bool) {
	sc := tomlScanner{s: s}
	for sc.i < len(s) {
		sc.skipBlank()
		if sc.i >= len(s) {
			break
		}
		if s[sc.i] == '[' {
			return 0, 0, false // a table starts: the rest is not top level
		}
		lineStart := sc.i
		name := sc.keyName()
		sc.skipSpaces()
		if sc.i >= len(s) || s[sc.i] != '=' {
			sc.skipLine()
			continue
		}
		sc.i++
		sc.value()
		if name == key {
			return lineStart, sc.i, true
		}
		sc.skipLine()
	}
	return 0, 0, false
}

// tomlScanner walks TOML text just far enough to find where a top-level
// value ends.
type tomlScanner struct {
	s string
	i int
}

// skipBlank skips blank lines, whitespace and comment lines.
func (sc *tomlScanner) skipBlank() {
	for sc.i < len(sc.s) {
		switch c := sc.s[sc.i]; {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			sc.i++
		case c == '#':
			sc.skipLine()
		default:
			return
		}
	}
}

func (sc *tomlScanner) skipSpaces() {
	for sc.i < len(sc.s) && (sc.s[sc.i] == ' ' || sc.s[sc.i] == '\t') {
		sc.i++
	}
}

// skipLine moves past the end of the current line.
func (sc *tomlScanner) skipLine() {
	for sc.i < len(sc.s) && sc.s[sc.i] != '\n' {
		sc.i++
	}
	if sc.i < len(sc.s) {
		sc.i++
	}
}

// keyName reads a bare or quoted key (dotted keys read whole).
func (sc *tomlScanner) keyName() string {
	start := sc.i
	for sc.i < len(sc.s) {
		c := sc.s[sc.i]
		if c == '"' || c == '\'' {
			sc.str()
			continue
		}
		if c == '=' || c == '\n' || c == ' ' || c == '\t' {
			break
		}
		sc.i++
	}
	return strings.Trim(sc.s[start:sc.i], `"'`)
}

// value reads one value: a string, an array (nested, across lines, with
// comments), an inline table, or a bare word; it stops before any trailing
// comment on its line.
func (sc *tomlScanner) value() {
	sc.skipSpaces()
	depth := 0
	for sc.i < len(sc.s) {
		switch c := sc.s[sc.i]; {
		case c == '"' || c == '\'':
			sc.str()
			if depth == 0 {
				return
			}
			continue
		case c == '[' || c == '{':
			depth++
		case c == ']' || c == '}':
			depth--
			if depth == 0 {
				sc.i++
				return
			}
		case c == '#':
			if depth == 0 {
				return
			}
			sc.skipLine()
			continue
		case c == '\n' || c == '\r':
			if depth == 0 {
				return
			}
		}
		sc.i++
	}
}

// str reads one string of any kind: "basic", 'literal', """multi-line"""
// or ”'multi-line literal”'.
func (sc *tomlScanner) str() {
	q := sc.s[sc.i]
	if delim := strings.Repeat(string(q), 3); strings.HasPrefix(sc.s[sc.i:], delim) {
		// Multi-line: ends at the first unescaped run of three quotes (a
		// longer run ends at its last three). Only basic strings escape.
		for sc.i += 3; sc.i < len(sc.s); sc.i++ {
			if q == '"' && sc.s[sc.i] == '\\' {
				sc.i++
				continue
			}
			if strings.HasPrefix(sc.s[sc.i:], delim) {
				for sc.i += 3; sc.i < len(sc.s) && sc.s[sc.i] == q; sc.i++ {
				}
				return
			}
		}
		return
	}
	for sc.i++; sc.i < len(sc.s); sc.i++ {
		switch sc.s[sc.i] {
		case '\\':
			if q == '"' {
				sc.i++
			}
		case q:
			sc.i++
			return
		case '\n':
			return
		}
	}
}
