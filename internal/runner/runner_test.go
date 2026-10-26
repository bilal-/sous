package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bilal-/sous/internal/testutil"
	"github.com/bilal-/sous/internal/thread"
)

// fakeRunner writes a sous-runner-fake whose calls are shell case arms.
func fakeRunner(t *testing.T, body string) Runner {
	t.Helper()
	// runnertest.Fake, which this package cannot import (it imports runner).
	p := testutil.Script(t, t.TempDir(), "sous-runner-fake", "case \"$1\" in\n"+body+"\nesac")
	return Registry.Discover("", nil, []string{p})[0]
}

func TestClientReadsTheContract(t *testing.T) {
	ctx := context.Background()
	r := fakeRunner(t, `start) cat > "$(dirname "$0")/req"; echo fake:1;;
status) echo '{"v":0,"state":"needs_you","text":"which fixture?"}';;
reply) cat > "$(dirname "$0")/answer";;
stop) exit 0;;
clean) exit 2;;`)
	ref, err := Start(ctx, r, Request{ID: 7, UID: "0123456789ab", Project: "/code/acme/billing", Brief: "fix it"})
	if err != nil || ref != "fake:1" {
		t.Fatalf("start: %q %v", ref, err)
	}
	req, _ := os.ReadFile(filepath.Join(filepath.Dir(r.Argv[0]), "req"))
	if !strings.Contains(string(req), `"v":0`) || !strings.Contains(string(req), `"id":7`) || !strings.Contains(string(req), `"brief":"fix it"`) {
		t.Fatalf("request: %s", req)
	}
	st, err := GetStatus(ctx, r, "/code/acme/billing", ref)
	if err != nil || st.State != NeedsYou || st.Text != "which fixture?" {
		t.Fatalf("status: %+v %v", st, err)
	}
	if err := Reply(ctx, r, "/code/acme/billing", ref, "use main's"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(filepath.Dir(r.Argv[0]), "answer")); string(b) != "use main's" {
		t.Fatalf("answer: %q", b)
	}
	if err := Stop(ctx, r, "/code/acme/billing", ref); err != nil {
		t.Fatal(err)
	}
	if err := Clean(ctx, r, "/code/acme/billing", ref); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("clean unsupported: %v", err)
	}
}

// Never guess: junk, an unknown state, a newer v, or exit 1 are errors.
func TestStatusNeverGuesses(t *testing.T) {
	for name, body := range map[string]string{
		"junk":    `status) echo nope;;`,
		"unknown": `status) echo '{"v":0,"state":"sleeping"}';;`,
		"newer":   `status) echo '{"v":1,"state":"done"}';;`,
		"failed":  `status) echo "cannot reach it" >&2; exit 1;;`,
	} {
		r := fakeRunner(t, body)
		if st, err := GetStatus(context.Background(), r, "/p", "fake:1"); err == nil {
			t.Errorf("%s: %+v", name, st)
		}
	}
}

func TestStartNotSetUp(t *testing.T) {
	r := fakeRunner(t, `start) echo "fake is not installed" >&2; exit 3;;`)
	if _, err := Start(context.Background(), r, Request{ID: 1, UID: "u", Project: "/p", Brief: "b"}); !errors.Is(err, ErrNotSetUp) || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("%v", err)
	}
}

func TestByRef(t *testing.T) {
	rs := []Runner{{Name: "claude"}, {Name: "orchid"}}
	if r, err := ByRef(rs, "orchid:T3"); err != nil || r.Name != "orchid" {
		t.Fatal(r, err)
	}
	if _, err := ByRef(rs, "gone:1"); err == nil {
		t.Fatal("unknown runner")
	}
}

// Stop is required: exit 2 is not "stopped", or sous would close the note
// while the run goes on.
func TestStopExit2IsAnError(t *testing.T) {
	r := fakeRunner(t, `stop) echo "cannot stop yet" >&2; exit 2;;`)
	if err := Stop(context.Background(), r, "/p", "fake:1"); err == nil || !strings.Contains(err.Error(), "cannot stop yet") {
		t.Fatalf("want an error with the reason, got %v", err)
	}
}

// A runner speaks the newer calls and fields only when describe says so.
// Anything but exit 0 with a readable line is a runner that predates it,
// including a shell case with no describe arm, which exits 0 silently.
func TestDescribe(t *testing.T) {
	ctx := context.Background()
	r := fakeRunner(t, `describe) echo '{"v":0,"calls":["start","status","stop","reply","approve"],"fields":["phase","question","result"]}';;`)
	c := Describe(ctx, r)
	if !c.Described || !c.Has("approve") || c.Has("discard") || !c.Speaks("question") || c.Speaks("progress") {
		t.Fatalf("%+v", c)
	}
	for name, body := range map[string]string{
		"no arm":        `start) echo fake:1;;`,
		"refused":       `describe) exit 2;;`,
		"junk":          `describe) echo nope;;`,
		"newer":         `describe) echo '{"v":1,"calls":["start","status","stop"]}';;`,
		"missing calls": `describe) echo '{"v":0,"calls":["status"]}';;`,
	} {
		c := Describe(ctx, fakeRunner(t, body))
		if c.Described || c.Has("approve") || c.Speaks("phase") || !c.Has("start") || !c.Has("reply") {
			t.Errorf("%s: %+v", name, c)
		}
	}
}

// A status line may carry a phase and the rest beside its state. A known
// phase must agree with the state; an unknown one is kept as reported.
func TestStatusPhases(t *testing.T) {
	ctx := context.Background()
	r := fakeRunner(t, `status) echo '{"v":0,"state":"needs_you","phase":"needs_you","text":"which fixture?","progress":{"done":3,"total":7,"step":"reviewing T4"},"question":{"id":"q1","token":"t9","text":"which fixture?","choices":["keep","new"],"other":true}}';;`)
	st, err := GetStatus(ctx, r, "/p", "fake:1")
	if err != nil || st.Phase != thread.PhaseNeedsYou || st.Progress.Done != 3 || st.Question.Token != "t9" || !st.Question.Other {
		t.Fatalf("%+v %v", st, err)
	}
	r = fakeRunner(t, `status) echo '{"v":0,"state":"running","phase":"ready"}';;`)
	if st, err := GetStatus(ctx, r, "/p", "fake:1"); err == nil {
		t.Fatalf("ready beside running: %+v", st)
	}
	r = fakeRunner(t, `status) echo '{"v":0,"state":"running","phase":"thinking"}';;`)
	if st, err := GetStatus(ctx, r, "/p", "fake:1"); err != nil || st.Phase != "thinking" {
		t.Fatalf("unknown phase: %+v %v", st, err)
	}
	r = fakeRunner(t, `status) echo '{"v":0,"state":"done","phase":"ready","result":{"base":"b","commit":"c"}}';;`)
	if st, err := GetStatus(ctx, r, "/p", "fake:1"); err != nil || st.Result.Commit != "c" {
		t.Fatalf("result: %+v %v", st, err)
	}
	r = fakeRunner(t, `status) echo '{"v":0,"state":"done","phase":"ready","result":{"base":"b"}}';;`)
	if st, err := GetStatus(ctx, r, "/p", "fake:1"); err == nil {
		t.Fatalf("a result without its commit: %+v", st)
	}
}

// Exit 4 from reply: the question moved on. It is not a failure to retry.
func TestReplyConflict(t *testing.T) {
	r := fakeRunner(t, `reply) echo "question q1 was replaced" >&2; exit 4;;`)
	if err := Reply(context.Background(), r, "/p", "fake:1", "keep"); !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "replaced") {
		t.Fatalf("%v", err)
	}
}
