package signal

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/text"
)

func gitOut(dir string, args ...string) (string, error) {
	out, err := project.Git(dir, args...)
	return strings.TrimSpace(out), err
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// ScanGit is the built-in git plugin: dirty, ahead, stash, no-upstream, last commit.
// Partial output is kept, but an incomplete scan fails so prior work stays stale.
func ScanGit(paths []string, w, warn io.Writer, now time.Time) error {
	if _, err := exec.LookPath("git"); err != nil {
		fmt.Fprintln(warn, "git not installed")
		return errors.New("git not installed")
	}
	// ~5 git processes per repo; run repos concurrently, each into its own
	// buffer so lines never interleave. Order is irrelevant downstream.
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, project.Workers)
	var unreadable int
	for _, p := range paths {
		if _, err := os.Lstat(filepath.Join(p, ".git")); err != nil {
			mu.Lock()
			fmt.Fprintf(warn, "sous-signal-git: not a repo: %s\n", p)
			unreadable++
			mu.Unlock()
			continue
		}
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var buf, wbuf bytes.Buffer
			ok := scanOne(p, &buf, &wbuf, now)
			mu.Lock()
			w.Write(buf.Bytes())
			warn.Write(wbuf.Bytes())
			if !ok {
				unreadable++
			}
			mu.Unlock()
		}(p)
	}
	wg.Wait()
	if unreadable > 0 {
		// Failing the plugin keeps prior observations (as stale) instead of
		// letting an unreadable repo read as clean.
		return fmt.Errorf("%d repo(s) unreadable", unreadable)
	}
	return nil
}

// scanOne emits findings for one repo. A git command that fails is reported
// on warn rather than read as "nothing": missing data must not look like zero.
func scanOne(p string, w, warn io.Writer, now time.Time) bool {
	if _, err := gitOut(p, "rev-parse", "--verify", "-q", "HEAD"); err != nil {
		return freshRepo(p, warn)
	}
	r := gitRepo{p: p, project: p, w: w, warn: warn, now: now}
	r.check()
	// The stash is the repo's, shared by its worktrees: counted once, here.
	if st := r.must("stash", "list"); st != "" {
		r.emit("stash", Unfinished, strconv.Itoa(countLines(st))+" stashes", now, "")
	}
	r.lastCommit()
	r.worktrees()
	return !r.failed
}

// runBranch is the branch sous's own runner gives each run. Its worktree
// is the run's, shown as the run, so it is not unfinished work too.
const runBranch = "sous/run-"

// worktrees: each linked worktree's uncommitted and unpushed work, under
// this repo's project. A worktree is a place to work on a branch, not a
// project of its own; its rows name it, so the person can find it.
func (r *gitRepo) worktrees() {
	wts, err := project.Worktrees(r.p)
	if err != nil {
		fmt.Fprintf(r.warn, "sous-signal-git: git worktree list failed in %s: %v\n", r.p, err)
		r.failed = true
		return
	}
	for _, wt := range wts {
		if strings.HasPrefix(wt.Branch, runBranch) {
			continue
		}
		t := gitRepo{p: wt.Path, project: r.project, at: wt.Path, w: r.w, warn: r.warn, now: r.now}
		t.check()
		r.failed = r.failed || t.failed
	}
}

// freshRepo: no HEAD is fine in a new repo; anything git itself cannot
// read is not. It reports whether the repo is usable.
func freshRepo(p string, warn io.Writer) bool {
	if _, err := gitOut(p, "rev-parse", "--is-inside-work-tree"); err != nil {
		fmt.Fprintf(warn, "sous-signal-git: unreadable repo: %s\n", p)
		return false
	}
	if _, err := gitOut(p, "symbolic-ref", "-q", "HEAD"); err != nil {
		fmt.Fprintf(warn, "sous-signal-git: unreadable repo (bad HEAD): %s\n", p)
		return false
	}
	return true // no commits: nothing to say
}

// gitRepo is one repo being scanned: each check emits its own signal.
type gitRepo struct {
	p, branch string
	project   string // the project the rows belong to: p, or the repo whose worktree p is
	at        string // a linked worktree's folder, "" for the repo itself
	w, warn   io.Writer
	now       time.Time
	failed    bool // any required read failed, including a linked worktree
}

// emit writes one signal. A worktree's rows are keyed by its folder, so
// each worktree's work is its own row, and say which worktree it is.
func (r gitRepo) emit(key string, kind Kind, text string, observed time.Time, state string) {
	id := "git:" + key
	if r.at != "" {
		id += ":" + r.at
		text += " · worktree " + filepath.Base(r.at)
	}
	WriteLine(r.w, Signal{ID: ID(r.project, id), Project: r.project, Kind: kind, Text: text, Observed: observed, State: state})
}

