package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/runner"
	"github.com/bilal-/sous/internal/runs"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/thread"
)

// Reply has already passed the open-note check before Done stops and closes
// the note. The fake runner then resumes; Reply's final SetRun correctly
// refuses the closed note. A fresh Done request must still stop that runner.
// This uses the actual public Dispatcher interleaving, never a fabricated
// closed/live thread or an impossible upstream-filed run.
func TestFreshDoneStopsRunResumedByEarlierReply(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/api", true)
	s := &store.Store{Home: f.SousHome}
	fake := f.plugin("sous-runner-fake", `d="$(dirname "$0")"
case "$1" in
reply)
  : > "$d/reply-entered"
  i=0
  while [ ! -f "$d/release-reply" ]; do
    i=$((i+1))
    [ "$i" -lt 800 ] || exit 1
    sleep 0.01
  done
  : > "$d/active"
  ;;
stop)
  printf 'stop\n' >> "$d/stops"
  rm -f "$d/active"
  ;;
*) exit 2;;
esac`)
	f.writeConfig(fmt.Sprintf("roots = [%q]\nplugins = [%q]\nrunner = \"fake\"\n", f.WS, fake))
	dir := filepath.Dir(fake)
	id, _, err := thread.NoteRun(s, project.Project{Path: p}, "check the result", "reply-done-race", "fake", "human", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := thread.SetRun(s, id, func(r *thread.Run) {
		r.Ref, r.State = "fake:1", thread.RunNeedsYou
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "active"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	d := runs.Dispatcher{Store: s, Runners: []runner.Runner{{Name: "fake", Argv: []string{fake}}}, Now: time.Now}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	replied := make(chan error, 1)
	go func() { replied <- d.ReplyTo(ctx, id, "", "continue", "") }()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		if _, err := os.Stat(filepath.Join(dir, "reply-entered")); err == nil {
			break
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		select {
		case err := <-replied:
			t.Fatalf("reply ended before reaching its runner: %v", err)
		case <-deadline.C:
			t.Fatal("reply never reached its runner")
		case <-time.After(10 * time.Millisecond):
		}
	}
	target := strconv.Itoa(id)
	if out, code := f.act("", "done", "--op", "op-close-during-reply", "--", target); code != 0 || out.Outcome != "done" {
		t.Fatalf("initial done: %+v, code %d", out, code)
	}
	closed, err := thread.Get(s, id)
	if err != nil || closed.Closed == nil {
		t.Fatalf("initial closure: %+v, %v", closed, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "active")); !os.IsNotExist(err) {
		t.Fatalf("initial done did not stop the runner: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "release-reply"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-replied:
		if !errors.Is(err, thread.ErrNotFound) {
			t.Fatalf("reply must refuse its late write to the closed note: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("released reply did not finish")
	}
	if _, err := os.Stat(filepath.Join(dir, "active")); err != nil {
		t.Fatalf("runner did not resume in the established interleaving: %v", err)
	}
	const recoveryOp = "op-fresh-done-recovery"
	if out, code := f.act("", "done", "--op", recoveryOp, "--", target); code != 0 || out.Outcome != "already_done" {
		t.Fatalf("fresh done on the closed note: %+v, code %d", out, code)
	}
	stops, err := os.ReadFile(filepath.Join(dir, "stops"))
	if err != nil || string(stops) != "stop\nstop\n" {
		t.Fatalf("fresh done failed to stop the resumed runner: calls=%q, err=%v", stops, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "active")); !os.IsNotExist(err) {
		t.Fatalf("fresh done left the resumed runner active: %v", err)
	}
	after, err := thread.Get(s, id)
	if err != nil || after.Closed == nil || !after.Closed.Equal(*closed.Closed) || after.ClosedBy != closed.ClosedBy {
		t.Fatalf("fresh done changed closure history: before=%+v, after=%+v, err=%v", closed, after, err)
	}
	// Recover only from the local closure receipt, without a usable runner or
	// configuration. A matching receipt must not repeat the fresh Stop.
	reviewLoseOuterReceipt(t, f)
	if err := os.Remove(fake); err != nil {
		t.Fatal(err)
	}
	f.writeConfig("broken = [")
	if out, code := f.act("", "done", "--op", recoveryOp, "--", target); code != 0 || out.Outcome != "already_done" {
		t.Fatalf("receipt replay required the old runner/config: %+v, code %d", out, code)
	}
	stops, err = os.ReadFile(filepath.Join(dir, "stops"))
	if err != nil || string(stops) != "stop\nstop\n" {
		t.Fatalf("receipt replay repeated Stop: calls=%q, err=%v", stops, err)
	}
}
