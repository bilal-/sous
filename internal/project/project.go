// Package project discovers repos under workspace roots and identifies them.
// No registry: a clone appears, a deletion vanishes.
package project

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
		level1, err := os.ReadDir(root)
		if err != nil {
			fmt.Fprintf(warn, "sous: cannot read %s: %v\n", root, err)
			unavailable++
			continue
		}
		for _, d1 := range level1 {
			p1 := filepath.Join(root, d1.Name())
			if !isDir(p1) { // os.Stat, not DirEntry.IsDir: symlinked repos and org folders count
				continue
			}
			if isRepo(p1) {
				add(&out, seen, root, p1, ignore)
				continue // never descend into a repo
			}
			level2, err := os.ReadDir(p1)
			if err != nil { // an org folder sous cannot read: its projects are unknown
				fmt.Fprintf(warn, "sous: cannot read %s: %v\n", p1, err)
				unavailable++
				continue
			}
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
	if out, err := Git(path, "log", "-1", "--format=%cI"); err == nil {
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(out)); err == nil {
			u := t.UTC()
			p.LastCommit = &u
		}
	}
	return p
}

// Git runs git in dir and returns what it printed, its last newline
// dropped (a line's leading space can mean something, as in status
// --porcelain). A failure says what git said.
func Git(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) && len(bytes.TrimSpace(ee.Stderr)) > 0 {
		err = errors.New(strings.TrimSpace(string(ee.Stderr)))
	}
	return strings.TrimRight(string(out), "\n"), err
}

// Remote returns "host/org/repo" for origin, or "".
func Remote(path string) string {
	out, err := Git(path, "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return normalizeRemote(strings.TrimSpace(out))
}

// normalizeRemote turns any git remote into "host/path", as plain text:
// git@host:path, user@host:path, host:path, ssh://user@host:port/path and
// https://host/path. It does no lookups; this string is the project's
// identity, so it must not change with the machine's ssh config.
func normalizeRemote(url string) string {
	if url == "" {
		return ""
	}
	url = strings.TrimSuffix(strings.TrimSuffix(url, "/"), ".git")
	if i := strings.Index(url, "://"); i >= 0 {
		url = url[i+3:]
		if at := strings.Index(url, "@"); at >= 0 && at < strings.IndexByte(url+"/", '/') {
			url = url[at+1:]
		}
		host, rest, _ := strings.Cut(url, "/")
		host, _, _ = strings.Cut(host, ":") // a port is not part of the host
		return joinRemote(host, rest)
	}
	// scp style: [user@]host:path
	if host, rest, ok := strings.Cut(url, ":"); ok && !strings.Contains(host, "/") {
		if at := strings.LastIndex(host, "@"); at >= 0 {
			host = host[at+1:]
		}
		return joinRemote(host, rest)
	}
	return url
}

func joinRemote(host, path string) string {
	if path == "" {
		return host
	}
	return host + "/" + strings.TrimPrefix(path, "/")
}

// ForPath returns the repo root containing path.
//
// A repo whose root is the home folder (a dotfiles repo) is not a project:
// otherwise every folder under home would belong to it.
func ForPath(path, home string) (string, bool) {
	out, err := Git(path, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", false
	}
	root := strings.TrimSpace(out)
	if home != "" && samePath(root, home) {
		return "", false
	}
	return root, true
}

func samePath(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
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
	if _, err := Git(root, "rev-parse", "--is-inside-work-tree"); err != nil {
		return Facts{}, fmt.Errorf("%s: not a git repository", root)
	}
	var f Facts
	if b, err := Git(root, "branch", "--show-current"); err == nil && strings.TrimSpace(b) != "" {
		f.Branch = strings.TrimSpace(b)
	} else {
		f.Branch = "(detached)"
	}
	if _, err := Git(root, "rev-parse", "--abbrev-ref", "-q", "@{u}"); err == nil {
		f.HasUpstream = true
	}
	if out, err := Git(root, "log", "-1", "--format=%cI%x00%s"); err == nil {
		ts, subject, _ := strings.Cut(strings.TrimSpace(out), "\x00")
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			u := t.UTC()
			f.LastCommit = &u
			f.LastSubject = subject
		}
	}
	return f, nil
}

// likelyRootNames are where people usually keep their repos, under home.
var likelyRootNames = []string{"code", "src", "dev", "projects", "workspace", "repos", "git", "Developer", "Projects", "Documents/GitHub"}

// LikelyRoots are the usual project folders under home that hold at least
// one project, as Discover sees it. On a disk that ignores letter case,
// ~/projects and ~/Projects are one folder and count once.
func LikelyRoots(home string) []string {
	var roots []string
	var seen []os.FileInfo
	for _, name := range likelyRootNames {
		dir := filepath.Join(home, name)
		fi, err := os.Stat(dir)
		if err != nil || !fi.IsDir() || slices.ContainsFunc(seen, func(s os.FileInfo) bool { return os.SameFile(s, fi) }) {
			continue
		}
		seen = append(seen, fi)
		if ps, _ := Discover([]string{dir}, nil, io.Discard); len(ps) > 0 {
			roots = append(roots, dir)
		}
	}
	return roots
}
