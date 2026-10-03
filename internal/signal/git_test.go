package signal

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/testutil"
)

func git(t *testing.T, dir string, args ...string)      { testutil.Git(t, dir, args...) }
func repo(t *testing.T, dir string, commit bool) string { return testutil.Repo(t, dir, commit, "") }

func scan(t *testing.T, paths ...string) ([]Signal, string) {
	var out, warn bytes.Buffer
	ScanGit(paths, &out, &warn, time.Now())
	sigs, err := readLines(&out)
	if err != nil {
		t.Fatalf("invalid JSONL: %v\n%s", err, out.String())
	}
	return sigs, warn.String()
}
func find(sigs []Signal, sub string) *Signal {
	for i := range sigs {
		if strings.Contains(sigs[i].Text, sub) {
			return &sigs[i]
		}
	}
	return nil
}

func TestScanGit(t *testing.T) {
	ws := t.TempDir()
	clean := repo(t, filepath.Join(ws, "clean"), true)
	dirty := repo(t, filepath.Join(ws, "dirty one"), true)
	os.WriteFile(filepath.Join(dirty, "x"), nil, 0o644)
	os.WriteFile(filepath.Join(dirty, "y"), nil, 0o644)
	stashed := repo(t, filepath.Join(ws, "stashed"), true)
	os.WriteFile(filepath.Join(stashed, "z"), []byte("z"), 0o644)
	git(t, stashed, "add", "z")
	git(t, stashed, "stash", "-q")
	empty := repo(t, filepath.Join(ws, "empty"), false)
	feature := repo(t, filepath.Join(ws, "feature"), true)
	git(t, feature, "checkout", "-q", "-b", "feat/x")
	ahead := repo(t, filepath.Join(ws, "ahead"), true)
	bare := testutil.Bare(t, filepath.Join(ws, "ahead.git"))
	git(t, ahead, "remote", "add", "origin", bare)
	git(t, ahead, "push", "-q", "-u", "origin", "main")
	git(t, ahead, "commit", "-q", "--allow-empty", "-m", "local only")

	sigs, _ := scan(t, clean)
	if len(sigs) != 1 || sigs[0].Kind != Info || sigs[0].Text != "init" || sigs[0].Observed.IsZero() {
		t.Fatalf("clean: %+v", sigs)
	}
	sigs, _ = scan(t, dirty)
	d := find(sigs, "uncommitted")
	if d == nil || d.Text != "2 files uncommitted · main" || d.Kind != Unfinished || d.Project != dirty || d.V != 0 || !strings.HasPrefix(d.ID, "s:") || len(d.ID) != 14 {
		t.Fatalf("dirty: %+v", d)
	}
	sigs2, _ := scan(t, dirty)
	if find(sigs2, "uncommitted").ID != d.ID {
		t.Fatal("id must be stable")
	}
	if sigs, _ = scan(t, stashed); find(sigs, "1 stashes") == nil {
		t.Fatal("stash")
	}
	if sigs, _ = scan(t, feature); find(sigs, "feat/x has no upstream") == nil {
		t.Fatal("no-upstream")
	}
	if sigs, _ = scan(t, clean); find(sigs, "no upstream") != nil {
		t.Fatal("default branch must not report no-upstream")
	}
	if sigs, _ = scan(t, ahead); find(sigs, "1 commits unpushed · main") == nil {
		t.Fatal("ahead")
	}
	if sigs, _ = scan(t, empty); len(sigs) != 0 {
		t.Fatalf("empty repo must emit nothing: %+v", sigs)
	}
	sigs, warn := scan(t, clean, filepath.Join(ws, "nope"), stashed)
	if find(sigs, "1 stashes") == nil || !strings.Contains(warn, "nope") {
		t.Fatalf("must continue past a bad path: %+v %q", sigs, warn)
	}
}

// #4: a git subcommand failing must not read as "clean". With an
// unreadable index, `git status` exits 128; that must reach the warn stream.
func TestScanGitWarnsWhenStatusFails(t *testing.T) {
	ws := t.TempDir()
	r := repo(t, filepath.Join(ws, "broken"), true)
	os.WriteFile(filepath.Join(r, "x"), nil, 0o644)
	git(t, r, "add", "x")
	os.WriteFile(filepath.Join(r, ".git", "index"), []byte("garbage"), 0o644)
	var out, warn bytes.Buffer
	ScanGit([]string{r}, &out, &warn, time.Now())
	if !strings.Contains(warn.String(), "broken") || !strings.Contains(warn.String(), "status") {
		t.Fatalf("expected a warning naming the repo and the failing command, got %q", warn.String())
	}
}

