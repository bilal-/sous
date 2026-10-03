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

	"github.com/bilal-/sous/internal/text"
)

func gitOut(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// ScanGit is the built-in git plugin: dirty, ahead, stash, no-upstream, last commit.
// It never fails as a whole; a bad path is warned and skipped.
func ScanGit(paths []string, w, warn io.Writer, now time.Time) error {
	if _, err := exec.LookPath("git"); err != nil {
		fmt.Fprintln(warn, "git not installed")
		return errors.New("git not installed")
	}
	// ~5 git processes per repo; run repos concurrently, each into its own
	// buffer so lines never interleave. Order is irrelevant downstream.
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	var unreadable int
	for _, p := range paths {
		if _, err := os.Lstat(filepath.Join(p, ".git")); err != nil {
			fmt.Fprintf(warn, "sous-signal-git: not a repo: %s\n", p)
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
	r := gitRepo{p: p, w: w, warn: warn, now: now}
	r.branch, _ = gitOut(p, "branch", "--show-current")
	if r.branch == "" {
		r.branch = "(detached)"
	}
	r.dirty()
	r.unpushed()
	if st := r.must("stash", "list"); st != "" {
		r.emit("stash", Unfinished, strconv.Itoa(countLines(st))+" stashes", now, "")
	}
	r.lastCommit()
	return true
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
	w, warn   io.Writer
	now       time.Time
}

func (r gitRepo) emit(key string, kind Kind, text string, observed time.Time, state string) {
	WriteLine(r.w, Signal{ID: ID(r.p, "git:"+key), Project: r.p, Kind: kind, Text: text, Observed: observed, State: state})
}

// must runs git, reporting a failure on warn; "" then.
func (r gitRepo) must(args ...string) string {
	out, err := gitOut(r.p, args...)
	if err != nil {
		fmt.Fprintf(r.warn, "sous-signal-git: git %s failed in %s: %v\n", args[0], r.p, err)
	}
	return out
}

// dirty: uncommitted files. The summary counts files; the state follows
// what changed in them (the diff itself, and which files are new), so any
// further edit ends a snooze. The content of new untracked files is not
// read.
func (r gitRepo) dirty() {
	st := r.must("status", "--porcelain")
	if st == "" {
		return
	}
	diff, _ := gitOut(r.p, "diff", "HEAD")
	sum := sha256.Sum256([]byte(st + "\x00" + diff))
	r.emit("dirty", Unfinished, text.Plural(countLines(st), "file")+" uncommitted · "+r.branch, r.now, hex.EncodeToString(sum[:8]))
}

// unpushed: commits ahead of the upstream, or a branch other than the
// default that was never pushed.
func (r gitRepo) unpushed() {
	if up, err := gitOut(r.p, "rev-parse", "--abbrev-ref", "-q", "@{u}"); err == nil && up != "" {
		if n := r.must("rev-list", "--count", "@{u}..HEAD"); n != "" && n != "0" {
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
func (r gitRepo) lastCommit() {
	subject, err := gitOut(r.p, "log", "-1", "--format=%s")
	if err != nil {
		return
	}
	when := r.now
	if ts, err := gitOut(r.p, "log", "-1", "--format=%cI"); err == nil {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			when = t
		}
	}
	r.emit("last_commit", Info, subject, when, "")
}
