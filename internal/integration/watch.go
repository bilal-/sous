package integration

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/signal"
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
	Meta             *Meta     `json:"meta,omitempty"` // version 1: board fields that changed
	Error            string    `json:"error,omitempty"`
	Exit             int       `json:"exit,omitempty"`
}

// Watch sends a baseline, changes, and availability transitions. It is a
// projection of current state, not a journal: a host reconnects by accepting
// another baseline. The host owns cancellation and notification policy.
func Watch(ctx context.Context, v int, read func(context.Context) (Snapshot, error), interval time.Duration, emit func(Event) error) error {
	return Stream{V: v, Interval: interval}.Run(ctx, read, emit)
}

// Stream is one watch: the protocol version it speaks, and from version 1
// the most bytes one line may take (0: no limit).
type Stream struct {
	V        int
	MaxBytes int
	Interval time.Duration
}

// Run reads the board once per Interval and sends a baseline, then changes
// and availability transitions. From version 1, changes carry only the
// changed items and the board fields that changed (Meta), and no line is
// longer than MaxBytes.
func (st Stream) Run(ctx context.Context, read func(context.Context) (Snapshot, error), emit func(Event) error) error {
	interval, v := st.Interval, st.V
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
		e := Event{Kind: "event", V: v, Provider: Provider, Changes: []Change{}}
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
			if v >= Version1 {
				e.Snapshot, e.Meta = nil, meta(*previous, s)
				if st.MaxBytes > 0 && size(e) > st.MaxBytes {
					e = Event{Kind: "event", V: v, Provider: Provider, Type: "snapshot", Revision: s.Revision, Snapshot: &s, Changes: []Change{}}
				}
			}
			previous, send = &s, true
		}
		if send && e.Type == "snapshot" && st.MaxBytes > 0 {
			trimmed := trim(*e.Snapshot, st.MaxBytes-(size(e)-size(*e.Snapshot)))
			e.Snapshot = &trimmed
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

// Meta is what changed of the board itself between two snapshots, each
// field replacing the one the host holds. Absent fields did not change.
type Meta struct {
	Configured  *bool                  `json:"configured,omitempty"`
	Available   *bool                  `json:"available,omitempty"`
	Complete    *bool                  `json:"complete,omitempty"`
	AsOf        *time.Time             `json:"as_of,omitempty"`
	Projects    *[]project.Project     `json:"projects,omitempty"`
	Sources     *[]signal.PluginStatus `json:"sources,omitempty"`
	Problems    *[]string              `json:"problems,omitempty"`
	Checked     *int                   `json:"checked,omitempty"`
	Unavailable *int                   `json:"unavailable,omitempty"`
}

// meta is the board fields that differ between a and b; nil when none do.
func meta(a, b Snapshot) *Meta {
	var m Meta
	changed := false
	set := func(differ bool, apply func()) {
		if differ {
			apply()
			changed = true
		}
	}
	set(a.Configured != b.Configured, func() { m.Configured = &b.Configured })
	set(a.Available != b.Available, func() { m.Available = &b.Available })
	set(a.Complete != b.Complete, func() { m.Complete = &b.Complete })
	set(!reflect.DeepEqual(a.AsOf, b.AsOf), func() { m.AsOf = b.AsOf })
	set(!reflect.DeepEqual(a.Projects, b.Projects), func() { m.Projects = &b.Projects })
	set(!reflect.DeepEqual(a.Sources, b.Sources), func() { m.Sources = &b.Sources })
	set(!reflect.DeepEqual(a.Problems, b.Problems), func() { m.Problems = &b.Problems })
	set(a.Checked != b.Checked, func() { m.Checked = &b.Checked })
	set(a.Unavailable != b.Unavailable, func() { m.Unavailable = &b.Unavailable })
	if !changed {
		return nil
	}
	return &m
}

// size is v's length as one JSON line.
func size(v any) int {
	b, _ := json.Marshal(v)
	return len(b)
}

// dropOrder is the sections a trimmed snapshot leaves out first.
var dropOrder = []string{"snoozed", "ideas", "unfinished", "on_others", "on_you"}

// trim leaves items out of s until it takes at most max bytes: the lowest
// sections first, the oldest in each first, counting them in Omitted. Its
// revision stays the whole board's, so changes still line up.
func trim(s Snapshot, max int) Snapshot {
	if size(s) <= max {
		return s
	}
	keep := append([]Item{}, s.Items...)
	s.Omitted = map[string]int{}
	for _, section := range dropOrder {
		for size(s) > max {
			i := oldest(keep, section)
			if i < 0 {
				break
			}
			keep = append(keep[:i], keep[i+1:]...)
			s.Items, s.Omitted[section] = keep, s.Omitted[section]+1
		}
	}
	if size(s) > max && len(s.Projects) > 0 {
		s.Omitted["projects"], s.Projects = len(s.Projects), []project.Project{}
	}
	if len(s.Omitted) == 0 {
		s.Omitted = nil
	}
	return s
}

// oldest is the index of section's oldest item in items, -1 when none.
func oldest(items []Item, section string) int {
	at := -1
	for i, it := range items {
		if it.Section == section && (at < 0 || it.Since.Before(items[at].Since)) {
			at = i
		}
	}
	return at
}
