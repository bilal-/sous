package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/bilal-/sous/internal/store"
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
