package integration

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/board"
)

func TestWatchBaselineChangesAndFailureRecovery(t *testing.T) {
	a := Snapshot{Kind: "snapshot", V: 0, Provider: "sous", Revision: "a", Items: []Item{{Key: "n:old", Item: board.Item{ID: "1", Text: "check retry timeout"}}}}
	b := a
	b.Revision = "b"
	b.Items = []Item{{Key: "n:old", Item: board.Item{ID: "1", Text: "check new timeout"}}}
	c := b
	c.Revision = "c"
	c.Items = []Item{{Key: "n:new", Item: board.Item{ID: "2", Text: "review parser"}}}
	steps := []struct {
		s Snapshot
		e error
	}{{a, nil}, {a, nil}, {Snapshot{}, errors.New("state unreadable")}, {Snapshot{}, errors.New("state unreadable")}, {b, nil}, {b, nil}, {c, nil}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	index := 0
	var events []Event
	err := Watch(ctx, func(context.Context) (Snapshot, error) {
		if index == len(steps) {
			cancel()
			return Snapshot{}, context.Canceled
		}
		s := steps[index]
		index++
		return s.s, s.e
	}, time.Millisecond, func(e Event) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, e := range events {
		types = append(types, e.Type)
	}
	if !reflect.DeepEqual(types, []string{"snapshot", "unavailable", "snapshot", "changes"}) {
		t.Fatalf("baseline, suppressed duplicates, uncertainty and recovery: %+v", events)
	}
	if events[0].Snapshot == nil || len(events[0].Changes) != 0 || events[1].Revision != "a" || events[1].Error != "state unreadable" || events[2].Snapshot.Revision != "b" {
		t.Fatalf("baseline or recovery lost state: %+v", events)
	}
	if len(events[3].Changes) != 2 || events[3].PreviousRevision != "b" {
		t.Fatalf("missing delta: %+v", events[3])
	}
	if events[3].Changes[0].Type != "item.added" || events[3].Changes[0].Key != "n:new" || events[3].Changes[1].Type != "item.removed" || events[3].Changes[1].Key != "n:old" {
		t.Fatalf("changes must use stable keys; disappearance is not a closure: %+v", events[3].Changes)
	}
}

func TestWatchStopsWhenHostCannotRead(t *testing.T) {
	want := errors.New("host closed its pipe")
	reads := 0
	err := Watch(context.Background(), func(context.Context) (Snapshot, error) {
		reads++
		return Snapshot{Revision: "a"}, nil
	}, time.Millisecond, func(Event) error { return want })
	if !errors.Is(err, want) || reads != 1 {
		t.Fatalf("watch kept running after host left: %v, %d reads", err, reads)
	}
}

func TestWatchReportsMissingSnapshotAndCanStartLater(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	reads := 0
	var events []Event
	err := Watch(ctx, func(context.Context) (Snapshot, error) {
		reads++
		if reads <= 2 {
			return Snapshot{}, ErrNotReady
		}
		return Snapshot{Revision: "a"}, nil
	}, time.Millisecond, func(e Event) error {
		events = append(events, e)
		if len(events) == 2 {
			cancel()
		}
		return nil
	})
	if err != nil || len(events) != 2 || events[0].Type != "unavailable" || events[0].Exit != 3 || events[1].Type != "snapshot" {
		t.Fatalf("missing board looked empty or could not recover: %+v %v", events, err)
	}
}
