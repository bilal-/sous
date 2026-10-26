package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/testutil"
	"github.com/bilal-/sous/internal/thread"
)

type outcome struct {
	Kind    string `json:"kind"`
	V       int    `json:"v"`
	Op      string `json:"op"`
	Action  string `json:"action"`
	ID      string `json:"id"`
	Outcome string `json:"outcome"`
	Reason  string `json:"reason"`
}

func (f *fx) act(stdin string, args ...string) (outcome, int) {
	f.t.Helper()
	out, errs, code := f.runStdin(stdin, append([]string{"integration", "--protocol", "1", "--json"}, args...)...)
	var o outcome
	if err := json.Unmarshal([]byte(out), &o); err != nil {
		f.t.Fatalf("%v: %d %q %q", args, code, out, errs)
	}
	return o, code
}

// A host starts a run with an op it made: only in a project that allows
// remote runs, once however often it asks, and the same op with another
// brief is refused.
func TestIntegrationRunAction(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/api", true)
	starts := filepath.Join(f.Home, "starts")
	ru := f.plugin("sous-runner-fake", `case "$1" in
start) cat > /dev/null; echo x >> "`+starts+`"; echo fake:1;;
status) echo '{"v":0,"state":"running"}';;
stop) ;;
esac`)
	f.writeConfig("roots = [\"" + f.WS + "\"]\nplugins = [\"" + ru + "\"]\nrunner = \"fake\"\n")
	op := "op-0123456789abcdef"
	if o, code := f.act("fix the flaky test", "run", "--op", op, "--", p); code != 2 || o.Outcome != "refused" || o.Reason != "not_allowed_project" {
		t.Fatalf("not allowed: %+v %d", o, code)
	}
	f.writeConfig("roots = [\"" + f.WS + "\"]\nplugins = [\"" + ru + "\"]\nrunner = \"fake\"\n[projects.\"acme/api\"]\nremote_runs = \"true\"\n")
	o, code := f.act("fix the flaky test", "run", "--op", op, "--", p)
	if code != 0 || o.Kind != "outcome" || o.V != 1 || o.Outcome != "done" || o.ID == "" || o.Op != op || o.Action != "run" {
		t.Fatalf("run: %+v %d", o, code)
	}
	again, code := f.act("fix the flaky test", "run", "--op", op, "--", p)
	if code != 0 || again.Outcome != "already_done" || again.ID != o.ID {
		t.Fatalf("again: %+v %d", again, code)
	}
	if b, _ := os.ReadFile(starts); strings.Count(string(b), "x") != 1 {
		t.Fatalf("started %d times", strings.Count(string(b), "x"))
	}
	if o, code := f.act("something else", "run", "--op", op, "--", p); code != 2 || o.Reason != "op_reused" {
		t.Fatalf("reused: %+v %d", o, code)
	}
	if o, code := f.act(" ", "run", "--op", "op-emptybrief0000001", "--", p); code != 2 || o.Reason != "bad_input" {
		t.Fatalf("empty brief: %+v %d", o, code)
	}
	if _, errs, code := f.runStdin("x", "integration", "run", "--op", op, "--", p); code != 2 || !strings.Contains(errs, "--protocol 1") {
		t.Fatalf("v0 has no run: %d %q", code, errs)
	}
	if _, _, code := f.runStdin("x", "integration", "--protocol", "1", "run", "--", p); code != 2 {
		t.Fatalf("run needs --op: %d", code)
	}
	// stop: once, and the run reads as stopped.
	so, code := f.act("", "stop", "--op", "op-stop000000000001", "--", o.ID)
	th, _ := thread.Get(&store.Store{Home: f.SousHome}, mustAtoi(t, o.ID))
	if code != 0 || so.Outcome != "done" || th.Run.State != thread.RunFailed || th.Run.Text != "stopped" || th.Closed != nil {
		t.Fatalf("stop: %+v %d %+v", so, code, th.Run)
	}
}

