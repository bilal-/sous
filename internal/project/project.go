// Package project discovers repos under workspace roots and identifies them.
// No registry: a clone appears, a deletion vanishes.
package project

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Project struct {
	Path       string     `json:"path"`
	Org        string     `json:"org"`
	Name       string     `json:"name"`
	Remote     *string    `json:"remote"`
	LastCommit *time.Time `json:"last_commit"`
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func isRepo(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git")) // dir or file (worktree)
	return err == nil
}

func ignored(rel string, ignore []string) bool {
	for _, pat := range ignore {
		if ok, _ := filepath.Match(pat, rel); ok {
			return true
		}
	}
	return false
}

// Discover walks each root to depth 2, stops at the first .git, never descends
// into a repo. Returns projects sorted by path and the count of missing roots.
func Discover(roots, ignore []string, warn io.Writer) ([]Project, int) {
	seen := map[string]bool{}
	var out []Project
	unavailable := 0
	for _, root := range roots {
		root = filepath.Clean(root)
		if abs, err := filepath.Abs(root); err == nil {
			root = abs // ids and thread matching key on absolute paths
		}
		if st, err := os.Stat(root); err != nil || !st.IsDir() {
			fmt.Fprintf(warn, "sous: root not found: %s\n", root)
			unavailable++
			continue
		}
		// Physical path, so it agrees with what git reports (macOS /var → /private/var).
		if real, err := filepath.EvalSymlinks(root); err == nil {
			root = real
		}
		if isRepo(root) { // a root pointed straight at one repo is that repo
			add(&out, seen, filepath.Dir(root), root, ignore)
			continue
		}
		level1, _ := os.ReadDir(root)
		for _, d1 := range level1 {
			p1 := filepath.Join(root, d1.Name())
			if !isDir(p1) { // os.Stat, not DirEntry.IsDir: symlinked repos and org folders count
				continue
			}
			if isRepo(p1) {
				add(&out, seen, root, p1, ignore)
				continue // never descend into a repo
			}
			level2, _ := os.ReadDir(p1)
			for _, d2 := range level2 {
				p2 := filepath.Join(p1, d2.Name())
				if !isDir(p2) {
					continue
				}
				if isRepo(p2) {
					add(&out, seen, root, p2, ignore)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, unavailable
}

func add(out *[]Project, seen map[string]bool, root, path string, ignore []string) {
	if seen[path] {
		return
	}
	rel, _ := filepath.Rel(root, path)
	if ignored(rel, ignore) {
		return
	}
	seen[path] = true
	*out = append(*out, Describe(path))
}

// OrgName is the "org/name" a project is known by in config and messages:
// the parent directory and the repo directory.
func OrgName(path string) string {
	return filepath.Base(filepath.Dir(path)) + "/" + filepath.Base(path)
}

// Describe builds a Project for any repo path (used for repos outside roots too).
func Describe(path string) Project {
	p := Project{Path: path, Org: filepath.Base(filepath.Dir(path)), Name: filepath.Base(path)}
	if r := Remote(path); r != "" {
		p.Remote = &r
	}
	if out, err := git(path, "log", "-1", "--format=%cI"); err == nil {
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(out)); err == nil {
			u := t.UTC()
			p.LastCommit = &u
		}
	}
	return p
}

func git(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	return string(out), err
}

// Remote returns "host/org/repo" for origin, or "".
func Remote(path string) string {
	out, err := git(path, "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return normalizeRemote(strings.TrimSpace(out))
}

func normalizeRemote(url string) string {
	if url == "" {
		return ""
	}
	url = strings.TrimSuffix(strings.TrimSuffix(url, "/"), ".git")
	switch {
	case strings.HasPrefix(url, "git@"):
		url = strings.Replace(strings.TrimPrefix(url, "git@"), ":", "/", 1)
	case strings.Contains(url, "://"):
		url = url[strings.Index(url, "://")+3:]
		if i := strings.Index(url, "@"); i >= 0 {
			url = url[i+1:]
		}
	}
	return url
}

// ForPath returns the repo root containing path.
func ForPath(path string) (string, bool) {
	out, err := git(path, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(out), true
}

// Age formats a duration since t the way the board does: 5m, 3h, 6d, 3mo, 1y.
func Age(now, t time.Time) string {
	s := now.Sub(t).Seconds()
	switch {
	case s < 0:
		return "now"
	case s < 3600:
		return fmt.Sprintf("%dm", int(s/60))
	case s < 86400:
		return fmt.Sprintf("%dh", int(s/3600))
	case s < 2592000:
		return fmt.Sprintf("%dd", int(s/86400))
	case s < 31536000:
		return fmt.Sprintf("%dmo", int(s/2592000))
	}
	return fmt.Sprintf("%dy", int(s/31536000))
}

func RenderTable(w io.Writer, ps []Project, now time.Time) {
	ow, nw := 3, 4
	for _, p := range ps {
		if len(p.Org) > ow {
			ow = len(p.Org)
		}
		if len(p.Name) > nw {
			nw = len(p.Name)
		}
	}
	for _, p := range ps {
		host, last := "local", "no commits"
		if p.Remote != nil {
			host = strings.TrimSuffix(strings.SplitN(*p.Remote, "/", 2)[0], ".com")
		}
		if p.LastCommit != nil {
			last = Age(now, *p.LastCommit)
		}
		fmt.Fprintf(w, "%-*s   %-*s   %-8s   %s\n", ow, p.Org, nw, p.Name, host, last)
	}
}

// Facts are the git facts a person wants when landing in a repo. They are
// not signals (nothing is "waiting" in them); they set the scene.
type Facts struct {
	Branch      string     `json:"branch"`
	HasUpstream bool       `json:"has_upstream"`
	LastCommit  *time.Time `json:"last_commit"`
	LastSubject string     `json:"last_subject"`
}

// ReadFacts reads them for a repo root. A repo with no commits has a branch and
// nothing else.
func ReadFacts(root string) (Facts, error) {
	if _, err := git(root, "rev-parse", "--is-inside-work-tree"); err != nil {
		return Facts{}, fmt.Errorf("%s: not a git repository", root)
	}
	var f Facts
	if b, err := git(root, "branch", "--show-current"); err == nil && strings.TrimSpace(b) != "" {
		f.Branch = strings.TrimSpace(b)
	} else {
		f.Branch = "(detached)"
	}
	if _, err := git(root, "rev-parse", "--abbrev-ref", "-q", "@{u}"); err == nil {
		f.HasUpstream = true
	}
	if out, err := git(root, "log", "-1", "--format=%cI%x00%s"); err == nil {
		ts, subject, _ := strings.Cut(strings.TrimSpace(out), "\x00")
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			u := t.UTC()
			f.LastCommit = &u
			f.LastSubject = subject
		}
	}
	return f, nil
}
