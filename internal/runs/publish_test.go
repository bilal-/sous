package runs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/runner"
	"github.com/bilal-/sous/internal/runner/runnertest"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/testutil"
	"github.com/bilal-/sous/internal/thread"
)

// workOn makes a commit on branch in repo, adding file, and returns the
// commit it started from and the new one.
func workOn(t *testing.T, repo, branch, file string) (string, string) {
	t.Helper()
	base := strings.TrimSpace(testutil.Git(t, repo, "rev-parse", "HEAD"))
	testutil.Git(t, repo, "checkout", "-q", "-b", branch)
	os.MkdirAll(filepath.Dir(filepath.Join(repo, file)), 0o755)
	os.WriteFile(filepath.Join(repo, file), []byte("work\n"), 0o600)
	testutil.Git(t, repo, "add", file)
	testutil.Git(t, repo, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "-m", "work")
	commit := strings.TrimSpace(testutil.Git(t, repo, "rev-parse", "HEAD"))
	testutil.Git(t, repo, "checkout", "-q", "-")
	return base, commit
}

func readyRunner(t *testing.T, base, commit, branch string) runner.Runner {
	return runnertest.Fake(t, `describe) echo '{"v":0,"calls":["start","status","stop","approve","discard"],"fields":["phase","result"]}';;
start) cat > /dev/null; echo fake:1;;
status) echo '{"v":0,"state":"done","phase":"ready","result":{"base":"`+base+`","commit":"`+commit+`"}}';;
approve) cat > /dev/null; echo '{"v":0,"branch":"`+branch+`","commit":"`+commit+`","base":"`+base+`"}';;
discard) ;;
stop) ;;`)
}

// Approving pushes exactly the approved commit and opens a draft PR; a
// failure part way is recorded, and approving again carries on from there.
func TestApprovePublishesInStages(t *testing.T) {
	origin := testutil.Bare(t, filepath.Join(t.TempDir(), "origin.git"))
	repo := testutil.Repo(t, t.TempDir(), true, origin)
	base, commit := workOn(t, repo, "sous/run-1", "fix.txt")
	s := &store.Store{Home: t.TempDir()}
	opened, fails := 0, 1
	d := &Dispatcher{Store: s, Runners: []runner.Runner{readyRunner(t, base, commit, "sous/run-1")}, Now: time.Now,
		OpenDraft: func(p, branch, title, body string) (string, error) {
			opened++
			if fails > 0 {
				fails--
				return "", errors.New("gh: not logged in")
			}
			return "https://git.example.org/acme/api/pull/7", nil
		}}
	id, _, _ := d.Start(context.Background(), project.Project{Path: repo}, "fix it", "", "fake", "", "")
	if _, err := d.Approve(context.Background(), id, base, ""); !errors.Is(err, ErrCommitChanged) {
		t.Fatalf("another commit: %v", err)
	}
	pub, err := d.Approve(context.Background(), id, commit, "op-0123456789abcdef")
	var pe PublishError
	if !errors.As(err, &pe) || pe.Stage != "pr" || pub.Stage != thread.PubPushed {
		t.Fatalf("%+v %v", pub, err)
	}
	if got := strings.TrimSpace(testutil.Git(t, origin, "rev-parse", "refs/heads/sous/run-1")); got != commit {
		t.Fatalf("pushed %q, want %q", got, commit)
	}
	pub, err = d.Approve(context.Background(), id, commit, "op-0123456789abcdef")
	th, _ := thread.Get(s, id)
	if err != nil || pub.Stage != thread.PubPublished || pub.PR == "" || opened != 2 || th.Run.Publication.PR != pub.PR {
		t.Fatalf("%+v %v %d", pub, err, opened)
	}
	if pub, err := d.Approve(context.Background(), id, commit, ""); err != nil || opened != 2 || pub.Stage != thread.PubPublished {
		t.Fatalf("again: %+v %v", pub, err)
	}
	if err := d.Discard(context.Background(), id); !errors.Is(err, ErrCommitChanged) {
		t.Fatalf("discarding a published run: %v", err)
	}
}

// A runner's private state is never published, nor a branch outside the
// runner's namespace.
func TestApproveRefusesPrivateStateAndForeignBranches(t *testing.T) {
	origin := testutil.Bare(t, filepath.Join(t.TempDir(), "origin.git"))
	repo := testutil.Repo(t, t.TempDir(), true, origin)
	base, commit := workOn(t, repo, "sous/run-2", ".orchid/roadmap.md")
	s := &store.Store{Home: t.TempDir()}
	d := &Dispatcher{Store: s, Runners: []runner.Runner{readyRunner(t, base, commit, "sous/run-2")}, Now: time.Now,
		OpenDraft: func(string, string, string, string) (string, error) { return "x", nil }}
	id, _, _ := d.Start(context.Background(), project.Project{Path: repo}, "fix it", "", "fake", "", "")
	if _, err := d.Approve(context.Background(), id, commit, ""); !errors.Is(err, ErrPrivateState) {
		t.Fatalf("%v", err)
	}
	if out, _ := project.Git(origin, "branch", "--list", "sous/run-2"); strings.TrimSpace(out) != "" {
		t.Fatal("pushed anyway")
	}
	d.Runners = []runner.Runner{readyRunner(t, base, commit, "main")}
	id, _, _ = d.Start(context.Background(), project.Project{Path: repo}, "fix it again", "", "fake", "", "")
	if _, err := d.Approve(context.Background(), id, commit, ""); !errors.Is(err, ErrBadBranch) {
		t.Fatalf("%v", err)
	}
}