// Git missing, or a repo git cannot read, must not look like
// "clean" — the plugin reports failure so observations are kept as stale.
func TestScanGitFailsLoudWithoutGit(t *testing.T) {
	ws := t.TempDir()
	r := repo(t, filepath.Join(ws, "r"), true)
	t.Setenv("PATH", t.TempDir())
	var out, warn bytes.Buffer
	if err := ScanGit([]string{r}, &out, &warn, time.Now()); err == nil || !strings.Contains(warn.String(), "git not installed") {
		t.Fatalf("no git must be an error: err=%v warn=%q", err, warn.String())
	}
}

func TestScanGitUnreadableRepoIsAnError(t *testing.T) {
	ws := t.TempDir()
	r := repo(t, filepath.Join(ws, "r"), true)
	os.WriteFile(filepath.Join(r, ".git", "HEAD"), []byte("ref: refs/heads/nope\n"), 0o644)
	os.WriteFile(filepath.Join(r, ".git", "HEAD"), []byte("garbage"), 0o644)
	var out, warn bytes.Buffer
	err := ScanGit([]string{r}, &out, &warn, time.Now())
	if err == nil || !strings.Contains(warn.String(), "unreadable") {
		t.Fatalf("broken HEAD must be reported, not read as no commits: err=%v warn=%q", err, warn.String())
	}
	// A genuinely empty repo is still silent and not an error.
	e := repo(t, filepath.Join(ws, "empty"), false)
	out.Reset()
	warn.Reset()
	if err := ScanGit([]string{e}, &out, &warn, time.Now()); err != nil || warn.Len() != 0 || out.Len() != 0 {
		t.Fatalf("empty repo: err=%v warn=%q out=%q", err, warn.String(), out.String())
	}
}

func TestCountLines(t *testing.T) {
	for in, want := range map[string]int{"": 0, "a": 1, "a\nb": 2, "a\nb\n": 3} {
		if got := countLines(in); got != want {
			t.Errorf("countLines(%q)=%d want %d", in, got, want)
		}
	}
}

// More edits to the same file change the dirty row's state, so a snooze on
// it ends; the one line text stays "1 files uncommitted".
func TestGitDirtyStateFollowsEdits(t *testing.T) {
	r := repo(t, filepath.Join(t.TempDir(), "acme/api"), true)
	os.WriteFile(filepath.Join(r, "a.txt"), []byte("one\n"), 0o644)
	git(t, r, "add", "a.txt")
	git(t, r, "commit", "-q", "-m", "a")
	state := func() (string, string) {
		var out bytes.Buffer
		ScanGit([]string{r}, &out, io.Discard, time.Now())
		sigs, _ := readLines(&out)
		for _, s := range sigs {
			if strings.Contains(s.Text, "uncommitted") {
				return s.Text, s.State
			}
		}
		t.Fatalf("no dirty row: %+v", sigs)
		return "", ""
	}
	os.WriteFile(filepath.Join(r, "a.txt"), []byte("one\ntwo\n"), 0o644)
	text1, s1 := state()
	os.WriteFile(filepath.Join(r, "a.txt"), []byte("one\ntwo\nthree\n"), 0o644)
	text2, s2 := state()
	if text1 != text2 || s1 == "" || s1 == s2 {
		t.Fatalf("%q %q / %q %q", text1, s1, text2, s2)
	}
}

// An edit that keeps the same line counts (a word swapped) and a
// new untracked file both change the dirty state.
func TestGitDirtyStateFollowsContent(t *testing.T) {
	r := repo(t, filepath.Join(t.TempDir(), "acme/api"), true)
	os.WriteFile(filepath.Join(r, "a.txt"), []byte("one\n"), 0o644)
	git(t, r, "add", "a.txt")
	git(t, r, "commit", "-q", "-m", "a")
	state := func() string {
		var out bytes.Buffer
		ScanGit([]string{r}, &out, io.Discard, time.Now())
		sigs, _ := readLines(&out)
		for _, s := range sigs {
			if strings.Contains(s.Text, "uncommitted") {
				return s.State
			}
		}
		return ""
	}
	os.WriteFile(filepath.Join(r, "a.txt"), []byte("two\n"), 0o644)
	s1 := state()
	os.WriteFile(filepath.Join(r, "a.txt"), []byte("six\n"), 0o644)
	s2 := state()
	os.WriteFile(filepath.Join(r, "new.txt"), []byte("x"), 0o644)
	s3 := state()
	if s1 == "" || s1 == s2 || s2 == s3 {
		t.Fatalf("%q %q %q", s1, s2, s3)
	}
}