// A reply names the question it answers. One that no longer matches is a
// conflict, never sent; a runner that saw the question move on says so
// with exit 4. A runner that does not report questions gets a token made
// from the question's words.
func TestIntegrationReplyAction(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/api", true)
	status := filepath.Join(f.Home, "status")
	answers := filepath.Join(f.Home, "answers")
	os.WriteFile(status, []byte(`{"v":0,"state":"needs_you","phase":"needs_you","text":"which fixture?","question":{"id":"q1","token":"t1","text":"which fixture?"}}`), 0o600)
	ru := f.plugin("sous-runner-fake", `case "$1" in
describe) echo '{"v":0,"calls":["start","status","stop","reply"],"fields":["phase","question"]}';;
start) cat > /dev/null; echo fake:1;;
status) cat "`+status+`";;
reply) cat >> "`+answers+`"; echo >> "`+answers+`";;
stop) ;;
esac`)
	f.writeConfig("roots = [\"" + f.WS + "\"]\nplugins = [\"" + ru + "\"]\nrunner = \"fake\"\n[projects.\"acme/api\"]\nremote_runs = \"true\"\n")
	run, _ := f.act("pick a fixture", "run", "--op", "op-run0000000000001", "--", p)
	if o, code := f.act("keep", "reply", "--op", "op-reply00000000001", "--question", "q1:old", "--", run.ID); code != 4 || o.Outcome != "conflict" || o.Reason != "stale_question" {
		t.Fatalf("stale: %+v %d", o, code)
	}
	if b, _ := os.ReadFile(answers); len(b) != 0 {
		t.Fatalf("a stale answer reached the runner: %s", b)
	}
	o, code := f.act("keep old", "reply", "--op", "op-reply00000000002", "--question", "q1:t1", "--", run.ID)
	b, _ := os.ReadFile(answers)
	var sent struct {
		Op       string `json:"op"`
		Answer   string `json:"answer"`
		Question struct{ ID, Token string }
	}
	json.Unmarshal(b, &sent)
	if code != 0 || o.Outcome != "done" || sent.Answer != "keep old" || sent.Question.Token != "t1" || sent.Op != "op-reply00000000002" {
		t.Fatalf("reply: %+v %d %s", o, code, b)
	}
	// A runner that predates questions: the token is made from the text.
	os.WriteFile(status, []byte(`{"v":0,"state":"needs_you","text":"which branch?"}`), 0o600)
	plain := f.plugin("sous-runner-plain", `case "$1" in
start) cat > /dev/null; echo plain:1;;
status) cat "`+status+`";;
reply) cat >> "`+answers+`.plain";;
stop) ;;
esac`)
	f.writeConfig("roots = [\"" + f.WS + "\"]\nplugins = [\"" + plain + "\"]\nrunner = \"plain\"\n[projects.\"acme/api\"]\nremote_runs = \"true\"\n")
	run2, _ := f.act("pick a branch", "run", "--op", "op-run0000000000002", "--", p)
	f.run("show", run2.ID)
	q := thread.Run{State: thread.RunNeedsYou, Text: "which branch?"}.Asked()
	if o, code := f.act("main", "reply", "--op", "op-reply00000000003", "--question", q.ID+":"+q.Token, "--", run2.ID); code != 0 || o.Outcome != "done" {
		t.Fatalf("plain: %+v %d", o, code)
	}
	if b, _ := os.ReadFile(answers + ".plain"); string(b) != "main" {
		t.Fatalf("plain answer: %q", b)
	}
	os.WriteFile(status, []byte(`{"v":0,"state":"needs_you","text":"which tag?"}`), 0o600)
	if o, code := f.act("main", "reply", "--op", "op-reply00000000004", "--question", q.ID+":"+q.Token, "--", run2.ID); code != 4 || o.Reason != "stale_question" {
		t.Fatalf("plain stale: %+v %d", o, code)
	}
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// A host approves the exact commit it showed: sous pushes it and, with no
// tracker it knows for the remote, says the pull request is left to do,
// keeping how far it got. diff shows the result a page at a time.
func TestIntegrationApproveDiffDiscard(t *testing.T) {
	f := fixture(t)
	origin := testutil.Bare(t, filepath.Join(t.TempDir(), "origin.git"))
	p := testutil.Repo(t, filepath.Join(f.WS, "acme", "api"), true, origin)
	base := strings.TrimSpace(f.git(p, "rev-parse", "HEAD"))
	f.git(p, "checkout", "-q", "-b", "sous/run-1")
	os.WriteFile(filepath.Join(p, "fix.txt"), []byte("fixed\n"), 0o600)
	f.git(p, "add", "fix.txt")
	f.git(p, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "-m", "fix")
	commit := strings.TrimSpace(f.git(p, "rev-parse", "HEAD"))
	f.git(p, "checkout", "-q", "-")
	ru := f.plugin("sous-runner-fake", `case "$1" in
describe) echo '{"v":0,"calls":["start","status","stop","approve","discard"],"fields":["phase","result"]}';;
start) cat > /dev/null; echo fake:1;;
status) echo '{"v":0,"state":"done","phase":"ready","result":{"base":"`+base+`","commit":"`+commit+`"}}';;
approve) cat > /dev/null; echo '{"v":0,"branch":"sous/run-1","commit":"`+commit+`","base":"`+base+`"}';;
discard) ;;
stop) ;;
esac`)
	f.writeConfig("roots = [\"" + f.WS + "\"]\nplugins = [\"" + ru + "\"]\nrunner = \"fake\"\n[projects.\"acme/api\"]\nremote_runs = \"true\"\n")
	run, _ := f.act("fix it", "run", "--op", "op-run0000000000001", "--", p)
	f.run("show", run.ID)
	out, errs, code := f.run("integration", "--protocol", "1", "--json", "diff", "--page", "1", "--", run.ID)
	var d struct {
		Commit string `json:"commit"`
		Pages  int    `json:"pages"`
		Patch  string `json:"patch"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &d) != nil || d.Commit != commit || d.Pages != 1 || !strings.Contains(d.Patch, "+fixed") {
		t.Fatalf("diff: %d %q %q", code, out, errs)
	}
	if o, code := f.act("", "approve", "--op", "op-approve000000001", "--commit", base, "--", run.ID); code != 4 || o.Reason != "commit_changed" {
		t.Fatalf("stale commit: %+v %d", o, code)
	}
	o, code := f.act("", "approve", "--op", "op-approve000000002", "--commit", commit, "--", run.ID)
	if code != 1 || o.Outcome != "failed" || !strings.HasPrefix(o.Reason, "pr_failed") {
		t.Fatalf("approve: %+v %d", o, code)
	}
	if got := strings.TrimSpace(f.git(origin, "rev-parse", "refs/heads/sous/run-1")); got != commit {
		t.Fatalf("pushed %q", got)
	}
	th, _ := thread.Get(&store.Store{Home: f.SousHome}, mustAtoi(t, run.ID))
	if th.Run.Publication == nil || th.Run.Publication.Stage != thread.PubPushed {
		t.Fatalf("%+v", th.Run.Publication)
	}
	if o, code := f.act("", "discard", "--op", "op-discard000000001", "--", run.ID); code != 4 || o.Reason != "commit_changed" {
		t.Fatalf("discard after approve: %+v %d", o, code)
	}
	run2, _ := f.act("fix it again", "run", "--op", "op-run0000000000002", "--", p)
	if out, errs, code := f.run("discard", run2.ID, "--json"); code != 0 || !strings.Contains(out, `"did": "discarded"`) {
		t.Fatalf("discard: %d %q %q", code, out, errs)
	}
}
