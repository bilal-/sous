package integration

import (
	"context"
	"errors"
	"reflect"
	"time"
)

type Change struct {
	Type string `json:"type"`
	Key  string `json:"key"`
	ID   string `json:"id"`
	Item *Item  `json:"item,omitempty"`
}

type Event struct {
	Kind             string    `json:"kind"`
	V                int       `json:"v"`
	Provider         string    `json:"provider"`
	Type             string    `json:"type"`
	Revision         string    `json:"revision"`
	PreviousRevision string    `json:"previous_revision,omitempty"`
	Snapshot         *Snapshot `json:"snapshot,omitempty"`
	Changes          []Change  `json:"changes"`
	Error            string    `json:"error,omitempty"`
	Exit             int       `json:"exit,omitempty"`
}

// Watch sends a baseline, changes, and availability transitions. It is a
// projection of current state, not a journal: a host reconnects by accepting
// another baseline. The host owns cancellation and notification policy.
func Watch(ctx context.Context, read func(context.Context) (Snapshot, error), interval time.Duration, emit func(Event) error) error {
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var previous *Snapshot
	lastError := ""
	for {
		if ctx.Err() != nil {
			return nil
		}
		s, err := read(ctx)
		if ctx.Err() != nil {
			return nil
		}
		e := Event{Kind: "event", V: Version, Provider: Provider, Changes: []Change{}}
		if previous != nil {
			e.Revision = previous.Revision
		}
		send := false
		switch {
		case err != nil:
			if err.Error() != lastError {
				e.Type, e.Error, e.Exit = "unavailable", err.Error(), 1
				if errors.Is(err, ErrNotReady) {
					e.Exit = 3
				}
				send = true
			}
			lastError = err.Error()
		case previous == nil || lastError != "":
			e.Type, e.Revision, e.Snapshot = "snapshot", s.Revision, &s
			lastError, previous, send = "", &s, true
		case previous.Revision != s.Revision:
			e.Type, e.PreviousRevision, e.Revision, e.Snapshot = "changes", previous.Revision, s.Revision, &s
			e.Changes = changes(*previous, s)
			previous, send = &s, true
		}
		if send {
			if err := emit(e); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func changes(previous, next Snapshot) []Change {
	before := map[string]Item{}
	for _, item := range previous.Items {
		before[item.Key] = item
	}
	after, out := map[string]bool{}, []Change{}
	for _, item := range next.Items {
		after[item.Key] = true
		old, exists := before[item.Key]
		typeName := "item.updated"
		if !exists {
			typeName = "item.added"
		} else if reflect.DeepEqual(stableItem(old), stableItem(item)) {
			continue
		}
		copy := item
		out = append(out, Change{Type: typeName, Key: item.Key, ID: item.ID, Item: &copy})
	}
	for _, item := range previous.Items {
		if !after[item.Key] {
			out = append(out, Change{Type: "item.removed", Key: item.Key, ID: item.ID})
		}
	}
	return out
}
