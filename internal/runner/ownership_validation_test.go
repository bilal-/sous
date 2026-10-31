package runner

import (
	"bytes"
	"context"
	"fmt"
	"github.com/bilal-/sous/internal/testutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLegacyRunNeedsCompletionEvidence(t *testing.T) {
	for _, operation := range []string{"status", "stop", "reply", "clean", "start"} {
		t.Run(operation, func(t *testing.T) {
			a, ref, dir := startedRun(t, "claude", time.Minute)
			t.Cleanup(func() { _ = a.Stop("", ref) })
			var m runMeta
			if err := readJSON(dir, "run.json", &m); err != nil {
				t.Fatal(err)
			}
			m.OwnerVersion, m.Generation, m.Session = 0, "", "legacy-session"
			if err := writeJSON(dir, "run.json", m); err != nil {
				t.Fatal(err)
			}
			var err error
			switch operation {
			case "status":
				_, err = a.Status("", ref)
			case "stop":
				err = a.Stop("", ref)
			case "reply":
				err = a.Reply("", ref, "preserve this work")
			case "clean":
				err = a.Clean("", ref)
			case "start":
				_, err = a.Start(Request{ID: m.ID, UID: filepath.Base(dir), Project: m.Project, Brief: "same task"})
			}
			if err == nil {
				t.Fatalf("%s accepted unresolved legacy work", operation)
			}
			if _, err := os.Stat(m.Worktree); err != nil {
				t.Fatalf("lost legacy work: %v", err)
			}
		})
	}
}

func TestStopRequestRequiresValidGeneration(t *testing.T) {
	dir := t.TempDir()
	s := ownedStage{Dir: dir, Generation: strings.Repeat("a", 32)}
	if err := writeJSON(dir, "stop.json", map[string]string{}); err != nil {
		t.Fatal(err)
	}
	if _, err := receivedStop(s); err == nil {
		t.Fatal("malformed request looked like no stop request")
	}
	if err := writeJSON(dir, "stop.json", stopRequest{Generation: strings.Repeat("b", 32)}); err != nil {
		t.Fatal(err)
	}
	if stopped, err := receivedStop(s); stopped || err != nil {
		t.Fatalf("stale request: %v %v", stopped, err)
	}
}

