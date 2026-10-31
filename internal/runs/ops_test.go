package runs

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/store"
)

// An op is done once: a repeat with the same input gets the same answer
// without doing it again, a repeat with other input is refused, and a
// failure leaves nothing behind so the same op can be tried again.
func TestOpIsDoneOnce(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	now := time.Now()
	ops := Ops{Store: s, Now: func() time.Time { return now }}
	calls := 0
	do := func() Outcome { calls++; return Outcome{Outcome: Done, ID: "7"} }
	if o := ops.Do("op-0123456789abcdef", "run", "h1", do); o.Outcome != Done || o.ID != "7" || calls != 1 {
		t.Fatalf("%+v %d", o, calls)
	}
	if o := ops.Do("op-0123456789abcdef", "run", "h1", do); o.Outcome != AlreadyDone || o.ID != "7" || calls != 1 {
		t.Fatalf("repeat: %+v %d", o, calls)
	}
	if o := ops.Do("op-0123456789abcdef", "run", "h2", do); o.Outcome != Refused || o.Reason != "op_reused" || calls != 1 {
		t.Fatalf("other input: %+v", o)
	}
	if o := ops.Do("op-0123456789abcdef", "stop", "h1", do); o.Outcome != Refused || o.Reason != "op_reused" {
		t.Fatalf("other action: %+v", o)
	}
	conflict := func() Outcome { return Outcome{Outcome: Conflict, Reason: "stale_question"} }
	ops.Do("op-conflict000000001", "reply", "h", conflict)
	if o := ops.Do("op-conflict000000001", "reply", "h", do); o.Outcome != Conflict || o.Reason != "stale_question" {
		t.Fatalf("a conflict is final: %+v", o)
	}
	refusals := 0
	notAllowed := func() Outcome { refusals++; return Outcome{Outcome: Refused, Reason: "not_allowed_project"} }
	ops.Do("op-refused0000000001", "run", "h", notAllowed)
	if o := ops.Do("op-refused0000000001", "run", "h", do); o.Outcome != Done || refusals != 1 {
		t.Fatalf("a refusal that depends on settings is not final: %+v", o)
	}
	failed := 0
	fail := func() Outcome { failed++; return Outcome{Outcome: Failed, Reason: "push_failed"} }
	ops.Do("op-failed00000000001", "approve", "h", fail)
	if o := ops.Do("op-failed00000000001", "approve", "h", fail); o.Outcome != Failed || failed != 2 {
		t.Fatalf("a failure may be tried again: %+v %d", o, failed)
	}
	for _, bad := range []string{"", "short", "has space in it here", "op-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"} {
		if o := ops.Do(bad, "run", "h", do); o.Outcome != Refused || o.Reason != "bad_input" {
			t.Errorf("%q: %+v", bad, o)
		}
	}
}

// While an op is being done, the same op again says so rather than doing
// it twice; one reserved long ago (sous stopped half way) is done again.
func TestOpInProgress(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	now := time.Now()
	ops := Ops{Store: s, Now: func() time.Time { return now }}
	release := make(chan struct{})
	started := make(chan struct{})
	var calls atomic.Int32
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ops.Do("op-slow000000000001", "run", "h", func() Outcome {
			calls.Add(1)
			close(started)
			<-release
			return Outcome{Outcome: Done, ID: "1"}
		})
	}()
	<-started
	if o := ops.Do("op-slow000000000001", "run", "h", func() Outcome { calls.Add(1); return Outcome{Outcome: Done} }); o.Outcome != InProgress {
		t.Fatalf("%+v", o)
	}
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
	// A reservation left by a sous that stopped half way.
	ops.reserve("op-stuck00000000001", "run", "h")
	ops.Now = func() time.Time { return now.Add(time.Hour) }
	if o := ops.Do("op-stuck00000000001", "run", "h", func() Outcome { return Outcome{Outcome: Done, ID: "2"} }); o.Outcome != Done {
		t.Fatalf("stale reservation: %+v", o)
	}
}

// reserve simulates a request that died after reserving its operation.
func (o Ops) reserve(op, action, hash string) { o.begin(op, action, hash) }
