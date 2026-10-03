package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/testutil"
)

// startedRun makes a run folder as Start would, with a fake watcher that
// exits at once; tests then play the watcher themselves.
func startedRun(t *testing.T, name string, limit time.Duration) (*Agent, string, string) {
	t.Helper()
	testutil.FakeBin(t, name, "")
	exe, _ := fakeSous(t)
	a, _ := Builtin(name, t.TempDir(), exe, limit)
	ref, err := a.Start(Request{ID: 3, UID: "u3", Project: gitRepo(t), Brief: "task"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return !watcherAlive(a.dir("u3")) }) // the fake watcher only records its call
	return a, ref, a.dir("u3")
}

// claudeSays fakes claude -p --output-format json: noise on stderr, then
// one JSON result line.
func claudeSays(t *testing.T, msg string, code int) {
	testutil.FakeBin(t, "claude", `echo "working" >&2
printf '{"type":"result","result":"%s","session_id":"s-1"}\n' "`+msg+`"
exit `+fmt.Sprint(code))
}

func TestWatchThenStatus(t *testing.T) {
	for _, c := range []struct {
		msg   string
		code  int
		state State
		text  string
	}{
		{`Fixed it.\\nSOUS: done the spec passes 20 times`, 0, Done, "the spec passes 20 times"},
		{`Stuck.\\nSOUS: needs you which fixture should win?`, 0, NeedsYou, "which fixture should win?"},
		{`All good, no marker`, 0, Done, "All good, no marker"},
		{`crashed`, 1, Failed, "exit 1: crashed"},
	} {
		a, ref, dir := startedRun(t, "claude", time.Minute)
		claudeSays(t, c.msg, c.code)
		if err := a.Watch(dir, false); err != nil {
			t.Fatal(err)
		}
		st, err := a.Status("", ref)
		if err != nil || st.State != c.state || !strings.Contains(st.Text, c.text) || st.Branch != "sous/run-3" || st.Log == "" {
			t.Errorf("%q: %+v %v", c.msg, st, err)
		}
	}
}

// With no last message, a failure shows the log's last line.
func TestFailureWithoutAMessageShowsTheLog(t *testing.T) {
	a, ref, dir := startedRun(t, "claude", time.Minute)
	testutil.FakeBin(t, "claude", `echo "API error: overloaded" >&2; exit 1`)
	a.Watch(dir, false)
	if st, _ := a.Status("", ref); st.State != Failed || st.Text != "exit 1: API error: overloaded" {
		t.Fatalf("%+v", st)
	}
}

// holdLock plays a live watcher: it holds the run's lock until released.
func holdLock(t *testing.T, dir string) func() {
	t.Helper()
	f, err := lockRun(dir)
	if err != nil {
		t.Fatal(err)
	}
	return func() { f.Close() }
}

func TestStatusRunning(t *testing.T) {
	a, ref, dir := startedRun(t, "claude", time.Minute)
	release := holdLock(t, dir)
	defer release()
	if st, _ := a.Status("", ref); st.State != Running {
		t.Fatalf("%+v", st)
	}
}

// Right after start, before the watcher has written anything,
// the run is running, not "stopped without a result".
func TestStatusRightAfterStart(t *testing.T) {
	testutil.FakeBin(t, "claude", "sleep 30")
	a, ref := watchedRun(t, "claude")
	if st, _ := a.Status("", ref); st.State != Running {
		t.Fatalf("%+v", st)
	}
	a.Stop("", ref)
}

// No result, watcher gone, is failed, never running forever,
// even when its old pid now belongs to some other live process.
func TestStatusWatcherGone(t *testing.T) {
	a, ref, dir := startedRun(t, "claude", time.Minute)
	os.WriteFile(filepath.Join(dir, "watcher.pid"), []byte(fmt.Sprint(os.Getpid())), 0o600) // a live process, not the watcher
	if st, _ := a.Status("", ref); st.State != Failed || !strings.Contains(st.Text, "stopped without a result") {
		t.Fatalf("%+v", st)
	}
	os.Remove(filepath.Join(dir, "watcher.pid")) // never even started
	if st, _ := a.Status("", ref); st.State != Failed {
		t.Fatalf("no pid: %+v", st)
	}
	if _, err := a.Status("", "claude:nope"); err == nil {
		t.Fatal("an unknown run is an error, not a state")
	}
}

func TestWatchTimeLimit(t *testing.T) {
	a, ref, dir := startedRun(t, "claude", 300*time.Millisecond)
	testutil.FakeBin(t, "claude", "sleep 30")
	start := time.Now()
	a.Watch(dir, false)
	if time.Since(start) > 5*time.Second {
		t.Fatal("the limit did not stop the agent")
	}
	if st, _ := a.Status("", ref); st.State != Failed || !strings.Contains(st.Text, "ran out of time") {
		t.Fatalf("%+v", st)
	}
}

// codexWrites fakes codex exec --json -o <file>: an event with the thread
// id, and the last message written where -o says.
const codexWrites = `out=""; while [ $# -gt 0 ]; do [ "$1" = "-o" ] && out="$2"; shift; done
echo '{"type":"thread.started","thread_id":"th-9"}'
printf 'Done.\nSOUS: done fixed\n' > "$out"`

func TestCodexSessionAndLastMessage(t *testing.T) {
	a, ref, dir := startedRun(t, "codex", time.Minute)
	testutil.FakeBin(t, "codex", codexWrites)
	a.Watch(dir, false)
	var m runMeta
	readJSON(dir, "run.json", &m)
	if st, _ := a.Status("", ref); st.State != Done || st.Text != "fixed" || m.Session != "th-9" {
		t.Fatalf("%+v session %q", st, m.Session)
	}
	if _, err := os.Stat(filepath.Join(m.Worktree, "last.txt")); err == nil {
		t.Fatal("last.txt must not land in the worktree")
	}
}

// The agent's environment blocks pushes; its folder is the worktree.
func TestWatchRunsInTheWorktreeWithPushBlocked(t *testing.T) {
	a, _, dir := startedRun(t, "claude", time.Minute)
	testutil.FakeBin(t, "claude", `pwd > where; env | grep -c GIT_CONFIG_KEY_ > keys; printf '{"result":"SOUS: done x","session_id":"s"}\n'`)
	a.Watch(dir, false)
	wt := filepath.Join(dir, "worktree")
	if where := readString(wt, "where"); !strings.HasSuffix(where, "/runs/u3/worktree") {
		t.Fatalf("ran in %q", where)
	}
	if readString(wt, "keys") == "0" {
		t.Fatal("push block not set")
	}
	if st, _ := os.Stat(filepath.Join(dir, "log")); st.Mode().Perm() != 0o600 {
		t.Fatal("log must be private")
	}
}
