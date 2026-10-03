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
)

type Config struct {
	Roots        []string                     `toml:"roots"`
	Ignore       []string                     `toml:"ignore"`
	Agent        string                       `toml:"agent"`
	Runner       string                       `toml:"runner"` // "" = Agent
	RefreshHours int                          `toml:"refresh_hours"`
	RunMinutes   int                          `toml:"run_minutes"`
	Plugins      []string                     `toml:"plugins"`
	GitLabHosts  []string                     `toml:"gitlab_hosts"`
	Projects     map[string]map[string]string `toml:"projects"`
}

// ProjectConfig is what config says about one project, typed. Raw keeps
// every key so third-party plugins can read their own.
// The per-project settings sous itself reads. Plugins may read others.
const (
	KeyBackend       = "backend"
	KeyGitHubAccount = "github_account"
)

// The top-level settings whose value names a plugin.
const (
	KeyAgent  = "agent"
	KeyRunner = "runner"
)

// ProjectKeys are the per-project settings sous reads.
var ProjectKeys = []string{KeyBackend, KeyGitHubAccount}

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
	return ProjectConfig{Backend: raw[KeyBackend], GitHubAccount: raw[KeyGitHubAccount], Raw: raw}
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
func Default() *Config {
	return &Config{Agent: "claude", RefreshHours: defaultRefreshHours, RunMinutes: defaultRunMinutes}
}

const (
	defaultRefreshHours = 4
	defaultRunMinutes   = 60
)

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
	return orDefault(c.RefreshHours, defaultRefreshHours, time.Hour)
}

// orDefault is n units, or def units when n is not set (0 or less).
func orDefault(n, def int, unit time.Duration) time.Duration {
	if n <= 0 {
		n = def
	}
	return time.Duration(n) * unit
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

// SetRoots writes the project folders, as people write them: under
// userHome as ~/..., others as given.
func SetRoots(home, userHome string, roots []string) error {
	shown := make([]string, len(roots))
	for i, r := range roots {
		shown[i] = Tilde(userHome, r)
	}
	return Set(home, nil, "roots", shown)
}

// tomlScanner walks TOML text just far enough to find where a top-level
// value ends.
type tomlScanner struct {
	s       string
	i       int
	comment bool // value() passed a comment inside a value (a multi-line list)
}

// valueHasComment: does the value of the key at start hold a comment?
func valueHasComment(s string, start int) bool {
	sc := tomlScanner{s: s, i: start}
	sc.keyName()
	sc.skipSpaces()
	sc.i++ // =
	sc.value()
	return sc.comment
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

// skipBlankKeepComments skips blank lines only: comments are content.
func (sc *tomlScanner) skipBlankKeepComments() {
	for sc.i < len(sc.s) && strings.ContainsRune(" \t\r\n", rune(sc.s[sc.i])) {
		sc.i++
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

// header reads a [table] header and returns its path, each part unquoted:
// [ projects . 'acme/*' ] is {"projects", "acme/*"}. The scanner moves to
// the next line.
func (sc *tomlScanner) header() []string {
	sc.i++ // [
	var parts []string
	for sc.i < len(sc.s) && sc.s[sc.i] != ']' && sc.s[sc.i] != '\n' {
		sc.skipSpaces()
		start := sc.i
		if c := sc.s[sc.i]; c == '"' || c == '\'' {
			sc.str()
			raw := sc.s[start:sc.i]
			if c == '"' {
				if u, err := strconv.Unquote(raw); err == nil {
					raw = u
				} else {
					raw = raw[1 : len(raw)-1]
				}
			} else {
				raw = raw[1 : len(raw)-1]
			}
			parts = append(parts, raw)
		} else {
			for sc.i < len(sc.s) && !strings.ContainsRune(". \t]\n", rune(sc.s[sc.i])) {
				sc.i++
			}
			parts = append(parts, sc.s[start:sc.i])
		}
		sc.skipSpaces()
		if sc.i < len(sc.s) && sc.s[sc.i] == '.' {
			sc.i++
		}
	}
	sc.skipLine()
	return parts
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
			sc.comment = true
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

// RunLimit is how long a built in run may take (run_minutes).
func (c *Config) RunLimit() time.Duration {
	return orDefault(c.RunMinutes, defaultRunMinutes, time.Minute)
}

// RunAgent is the runner go --run uses when none is named: runner, or the
// agent when runner is not set.
func (c *Config) RunAgent() string {
	if c.Runner != "" {
		return c.Runner
	}
	return c.Agent
}

// ChangeList adds items not already in have, or removes them, keeping order.
func ChangeList(have, items []string, add bool) []string {
	out := slices.Clone(have)
	for _, it := range items {
		switch i := slices.Index(out, it); {
		case add && i < 0:
			out = append(out, it)
		case !add && i >= 0:
			out = slices.Delete(out, i, i+1)
		}
	}
	return out
}

// CheckPattern: the only pattern a project's settings may name is org/*,
// every project in one org folder.
func CheckPattern(term string) error {
	if org, rest, ok := strings.Cut(term, "/"); !ok || rest != "*" || org == "" || strings.Contains(org, "*") {
		return fmt.Errorf("%q: the only pattern sous knows is org/*, for every project in one org folder", term)
	}
	return nil
}
