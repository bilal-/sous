// Package config loads ~/.sous/config.toml. A missing file is not an error:
// sous must work on a fresh machine with `sous projects --root ~/code`.
package config

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
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

func Load(home string) (*Config, error) {
	c := &Config{Agent: "claude", RefreshHours: 4}
	b, err := os.ReadFile(filepath.Join(home, "config.toml"))
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
		c.Roots[i] = Expand(r)
	}
	for i, p := range c.Plugins {
		c.Plugins[i] = Expand(p)
	}
	return c, nil
}

// RefreshWindow: how old the cached board may get before the shell
// surface refreshes it. The one place the 4-hour default lives.
func (c *Config) RefreshWindow() time.Duration {
	if c.RefreshHours > 0 {
		return time.Duration(c.RefreshHours) * time.Hour
	}
	return 4 * time.Hour
}

// Expand turns a leading ~ into the user's home directory.
// Tilde is the inverse of Expand: a path under the home folder written as
// ~/..., the way people type it.
func Tilde(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if rest, ok := strings.CutPrefix(p, home); ok && (rest == "" || rest[0] == '/') {
		return "~" + rest
	}
	return p
}

func Expand(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return p
}

var rootsLine = regexp.MustCompile(`(?m)^roots\s*=.*$`)

// SetRoots writes roots into config.toml, creating it if needed. Only the
// roots line changes; comments and every other setting stay as written.
func SetRoots(home string, roots []string) error {
	quoted := make([]string, len(roots))
	for i, r := range roots {
		quoted[i] = strconv.Quote(r)
	}
	line := "roots = [" + strings.Join(quoted, ", ") + "]"
	p := filepath.Join(home, "config.toml")
	b, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	s := string(b)
	switch {
	case rootsLine.MatchString(s):
		s = rootsLine.ReplaceAllLiteralString(s, line)
	default:
		// Above everything, so it can never land inside a [table].
		s = line + "\n" + s
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(s), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
