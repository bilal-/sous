package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestGoRunShowReplyDone(t *testing.T) {
	f := fixture(t)
	f.mkrepo("acme/billing", true)
	fake := f.plugin("sous-runner-fake", `d="$(dirname "$0")"
case "$1" in
start) echo fake:1;;
status) cat "$d/state" 2>/dev/null || echo '{"v":0,"state":"running"}';;
reply) cat > "$d/answer";;
stop) echo stopped > "$d/stopped";;
clean) echo cleaned > "$d/cleaned";;
esac`)
	out, errs, code := f.runStdin("Fix the flaky test\nmore context", "go", "billing", "--run", "-", "-a", "fake", "--json")
	var started changedJSON
	if code != 0 || json.Unmarshal([]byte(out), &started) != nil || started.ID != "1" || started.Did != "started" || started.Run == nil ||
		started.Run.Runner != "fake" || started.Run.State != "running" || started.Next[0] != "sous show 1" {
		t.Fatalf("%d %s %s", code, out, errs)
	}
	out, _, _ = f.run("go", "billing", "--run", "second task", "-a", "fake")
	if !strings.Contains(out, "started run 2 in billing (fake)") || !strings.Contains(out, "sous show 2") {
		t.Fatalf("text: %s", out)
	}
	os.WriteFile(filepath.Join(filepath.Dir(fake), "state"), []byte(`{"v":0,"state":"needs_you","text":"which fixture?"}`), 0o644)
	out, _, _ = f.run("show", "1", "--json")
	if !strings.Contains(out, `"state": "needs_you"`) || !strings.Contains(out, `sous reply 1`) {
		t.Fatalf("show: %s", out)
	}
	out, _, _ = f.run("show", "1")
	if !strings.Contains(out, "needs you") || !strings.Contains(out, "which fixture?") || !strings.Contains(out, `sous reply 1 "<answer>"`) {
		t.Fatalf("show text: %s", out)
	}
	if _, _, code := f.run("reply", "1", "use main's"); code != 0 {
		t.Fatal(code)
	}
	if b, _ := os.ReadFile(filepath.Join(filepath.Dir(fake), "answer")); string(b) != "use main's" {
		t.Fatalf("answer %q", b)
	}
	if _, errs, code := f.run("done", "1", "--clean"); code != 0 {
		t.Fatal(code, errs)
	}
	for _, name := range []string{"stopped", "cleaned"} {
		if _, err := os.Stat(filepath.Join(filepath.Dir(fake), name)); err != nil {
			t.Errorf("done --clean did not %s", name)
		}
	}
	if _, _, code := f.run("show", "1"); code != 0 {
		t.Fatal("a closed note can still be shown")
	}
}

func TestGoRunKeyIsIdempotentUnderRace(t *testing.T) {
	f := fixture(t)
	f.mkrepo("acme/billing", true)
	f.plugin("sous-runner-fake", `case "$1" in start) echo "fake:$$";; status) echo '{"v":0,"state":"running"}';; esac`)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); f.run("go", "billing", "--run", "fix it", "--key", "flaky", "-a", "fake") }()
	}
	wg.Wait()
	b, _ := os.ReadFile(filepath.Join(f.SousHome, "threads.json"))
	if n := strings.Count(string(b), `"key": "flaky"`); n != 1 {
		t.Fatalf("%d runs: %s", n, b)
	}
}

