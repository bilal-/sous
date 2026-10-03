package filing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/backend"
	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/testutil"
	"github.com/bilal-/sous/internal/thread"
)

// fakeBackend is a third-party backend executable: it records every call,
// files into a text file under the project, and answers status/close from
// that file. It is the contract as a plugin author would implement it.
const fakeBackend = `#!/bin/sh
echo "$*" >> "$SOUS_FAKE_CALLS"
case "$1" in
  detect) [ -f "$2/TRACKER" ] && exit 0; exit 1;;
  file) read -r body
        p=$(printf '%s' "$body" | sed 's/.*"project":"\([^"]*\)".*/\1/')
        id=$(printf '%s' "$body" | sed 's/.*"id":\([0-9]*\).*/\1/')
        grep -q "^$id " "$p/TRACKER" 2>/dev/null || echo "$id open" >> "$p/TRACKER"
        echo "fake:$id";;
  status) id=${3#fake:}; line=$(grep "^$id " "$2/TRACKER" 2>/dev/null)
          [ -z "$line" ] && { echo unknown; exit 0; }; echo "${line#* }";;
  close) id=${3#fake:}; sed "s/^$id open/$id closed/" "$2/TRACKER" > "$2/TRACKER.new" && mv "$2/TRACKER.new" "$2/TRACKER";;
  slow) sleep 30;;
esac
`

type env struct {
	f     *Filer
	s     *store.Store
	calls string
	ws    string
}

func setup(t *testing.T) env {
	dir := t.TempDir()
	bin := filepath.Join(dir, "sous-backend-fake")
	os.WriteFile(bin, []byte(fakeBackend), 0o755)
	calls := filepath.Join(dir, "calls")
	t.Setenv("SOUS_FAKE_CALLS", calls)
	ws := filepath.Join(dir, "ws")
	s := &store.Store{Home: filepath.Join(dir, ".sous")}
	cfg := &config.Config{Roots: []string{ws}}
	return env{f: &Filer{Store: s, Cfg: cfg, Backends: backend.Registry.Discover("", nil, []string{bin})}, s: s, calls: calls, ws: ws}
}

func (e env) repo(t *testing.T, rel, remote string, tracker bool) project.Project {
	p := testutil.Repo(t, filepath.Join(e.ws, rel), true, remote)
	if tracker {
		os.WriteFile(filepath.Join(p, "TRACKER"), nil, 0o644)
	}
	real, _ := filepath.EvalSymlinks(p)
	return project.Describe(real)
}