// must runs git, reporting a failure on warn; "" then.
func (r *gitRepo) must(args ...string) string {
	out, err := gitOut(r.p, args...)
	if err != nil {
		fmt.Fprintf(r.warn, "sous-signal-git: git %s failed in %s: %v\n", args[0], r.p, err)
		r.failed = true
		return ""
	}
	return out
}

// check reads the branch, its upstream, how far ahead it is and what is
// uncommitted from one git status, and emits what it found.
func (r *gitRepo) check() {
	out, err := project.Git(r.p, "status", "--porcelain", "--branch")
	if err != nil {
		fmt.Fprintf(r.warn, "sous-signal-git: git status failed in %s: %v\n", r.p, err)
		r.failed = true
		return
	}
	header, files, _ := strings.Cut(out, "\n")
	st := parseStatusHeader(header)
	r.branch = st.branch
	r.dirty(strings.TrimSpace(files))
	r.unpushed(st)
}

// branchStatus is the first line of git status --branch.
type branchStatus struct {
	branch   string // "(detached)" when HEAD is not on a branch
	upstream bool   // the branch tracks one that still exists
	ahead    string // commits ahead of it; "" when git did not count them
}

// parseStatusHeader reads "## main...origin/main [ahead 2, behind 1]" and
// its other forms: no upstream ("## main"), a gone upstream ("[gone]"),
// a detached HEAD ("## HEAD (no branch)") and a branch with no commits
// yet ("## No commits yet on main").
func parseStatusHeader(h string) branchStatus {
	h = strings.TrimPrefix(h, "## ")
	if strings.HasPrefix(h, "HEAD (no branch)") {
		return branchStatus{branch: "(detached)"}
	}
	for _, fresh := range []string{"No commits yet on ", "Initial commit on "} {
		if b, ok := strings.CutPrefix(h, fresh); ok {
			return branchStatus{branch: b}
		}
	}
	track := ""
	if i := strings.Index(h, " ["); i >= 0 && strings.HasSuffix(h, "]") {
		h, track = h[:i], h[i+2:len(h)-1]
	}
	branch, _, upstream := strings.Cut(h, "...")
	st := branchStatus{branch: branch, upstream: upstream && track != "gone"}
	if !st.upstream {
		return st
	}
	st.ahead = "0"
	for part := range strings.SplitSeq(track, ", ") {
		if n, ok := strings.CutPrefix(part, "ahead "); ok {
			st.ahead = n
		} else if part != "" && !strings.HasPrefix(part, "behind ") {
			st.ahead = "" // a form this does not know: count them instead
		}
	}
	return st
}

// dirty: uncommitted files. The summary counts files; the state follows
// what changed in them (the diff itself, and which files are new), so any
// further edit ends a snooze. The content of new untracked files is not
// read.
func (r *gitRepo) dirty(st string) {
	if st == "" {
		return
	}
	diff := r.must("diff", "HEAD")
	if r.failed {
		return // an incomplete fingerprint must not replace the known state
	}
	sum := sha256.Sum256([]byte(st + "\x00" + diff))
	r.emit("dirty", Unfinished, text.Plural(countLines(st), "file")+" uncommitted · "+r.branch, r.now, hex.EncodeToString(sum[:8]))
}

// unpushed: commits ahead of the upstream, or a branch other than the
// default that was never pushed.
func (r *gitRepo) unpushed(st branchStatus) {
	if st.upstream {
		n := st.ahead
		if n == "" {
			n = r.must("rev-list", "--count", "@{u}..HEAD")
		}
		if n != "" && n != "0" {
			r.emit("ahead", Unfinished, fmt.Sprintf("%s commits unpushed · %s", n, r.branch), r.now, "")
		}
		return
	}
	if r.branch != r.defaultBranch() && r.branch != "(detached)" {
		r.emit("no-upstream", Unfinished, r.branch+" has no upstream", r.now, "")
	}
}

func (r gitRepo) defaultBranch() string {
	def, _ := gitOut(r.p, "symbolic-ref", "-q", "--short", "refs/remotes/origin/HEAD")
	if def = strings.TrimPrefix(def, "origin/"); def != "" {
		return def
	}
	if _, err := gitOut(r.p, "show-ref", "-q", "--verify", "refs/heads/main"); err == nil {
		return "main"
	}
	return "master"
}

// lastCommit is information for here: the subject, dated by the commit.
func (r *gitRepo) lastCommit() {
	out := r.must("log", "-1", "--format=%cI%x00%s")
	if out == "" {
		return
	}
	ts, subject, _ := strings.Cut(out, "\x00")
	when := r.now
	if t, err := time.Parse(time.RFC3339, strings.TrimSpace(ts)); err == nil {
		when = t
	}
	r.emit("last_commit", Info, subject, when, "")
}