func TestRunMistakesSayWhy(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/billing", true)
	f.runIn(p, "note", "a plain note")
	if _, errs, code := f.run("reply", "1", "x"); code != 2 || !strings.Contains(errs, "not a run") {
		t.Fatalf("reply to a note: %d %s", code, errs)
	}
	if _, errs, code := f.run("go", "billing", "--run", "x", "-a", "nope"); code != 2 || !strings.Contains(errs, "claude") {
		t.Fatalf("unknown runner: %d %s", code, errs)
	}
	if _, _, code := f.run("go", "billing", "--run", "x", "--where"); code != 2 {
		t.Fatal("--run with --where")
	}
	if _, _, code := f.run("go", "billing", "--json"); code != 2 {
		t.Fatal("go --json without --run")
	}
	if _, _, code := f.run("done", "1", "--clean"); code != 2 {
		t.Fatal("--clean on a note without a run")
	}
	if _, _, code := f.runStdin("", "go", "billing", "--run", "-"); code != 2 {
		t.Fatal("an empty brief")
	}
}

// go --run takes the runner setting when -a is not given, and agent when
// that is unset. A runner plugin can be the default; a name sous does not
// know is refused before it is written.
func TestRunnerSetting(t *testing.T) {
	f := fixture(t)
	f.mkrepo("acme/billing", true)
	// queue is a runner and a launcher, so it can be the agent too. No
	// built in run starts here: its watcher would outlive the test.
	runnerBody := `case "$1" in start) echo queue:1;; status) echo '{"v":0,"state":"running"}';; esac`
	asRunner := f.plugin("sous-runner-queue", runnerBody)
	asLauncher := f.plugin("sous-launcher-queue", "exit 0")
	f.writeConfig("roots = [\"" + f.WS + "\"]\nplugins = [\"" + asRunner + "\", \"" + asLauncher + "\"]\n")
	if _, errs, code := f.run("config", "runner", "nope"); code != 2 || !strings.Contains(errs, "queue") {
		t.Fatalf("an unknown runner is refused, naming the ones there are: %d %q", code, errs)
	}
	if _, errs, code := f.run("config", "runner", "queue"); code != 0 {
		t.Fatal(errs)
	}
	out, errs, code := f.run("go", "billing", "--run", "fix it", "--json")
	if code != 0 || !strings.Contains(out, `"runner": "queue"`) {
		t.Fatalf("%d %s %s", code, out, errs)
	}
	f.run("config", "--unset", "runner")
	if _, errs, code := f.run("config", "agent", "queue"); code != 0 {
		t.Fatal(errs)
	}
	if out, _, _ := f.run("config"); !strings.Contains(out, "runner         queue (the agent)") {
		t.Fatalf("an unset runner says what it falls back to:\n%s", out)
	}
	out, _, _ = f.run("go", "billing", "--run", "fix it too", "--json")
	if !strings.Contains(out, `"runner": "queue"`) || !strings.Contains(out, `"id": "2"`) {
		t.Fatalf("with no runner set, agent picks it: %s", out)
	}
}

// Retrying the same brief while its runner is not set up keeps one note,
// started again each time, not a failed note per try.
func TestRunRetryWhileNotSetUp(t *testing.T) {
	f := fixture(t)
	f.mkrepo("acme/billing", true)
	f.plugin("sous-runner-off", `echo "off: install it first" >&2; exit 3`)
	for i := 0; i < 2; i++ {
		if _, _, code := f.run("go", "billing", "--run", "fix it", "-a", "off"); code != 3 {
			t.Fatalf("try %d: %d", i, code)
		}
	}
	if _, _, code := f.run("show", "2"); code != 1 {
		t.Fatal("a second try made a second note")
	}
	// Under --json the error names the run, and the next step it suggests
	// (its own key) starts the same note again, not a new one.
	out, _, _ := f.run("go", "billing", "--run", "fix it", "-a", "off", "--json")
	var got errorJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.ID != "1" || !slices.Contains(got.Next, "sous doctor") {
		t.Fatalf("%q", out)
	}
	retry := strings.Fields(got.Next[2])
	if _, _, code := f.runStdin("fix it", append(retry[1:], "-a", "off")...); code != 3 {
		t.Fatalf("%v: %d", retry, code)
	}
	if _, _, code := f.run("show", "2"); code != 1 {
		t.Fatalf("the suggested retry made a second note: %v", retry)
	}
}
