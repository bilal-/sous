package cli

import (
	"encoding/json"
	"fmt"
	"github.com/bilal-/sous/internal/integration"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/signal"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/thread"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestReviewV1DoneAndSnoozeCarryOps(t *testing.T) {
	for _, action := range integration.DescribeV1("test").Actions {
		if action.ID != "done" && action.ID != "snooze" {
			continue
		}
		if !action.Op || len(action.Argv) < 3 || action.Argv[1] != "integration" {
			t.Errorf("retryable browser action bypasses op contract: %+v", action)
		}
	}
}

// Model a process dying after the target mutation but before Ops.finish:
// retain all target-store state and outer input binding, discard only its final outcome.
func reviewLoseOuterReceipt(t *testing.T, f *fx) {
	t.Helper()
	p := filepath.Join(f.SousHome, "ops.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var d map[string]json.RawMessage
	if err = json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	var ops map[string]map[string]json.RawMessage
	if err = json.Unmarshal(d["ops"], &ops); err != nil {
		t.Fatal(err)
	}
	for _, record := range ops {
		delete(record, "final")
		record["at"] = json.RawMessage(`"2000-01-01T00:00:00Z"`)
	}
	d["ops"], err = json.Marshal(ops)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(p, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestReviewSnoozeRetryKeepsOriginalNoteDeadline(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/api", true)
	if out, errs, code := f.runIn(p, "note", "review retry semantics"); code != 0 {
		t.Fatalf("note: %d %s %s", code, out, errs)
	}
	op := "op-note-snooze0001"
	if out, code := f.act("", "snooze", "--op", op, "--", "1"); code != 0 || out.Outcome != "done" {
		t.Fatalf("snooze: %+v %d", out, code)
	}
	s := &store.Store{Home: f.SousHome}
	before, err := thread.Get(s, 1)
	if err != nil || before.SnoozedUntil == nil {
		t.Fatalf("deadline: %+v %v", before, err)
	}
	reviewLoseOuterReceipt(t, f)
	if out, code := f.act("", "snooze", "--op", op, "--", "1"); code != 0 || (out.Outcome != "done" && out.Outcome != "already_done") {
		t.Fatalf("retry: %+v %d", out, code)
	}
	after, err := thread.Get(s, 1)
	if err != nil || after.SnoozedUntil == nil || !after.SnoozedUntil.Equal(*before.SnoozedUntil) {
		t.Fatalf("retry moved the original deadline: before=%v after=%v err=%v", before.SnoozedUntil, after.SnoozedUntil, err)
	}
}

func TestReviewSnoozeRetryDoesNotHideChangedSignal(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/api", true)
	s := &store.Store{Home: f.SousHome}
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	observe := func(text string) []signal.Observed {
		t.Helper()
		c := signal.Collected{Signals: []signal.Tagged{{Signal: signal.Signal{ID: "s:abc111", Project: p, Kind: signal.Unfinished, Text: text}, Plugin: "git"}}, Plugins: []signal.PluginStatus{{Name: "git", Status: signal.StatusOK}}}
		rows, err := signal.Observe(s, c, []string{p}, now)
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	observe("1 file uncommitted")
	op := "op-signal-snooze01"
	if out, code := f.act("", "snooze", "--op", op, "--", "s:abc111"); code != 0 || out.Outcome != "done" {
		t.Fatalf("snooze: %+v %d", out, code)
	}
	reviewLoseOuterReceipt(t, f)
	now = now.Add(time.Hour)
	rows := observe("2 files uncommitted")
	if len(rows) != 1 || rows[0].Snoozed {
		t.Fatalf("changed signal did not wake: %+v", rows)
	}
	if out, code := f.act("", "snooze", "--op", op, "--", "s:abc111"); code != 0 || (out.Outcome != "done" && out.Outcome != "already_done") {
		t.Fatalf("retry: %+v %d", out, code)
	}
	rows = observe("2 files uncommitted")
	if len(rows) != 1 || rows[0].Snoozed {
		t.Fatalf("old request suppressed changed signal: %+v", rows)
	}
}

func TestReviewDoneRetryRemainsPrivate(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/api", true)
	if err := os.WriteFile(filepath.Join(p, "FOLLOWUPS.md"), []byte("# Follow-ups\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if out, errs, code := f.runIn(p, "note", "--file", "review retry semantics"); code != 0 {
		t.Fatalf("filed note: %d %s %s", code, out, errs)
	}
	path := filepath.Join(p, "FOLLOWUPS.md")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	op := "op-private-done01"
	for n := 0; n < 2; n++ {
		if out, code := f.act("", "done", "--op", op, "--", "1"); code != 0 || (out.Outcome != "done" && out.Outcome != "already_done") {
			t.Fatalf("done: %+v %d", out, code)
		}
		if n == 0 {
			reviewLoseOuterReceipt(t, f)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatalf("private done changed filed note: %v before=%q after=%q", err, before, after)
	}
	note, err := thread.Get(&store.Store{Home: f.SousHome}, 1)
	if err != nil || note.Closed == nil {
		t.Fatalf("private note not closed: %+v %v", note, err)
	}
}

func TestReviewSnoozeRetryDoesNotResolvePrefixAgain(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/api", true)
	s := &store.Store{Home: f.SousHome}
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	observe := func(id string) []signal.Observed {
		t.Helper()
		c := signal.Collected{Signals: []signal.Tagged{{Signal: signal.Signal{ID: id, Project: p, Kind: signal.Unfinished, Text: "work waiting"}, Plugin: "git"}}, Plugins: []signal.PluginStatus{{Name: "git", Status: signal.StatusOK}}}
		rows, err := signal.Observe(s, c, []string{p}, now)
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	observe("s:abc111")
	op := "op-prefix-snooze01"
	if out, code := f.act("", "snooze", "--op", op, "--", "s:abc"); code != 0 || out.Outcome != "done" {
		t.Fatalf("snooze: %+v %d", out, code)
	}
	reviewLoseOuterReceipt(t, f)
	now = now.Add(time.Hour)
	rows := observe("s:abc222") // a successful scan removes the original signal
	if len(rows) != 1 || rows[0].Snoozed {
		t.Fatalf("replacement: %+v", rows)
	}
	if out, code := f.act("", "snooze", "--op", op, "--", "s:abc"); code != 0 || (out.Outcome != "done" && out.Outcome != "already_done") {
		t.Fatalf("retry: %+v %d", out, code)
	}
	rows = observe("s:abc222")
	if len(rows) != 1 || rows[0].Snoozed {
		t.Fatalf("old request retargeted a reused signal prefix: %+v", rows)
	}
}

func TestReviewDoneNeverClosesWhenStopFails(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/api", true)
	s := &store.Store{Home: f.SousHome}
	ru := f.plugin("sous-runner-fake", `case "$1" in stop) echo "owned process unavailable" >&2; exit 1;; *) exit 2;; esac`)
	f.writeConfig(fmt.Sprintf("roots = [%q]\nplugins = [%q]\nrunner = \"fake\"\n", f.WS, ru))
	id, _, err := thread.NoteRun(s, project.Project{Path: p}, "check task", "review-stop-fails", "fake", "human", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err = thread.SetRun(s, id, func(r *thread.Run) { r.Ref = "fake:1"; r.State = thread.RunRunning }); err != nil {
		t.Fatal(err)
	}
	if out, code := f.act("", "done", "--op", "op-done-stop-fail1", "--", strconv.Itoa(id)); code != 1 || out.Outcome != "failed" {
		t.Fatalf("failed stop: %+v %d", out, code)
	}
	note, err := thread.Get(s, id)
	if err != nil || note.Closed != nil {
		t.Fatalf("failed stop closed note: %+v %v", note, err)
	}
}

func TestPrivateActionsDoNotNeedConfigAndReplayBeforeRunner(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/api", true)
	s := &store.Store{Home: f.SousHome}
	id, _, err := thread.Note(s, project.Project{Path: p}, thread.Me, "inspect private result", "human", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	f.writeConfig("broken = [")
	target := strconv.Itoa(id)
	if out, code := f.act("", "snooze", "--op", "op-no-config-snooze", "--", target); code != 0 || out.Outcome != "done" {
		t.Fatal(out, code)
	}
	if out, code := f.act("", "done", "--op", "op-no-config-done", "--", target); code != 0 || out.Outcome != "done" {
		t.Fatal(out, code)
	}
	reviewLoseOuterReceipt(t, f)
	if out, code := f.act("", "done", "--op", "op-no-config-done", "--", target); code != 0 || out.Outcome != "already_done" {
		t.Fatal(out, code)
	}
	// A completed run closure must likewise recover without resolving its runner.
	id, _, err = thread.NoteRun(s, project.Project{Path: p}, "inspect run", "private-run", "missing", "human", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := thread.SetRun(s, id, func(r *thread.Run) { r.Ref = "missing:1"; r.State = thread.RunDone }); err != nil {
		t.Fatal(err)
	}
	const op = "op-closed-run-retry"
	if _, err := thread.DoneOnce(s, id, time.Now(), op); err != nil {
		t.Fatal(err)
	}
	if out, code := f.act("", "done", "--op", op, "--", strconv.Itoa(id)); code != 0 || out.Outcome != "already_done" {
		t.Fatal(out, code)
	}
}

func TestPrivateActionsRefuseInvalidSelectorsWithoutMutation(t *testing.T) {
	f := fixture(t)
	s := &store.Store{Home: f.SousHome}
	for i, target := range []string{"0", "invalid", "s:a"} {
		out, code := f.act("", "snooze", "--op", fmt.Sprintf("op-invalid-target-%d", i), "--", target)
		if code != 2 || out.Outcome != "refused" || out.Reason != "bad_input" {
			t.Fatal(target, out, code)
		}
	}
	for _, name := range []string{"threads.json", "observed.json"} {
		if _, err := os.Stat(filepath.Join(s.Home, name)); !os.IsNotExist(err) {
			t.Fatal("invalid input wrote target", name, err)
		}
	}
}