func (e env) note(t *testing.T, p project.Project, text string) int {
	id, err := thread.Note(e.s, p, thread.Me, text, "human", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestFileRoutesToDetectedBackendAndIsIdempotent(t *testing.T) {
	e := setup(t)
	p := e.repo(t, "a/r", "", true)
	id := e.note(t, p, "x")
	ref, err := e.f.File(context.Background(), id, false)
	if err != nil || ref != "fake:1" {
		t.Fatal(ref, err)
	}
	ref2, _ := e.f.File(context.Background(), id, false)
	if ref2 != ref {
		t.Fatal("second file returns the recorded ref")
	}
	b, _ := os.ReadFile(e.calls)
	if strings.Count(string(b), "file") != 1 {
		t.Fatalf("backend must be asked to file once:\n%s", b)
	}
}

func TestFileErrors(t *testing.T) {
	e := setup(t)
	plain := e.repo(t, "a/plain", "", false)
	if _, err := e.f.File(context.Background(), e.note(t, plain, "x"), false); !errors.Is(err, ErrNoTracker) {
		t.Fatalf("no tracker: %v", err)
	}
	if _, err := e.f.File(context.Background(), 99, false); !errors.Is(err, thread.ErrNotFound) {
		t.Fatalf("missing thread: %v", err)
	}
	e.f.Cfg.Projects = map[string]map[string]string{"a/plain": {"backend": "jira"}}
	if _, err := e.f.File(context.Background(), 1, false); !errors.Is(err, backend.ErrUnknownBackend) {
		t.Fatalf("declared-but-missing backend: %v", err)
	}
}

func TestFileRefusesWorktreeUnlessExplicit(t *testing.T) {
	e := setup(t)
	main := e.repo(t, "a/r", "", true)
	wt := filepath.Join(e.ws, "a", "r-wt")
	testutil.Git(t, main.Path, "worktree", "add", "-q", wt, "-b", "feature")
	os.WriteFile(filepath.Join(wt, "TRACKER"), nil, 0o644)
	real, _ := filepath.EvalSymlinks(wt)
	id := e.note(t, project.Describe(real), "from worktree")
	if _, err := e.f.File(context.Background(), id, false); !errors.Is(err, ErrWorktree) {
		t.Fatalf("%v", err)
	}
	if _, err := e.f.File(context.Background(), id, true); err != nil {
		t.Fatalf("explicit allows it: %v", err)
	}
}

func TestRefileWhenMarkerMissing(t *testing.T) {
	e := setup(t)
	p := e.repo(t, "a/r", "", true)
	id := e.note(t, p, "x")
	e.f.File(context.Background(), id, false)
	os.WriteFile(filepath.Join(p.Path, "TRACKER"), nil, 0o644) // the user wiped the tracker
	if _, err := e.f.File(context.Background(), id, false); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(p.Path, "TRACKER"))
	if !strings.Contains(string(b), "1 open") {
		t.Fatalf("must re-file:\n%s", b)
	}
}

func TestCloseUpstreamAndReconcile(t *testing.T) {
	e := setup(t)
	p := e.repo(t, "a/r", "", true)
	a := e.note(t, p, "closed by me via --close")
	b := e.note(t, p, "ticked upstream")
	c := e.note(t, p, "never filed")
	for _, id := range []int{a, b} {
		if _, err := e.f.File(context.Background(), id, false); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.f.CloseUpstream(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if err := e.f.CloseUpstream(context.Background(), c); err != nil {
		t.Fatal("unfiled thread: close upstream is a no-op")
	}
	// b closed in the tracker by someone else.
	tr := filepath.Join(p.Path, "TRACKER")
	body, _ := os.ReadFile(tr)
	os.WriteFile(tr, []byte(strings.Replace(string(body), "2 open", "2 closed", 1)), 0o644)

	now := time.Now()
	views, _ := thread.Open(e.s, now)
	got := e.f.Reconcile(context.Background(), views, now)
	var ids []int
	for _, v := range got {
		ids = append(ids, v.ID)
	}
	// a and b are closed upstream → dropped and closed with provenance; c stays.
	if len(got) != 1 || got[0].ID != c {
		t.Fatalf("reconcile kept %v", ids)
	}
	for _, id := range []int{a, b} {
		th, _ := thread.Get(e.s, id)
		if th.Closed == nil || th.ClosedBy != "upstream" {
			t.Fatalf("thread %d: %+v", id, th)
		}
	}
}

func TestReconcileReportsErrorsAndHonoursDeadline(t *testing.T) {
	e := setup(t)
	p := e.repo(t, "a/r", "", true)
	id := e.note(t, p, "x")
	e.f.File(context.Background(), id, false)
	// A ref this setup has no backend for.
	raw, _ := os.ReadFile(filepath.Join(e.s.Home, "threads.json"))
	os.WriteFile(filepath.Join(e.s.Home, "threads.json"), []byte(strings.Replace(string(raw), `"ref": "fake:1"`, `"ref": "jira:X-1"`, 1)), 0o644)
	now := time.Now()
	views, _ := thread.Open(e.s, now)
	got := e.f.Reconcile(context.Background(), views, now)
	if len(got) != 1 || got[0].Upstream != "error" || !strings.Contains(got[0].UpstreamErr, "jira not configured") {
		t.Fatalf("%+v", got)
	}
	// An already-expired context: nothing hangs, nothing is closed.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	got = e.f.Reconcile(ctx, views, now)
	if time.Since(start) > 2*time.Second || len(got) != 1 {
		t.Fatalf("expired ctx: %v %+v", time.Since(start), got)
	}
}

func TestCurrentPathFollowsRemote(t *testing.T) {
	e := setup(t)
	p := e.repo(t, "a/old", "git@github.com:o/r.git", false)
	id := e.note(t, p, "x")
	np := filepath.Join(e.ws, "a", "new")
	os.Rename(p.Path, np)
	th, _ := thread.Get(e.s, id)
	real, _ := filepath.EvalSymlinks(np)
	if got := e.f.CurrentPath(th); got != real {
		t.Fatalf("moved repo resolved by remote: %s", got)
	}
	th.Remote = nil
	if got := e.f.CurrentPath(th); got != p.Path {
		t.Fatal("no remote: stored path")
	}
}

// Review: if closing the note here fails, it stays on the board with the
// reason, instead of vanishing while still open on disk.
func TestReconcileKeepsANoteItCouldNotClose(t *testing.T) {
	st := &store.Store{Home: t.TempDir()}
	ref := "fake:1"
	now := time.Now()
	views := []thread.View{{Thread: thread.Thread{ID: 99, Text: "x", Kind: thread.Me, Ref: &ref}}} // 99 is not in the store
	f := &Filer{Store: st, Backends: []backend.Backend{{Name: "fake", Argv: []string{"/bin/sh", "-c", `echo closed`, "sh"}}}}
	out := f.Reconcile(context.Background(), views, now)
	if len(out) != 1 || out[0].Upstream != "error" || !strings.Contains(out[0].UpstreamErr, "could not be closed here") {
		t.Fatalf("%+v", out)
	}
}