func TestOutcomeRequiresCompleteMatchingEnvelope(t *testing.T) {
	s := ownedStage{Generation: strings.Repeat("a", 32), Token: strings.Repeat("b", 32)}
	good := `{"v":1,"final":true,"generation":"` + s.Generation + `","token":"` + s.Token + `","exit":0}`
	if _, err := decodeOutcome(strings.NewReader(good), s, true); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		strings.Replace(good, `,"exit":0`, "", 1),
		strings.Replace(good, `"exit":0`, `"exit":null`, 1),
		strings.Replace(good, `"exit":0`, `"exit":0,"stopped":true`, 1),
		strings.Replace(good, `"v":1`, `"v":2`, 1),
		strings.Replace(good, `"final":true`, `"final":false`, 1),
		strings.Replace(good, s.Token, strings.Repeat("c", 32), 1),
	} {
		if _, err := decodeOutcome(bytes.NewBufferString(bad), s, true); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestCompletedLegacyRunCanResumeWithNewOwnership(t *testing.T) {
	a, ref, dir := startedRun(t, "claude", time.Minute)
	var m runMeta
	if err := readJSON(dir, "run.json", &m); err != nil {
		t.Fatal(err)
	}
	m.OwnerVersion, m.Generation, m.Session = 0, "", "legacy-session"
	if err := writeJSON(dir, "run.json", m); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(dir, "result.json", result{Exit: 0, Message: "SOUS: needs you which fixture?", Ended: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if st, err := a.Status("", ref); err != nil || st.State != NeedsYou {
		t.Fatalf("legacy status: %+v %v", st, err)
	}
	if err := a.Reply("", ref, "use the small fixture"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return !watcherAlive(dir) })
	_, next, err := a.meta(ref)
	if err != nil || next.OwnerVersion != 1 || !validOwnerID(next.Generation) || next.Session != m.Session || next.Answer != "use the small fixture" {
		t.Fatalf("new owned attempt: %+v %v", next, err)
	}
}

func TestCheckReturnsAuthenticatedStopBeforeContextCancellation(t *testing.T) {
	a, ref, dir := startedRun(t, "claude", time.Minute)
	_, m, err := a.meta(ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.Worktree, "fix"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, m.Worktree, "add", "fix")
	testutil.Git(t, m.Worktree, "-c", "user.name=Sam", "-c", "user.email=sam@example.invalid", "commit", "-qm", "fixture")
	entered := filepath.Join(t.TempDir(), "entered")
	m.Check = fmt.Sprintf("echo entered > %q; sleep 10", entered)
	lock, err := lockRun(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	owner := *a
	owner.Exe, err = os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	type answer struct {
		outcome stageOutcome
		err     error
	}
	done := make(chan answer, 1)
	go func() { _, outcome, err := owner.runCheck(ctx, dir, m, lock); done <- answer{outcome, err} }()
	waitFor(t, func() bool { _, err := os.Stat(entered); return err == nil })
	if err := requestStop(dir, m); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.err != nil || !got.outcome.Stopped || ctx.Err() != nil {
		t.Fatalf("stop was lost without watcher polling: %+v %v; ctx=%v", got.outcome, got.err, ctx.Err())
	}
}

func TestUnknownOwnershipVersionIsNeverMutated(t *testing.T) {
	a, ref, dir := startedRun(t, "claude", time.Minute)
	var m runMeta
	if err := readJSON(dir, "run.json", &m); err != nil {
		t.Fatal(err)
	}
	m.OwnerVersion = 99
	if err := writeJSON(dir, "run.json", m); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "run.json"))
	if _, err := a.Status("", ref); err == nil {
		t.Fatal("future ownership status accepted")
	}
	if err := a.Stop("", ref); err == nil {
		t.Fatal("future ownership stopped")
	}
	if err := a.Reply("", ref, "answer"); err == nil {
		t.Fatal("future ownership resumed")
	}
	if err := a.Clean("", ref); err == nil {
		t.Fatal("future ownership cleaned")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "run.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("future ownership overwritten")
	}
}

func TestCompletedStageDoesNotBecomeUncertainWhileWatcherFinishes(t *testing.T) {
	a, ref, dir := startedRun(t, "claude", time.Minute)
	_, m, err := a.meta(ref)
	if err != nil {
		t.Fatal(err)
	}
	s := ownedStage{Version: 1, Dir: dir, Generation: m.Generation, Token: strings.Repeat("c", 32), Deadline: time.Now().Add(time.Minute), Argv: []string{"true"}}
	if err := saveIntent(s); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(dir, outcomeName(s), stageOutcome{Version: 1, Final: true, Generation: s.Generation, Token: s.Token, Exit: 0}); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(dir, "result.json", result{Exit: 0, Message: "SOUS: done checked", Ended: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	release := holdLock(t, dir)
	defer release()
	if err := a.Stop("", ref); err == nil {
		t.Fatal("Stop claimed a live watcher had finished")
	}
	release()
	if _, err := os.Stat(filepath.Join(dir, "uncertain.json")); !os.IsNotExist(err) {
		t.Fatalf("confirmed cleanup became permanently uncertain: %v", err)
	}
	if st, err := a.Status("", ref); err != nil || st.State != Done {
		t.Fatalf("confirmed result lost: %+v %v", st, err)
	}
}

func TestOldStopRequestDoesNotStopNewAttempt(t *testing.T) {
	a, ref, dir := startedRun(t, "claude", time.Minute)
	_, m, err := a.meta(ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := requestStop(dir, m); err != nil {
		t.Fatal(err)
	}
	claudeSays(t, "SOUS: done new attempt", 0)
	if err := a.Watch(dir, false); err != nil {
		t.Fatal(err)
	}
	if st, err := a.Status("", ref); err != nil || st.State != Done || st.Text != "new attempt" {
		t.Fatalf("stale stop applied: %+v %v", st, err)
	}
}

func TestInvalidOutcomeCannotBeReplacedByLateSuccess(t *testing.T) {
	a, ref, dir := startedRun(t, "claude", time.Minute)
	_, m, err := a.meta(ref)
	if err != nil {
		t.Fatal(err)
	}
	s := ownedStage{Version: 1, Dir: dir, Generation: m.Generation, Token: strings.Repeat("c", 32), Deadline: time.Now().Add(time.Minute), Argv: []string{"true"}}
	if err := saveIntent(s); err != nil {
		t.Fatal(err)
	}
	outcome := stageOutcome{Version: 99, Final: true, Generation: s.Generation, Token: s.Token, Exit: 0}
	if err := writeJSON(dir, outcomeName(s), outcome); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Status("", ref); err == nil {
		t.Fatal("future outcome accepted")
	}
	outcome.Version = 1
	if err := writeJSON(dir, outcomeName(s), outcome); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(dir, "result.json", result{Exit: 0, Message: "SOUS: done late result", Ended: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Status("", ref); err == nil {
		t.Fatal("late outcome cleared uncertainty")
	}
	if err := a.Stop("", ref); err == nil {
		t.Fatal("late outcome made stop safe")
	}
	if err := a.Clean("", ref); err == nil {
		t.Fatal("late outcome made cleanup safe")
	}
}
