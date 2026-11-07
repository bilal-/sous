package signal

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/store"
)

func TestGitScanFailureKeepsKnownWork(t *testing.T) {
	for _, kind := range []string{"index", "worktree index", "missing repository"} {
		t.Run(kind, func(t *testing.T) {
			home := t.TempDir()
			root := repo(t, filepath.Join(home, "api"), true)
			healthy := repo(t, filepath.Join(home, "site"), true)
			dirty := root
			index := filepath.Join(root, ".git", "index")
			if kind == "worktree index" {
				dirty = filepath.Join(home, "topic")
				git(t, root, "worktree", "add", "-q", "-b", "feat/topic", dirty)
				b, err := os.ReadFile(filepath.Join(dirty, ".git"))
				if err != nil {
					t.Fatal(err)
				}
				index = filepath.Join(strings.TrimSpace(strings.TrimPrefix(string(b), "gitdir:")), "index")
			}
			if err := os.WriteFile(filepath.Join(dirty, "unfinished"), []byte("work"), 0600); err != nil {
				t.Fatal(err)
			}
			st := &store.Store{Home: t.TempDir()}
			now := time.Now()
			collect := func(at time.Time) ([]Observed, error) {
				t.Helper()
				var out, warn bytes.Buffer
				scanErr := ScanGit([]string{root, healthy}, &out, &warn, at)
				state := StatusOK
				if scanErr != nil {
					state = StatusFailed
				}
				sigs, bad := ReadLinesLenient(&out)
				if bad != 0 {
					t.Fatal(bad)
				}
				c := Collected{Plugins: []PluginStatus{{Name: "git", Status: state}}}
				for _, s := range sigs {
					c.Signals = append(c.Signals, Tagged{Signal: s, Plugin: "git"})
				}
				rows, err := Observe(st, c, []string{root, healthy}, at)
				if err != nil {
					t.Fatal(err)
				}
				return rows, scanErr
			}
			before, err := collect(now)
			if err != nil {
				t.Fatal(err)
			}
			id := ""
			for _, row := range before {
				if strings.Contains(row.Text, "uncommitted") {
					id = row.ID
				}
			}
			if id == "" {
				t.Fatal("fixture did not show unfinished work")
			}
			if kind == "missing repository" {
				if err := os.RemoveAll(root); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(index, []byte("garbage"), 0600); err != nil {
				t.Fatal(err)
			}
			after, err := collect(now.Add(time.Second))
			if err == nil {
				t.Fatal("incomplete scan reported success")
			}
			stale, fresh := false, false
			for _, row := range after {
				if row.ID == id {
					stale = row.Stale
				}
				if row.Project == healthy && row.Kind == Info {
					fresh = !row.Stale
				}
			}
			if !stale || !fresh {
				t.Fatalf("stale work=%v fresh healthy repo=%v: %+v", stale, fresh, after)
			}
		})
	}
}

// git status says "[gone]" both for an upstream that was deleted and for
// one whose ref names an object git cannot read. Only the first is "no
// upstream"; the second must fail the scan, so known work stays (stale)
// rather than reading as pushed.
func TestGitUnreadableUpstreamFailsTheScan(t *testing.T) {
	ws := t.TempDir()
	bare := filepath.Join(ws, "up.git")
	git(t, ws, "init", "-q", "--bare", bare)
	r := repo(t, filepath.Join(ws, "api"), true)
	git(t, r, "remote", "add", "origin", bare)
	git(t, r, "push", "-q", "-u", "origin", "HEAD:main")
	git(t, r, "commit", "-q", "--allow-empty", "-m", "unpushed")
	tracking := filepath.Join(r, ".git", "refs", "remotes", "origin", "main")

	if sigs, _ := scan(t, r); find(sigs, "1 commits unpushed") == nil {
		t.Fatalf("setup: no unpushed row: %+v", sigs)
	}
	if err := os.WriteFile(tracking, []byte(strings.Repeat("1", 40)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, warn bytes.Buffer
	if err := ScanGit([]string{r}, &out, &warn, time.Now()); err == nil {
		t.Fatalf("an unreadable upstream must fail the scan; warn=%q out=%q", warn.String(), out.String())
	}

	if err := os.Remove(tracking); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	warn.Reset()
	if err := ScanGit([]string{r}, &out, &warn, time.Now()); err != nil {
		t.Fatalf("a deleted upstream is no upstream, not a failure: %v %s", err, warn.String())
	}
}
