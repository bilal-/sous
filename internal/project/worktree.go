package project

import (
	"os"
	"path/filepath"
	"strings"
)

// MainOf: when dir is a linked worktree (`git worktree add`), the main
// repo it belongs to. A worktree is part of that repo's project, not a
// project of its own: it shares the repo's history, stashes and remote.
//
// It reads git's files rather than running git, since discovery asks
// this of every folder it meets. A linked worktree's .git is a file
// naming its folder under the repo's .git/worktrees, which holds a
// commondir file; a submodule's .git file names a folder that does not,
// so a submodule stays its own repo. A worktree of a bare repo has no
// main folder to belong to, so it stays a project too.
func MainOf(dir string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(dir, ".git"))
	if err != nil {
		return "", false // a .git folder, or none: not a linked worktree
	}
	gitdir, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
	if !ok {
		return "", false
	}
	gitdir = abs(dir, strings.TrimSpace(gitdir))
	if filepath.Base(filepath.Dir(gitdir)) != "worktrees" {
		return "", false
	}
	c, err := os.ReadFile(filepath.Join(gitdir, "commondir"))
	if err != nil {
		return "", false
	}
	common := abs(gitdir, strings.TrimSpace(string(c)))
	if filepath.Base(common) != ".git" {
		return "", false // bare
	}
	main := filepath.Dir(common)
	if real, err := filepath.EvalSymlinks(main); err == nil {
		main = real
	}
	if !isDir(filepath.Join(main, ".git")) {
		return "", false
	}
	return main, true
}

func abs(base, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(base, p)
}

// Worktree is one linked worktree of a repo, as `git worktree list` has it.
type Worktree struct {
	Path   string
	Branch string // "" when detached
}

// Worktrees lists repo's linked worktrees whose folders still exist: not
// the main one, and not one git has marked prunable.
func Worktrees(repo string) ([]Worktree, error) {
	out, err := Git(repo, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var all []Worktree
	var cur *Worktree
	gone, first := false, true
	flush := func() {
		if cur != nil && !first && !gone && isDir(cur.Path) {
			all = append(all, *cur)
		}
		if cur != nil {
			first = false // git lists the main worktree first
		}
		cur, gone = nil, false
	}
	for _, line := range strings.Split(out, "\n") {
		switch k, v, _ := strings.Cut(line, " "); k {
		case "worktree":
			flush()
			cur = &Worktree{Path: v}
		case "branch":
			if cur != nil {
				cur.Branch = strings.TrimPrefix(v, "refs/heads/")
			}
		case "prunable":
			gone = true
		}
	}
	flush()
	return all, nil
}
