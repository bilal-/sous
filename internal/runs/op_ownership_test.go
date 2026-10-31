package runs

import (
	"github.com/bilal-/sous/internal/store"
	"testing"
	"time"
)

// Wall-clock reservation age is not proof the original process stopped.
// The action remains live while a second caller sees an expired reservation.
func TestReviewOpLiveOwnerSurvivesReservationExpiry(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	older := Ops{Store: s, Now: func() time.Time { return now }}
	later := Ops{Store: s, Now: func() time.Time { return now.Add(time.Hour) }}
	started, release := make(chan struct{}), make(chan struct{})
	original := make(chan Outcome, 1)
	go func() {
		original <- older.Do("op-live-owner00001", "snooze", "same", func() Outcome {
			close(started)
			<-release
			return Outcome{Outcome: Done, ID: "1"}
		})
	}()
	<-started
	defer func() { close(release); <-original }()
	repeated := make(chan Outcome, 1)
	called := make(chan struct{}, 1)
	go func() {
		repeated <- later.Do("op-live-owner00001", "snooze", "same", func() Outcome {
			called <- struct{}{}
			return Outcome{Outcome: Done, ID: "2"}
		})
	}()
	select {
	case out := <-repeated:
		if out.Outcome != InProgress {
			t.Fatalf("live expired reservation re-executed: %+v", out)
		}
	case <-time.After(time.Second):
		t.Fatal("same live op must return in_progress promptly")
	}
	select {
	case <-called:
		t.Fatal("repeated operation executed while original still owned it")
	default:
	}
}

// A former reservation must never finalize or erase its replacement.
// This is a focused state-machine proof for implementations retaining leases.
func TestReviewOpOldFinishCannotOverwriteNewReceipt(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	older := Ops{Store: s, Now: func() time.Time { return now }}
	later := Ops{Store: s, Now: func() time.Time { return now.Add(time.Hour) }}
	const op = "op-old-finish00001"
	generation, early, err := older.begin(op, "snooze", "same")
	if err != nil || early != nil {
		t.Fatalf("reserve: %+v %v", early, err)
	}
	if out := later.Do(op, "snooze", "same", func() Outcome { return Outcome{Outcome: Done, ID: "new"} }); out.Outcome != Done {
		t.Fatalf("recover: %+v", out)
	}
	if err := older.finish(op, generation, Outcome{Outcome: Failed, Reason: "old worker failed"}); err == nil {
		t.Fatal("obsolete finalizer accepted")
	}
	executed := false
	out := later.Do(op, "snooze", "same", func() Outcome { executed = true; return Outcome{Outcome: Done, ID: "third"} })
	if executed || out.Outcome != AlreadyDone || out.ID != "new" {
		t.Fatalf("old finalizer destroyed current receipt: called=%v outcome=%+v", executed, out)
	}
}
