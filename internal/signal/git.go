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
		// No HEAD is fine for a fresh repo; anything git itself cannot read is not.
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
	must := func(args ...string) string {
		out, err := gitOut(p, args...)
		if err != nil {
			fmt.Fprintf(warn, "sous-signal-git: git %s failed in %s: %v\n", args[0], p, err)
		}
		return out
	}
	branch, _ := gitOut(p, "branch", "--show-current")
	if branch == "" {
		branch = "(detached)"
	}
	def, _ := gitOut(p, "symbolic-ref", "-q", "--short", "refs/remotes/origin/HEAD")
	def = strings.TrimPrefix(def, "origin/")
	if def == "" {
		if _, err := gitOut(p, "show-ref", "-q", "--verify", "refs/heads/main"); err == nil {
			def = "main"
		} else {
			def = "master"
		}
	}
	emit := func(key string, kind Kind, text string, observed time.Time, state ...string) {
		WriteLine(w, Signal{ID: ID(p, "git:"+key), Project: p, Kind: kind, Text: text, Observed: observed, State: strings.Join(state, "")})
	}

	if st := must("status", "--porcelain"); st != "" {
		// The summary counts files; the state follows their content, so more
		// edits to the same file end a snooze.
		stat, _ := gitOut(p, "diff", "HEAD", "--numstat")
		sum := sha256.Sum256([]byte(st + "\x00" + stat))
		emit("dirty", Unfinished, fmt.Sprintf("%d files uncommitted · %s", countLines(st), branch), now, hex.EncodeToString(sum[:8]))
	}
	if up, err := gitOut(p, "rev-parse", "--abbrev-ref", "-q", "@{u}"); err == nil && up != "" {
		if n := must("rev-list", "--count", "@{u}..HEAD"); n != "" && n != "0" {
			emit("ahead", Unfinished, fmt.Sprintf("%s commits unpushed · %s", n, branch), now)
		}
	} else if branch != def && branch != "(detached)" {
		emit("no-upstream", Unfinished, branch+" has no upstream", now)
	}
	if st := must("stash", "list"); st != "" {
		emit("stash", Unfinished, strconv.Itoa(countLines(st))+" stashes", now)
	}
	if subject, err := gitOut(p, "log", "-1", "--format=%s"); err == nil {
		when := now
		if ts, err := gitOut(p, "log", "-1", "--format=%cI"); err == nil {
			if t, err := time.Parse(time.RFC3339, ts); err == nil {
				when = t
			}
		}
		emit("last_commit", Info, subject, when)
	}
	return true
}
