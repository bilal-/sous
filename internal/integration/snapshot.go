package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/bilal-/sous/internal/board"
	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/signal"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/thread"
)

var ErrNotReady = errors.New("no structured board snapshot; run sous --refresh")

type Item struct {
	board.Item
	Key     string   `json:"key"`
	Section string   `json:"section"`
	Actions []string `json:"actions"`
}

type Snapshot struct {
	Kind        string                `json:"kind"`
	V           int                   `json:"v"`
	Provider    string                `json:"provider"`
	Revision    string                `json:"revision"`
	Configured  bool                  `json:"configured"`
	Available   bool                  `json:"available"`
	Complete    bool                  `json:"complete"`
	AsOf        *time.Time            `json:"as_of"`
	ObservedAt  time.Time             `json:"observed_at"`
	Items       []Item                `json:"items"`
	Projects    []project.Project     `json:"projects"`
	Sources     []signal.PluginStatus `json:"sources"`
	Problems    []string              `json:"problems"`
	Checked     int                   `json:"checked"`
	Unavailable int                   `json:"unavailable"`
}

// Reader overlays current notes and local runner/backend answers onto the
// last full board. Reconcile must be offline, as it is for session starts.
// No snapshot read launches a refresh or calls a signal program.
type Reader struct {
	Store      *store.Store
	Configured bool
	Now        time.Time
	Reconcile  board.Reconciler
}

func (r Reader) Read(ctx context.Context) (Snapshot, error) {
	now := r.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	c, err := board.ReadCurrentCache(r.Store, now)
	if err != nil {
		return Snapshot{}, err
	}
	d := c.Data
	available := d != nil && c.RenderedAt != nil
	if !available {
		if r.Configured {
			return Snapshot{}, ErrNotReady
		}
		views, err := thread.Open(r.Store, now)
		if err != nil {
			return Snapshot{}, err
		}
		d = &board.Data{Threads: views, RenderedAt: now}
	}
	if r.Reconcile != nil {
		d.Threads = r.Reconcile(ctx, d.Threads)
	}
	s := Snapshot{Kind: "snapshot", V: Version, Provider: Provider, Configured: r.Configured, Available: available,
		ObservedAt: now.UTC(), Items: []Item{}, Projects: append([]project.Project{}, d.Projects...), Sources: append([]signal.PluginStatus{}, d.Plugins...),
		Checked: d.Checked, Unavailable: d.Unavailable}
	if available {
		asOf := c.RenderedAt.UTC()
		s.AsOf = &asOf
	}
	sections := board.Classify(d)
	s.Problems = append([]string{}, sections.Why...)
	if !r.Configured {
		s.Problems = append(s.Problems, config.NoRootsHint)
	}
	keys := map[string]string{}
	for _, v := range d.Threads {
		keys[fmt.Sprint(v.ID)] = "n:" + v.UID
	}
	for _, section := range []struct {
		name string
		rows []board.Row
	}{
		{"on_you", sections.Me}, {"on_others", sections.Them}, {"unfinished", sections.Unfinished}, {"ideas", sections.Ideas}, {"snoozed", sections.Snoozed},
	} {
		for _, row := range section.rows {
			item := Item{Item: row.Item(), Key: row.ID, Section: section.name, Actions: []string{}}
			if !signal.IsID(row.ID) {
				item.Key = keys[row.ID]
			}
			s.Items = append(s.Items, item)
		}
	}
	// A cached checkout may have disappeared since the board was built. Keep
	// its work, make the gap explicit, and don't advertise an unusable launch.
	missing, checked := map[string]bool{}, map[string]bool{}
	check := func(path string) {
		if checked[path] {
			return
		}
		checked[path] = true
		st, err := os.Stat(path)
		if err != nil || !st.IsDir() {
			missing[path] = true
			s.Problems = append(s.Problems, "project unavailable: "+path)
		}
	}
	for _, p := range s.Projects {
		check(p.Path)
	}
	for i := range s.Items {
		item := &s.Items[i]
		if item.Upstream != nil {
			switch item.Upstream.State {
			case "error":
				s.Problems = append(s.Problems, fmt.Sprintf("filed note %s status unavailable: %s", item.ID, item.Upstream.Error))
			case "unknown":
				s.Problems = append(s.Problems, fmt.Sprintf("filed note %s ref missing", item.ID))
			}
		}
		check(item.Project)
		if missing[item.Project] {
			item.Stale = true
		} else {
			item.Actions = append(item.Actions, "open")
		}
		item.Actions = append(item.Actions, "snooze")
		if !signal.IsID(item.ID) {
			item.Actions = append(item.Actions, "show", "done")
			if item.Run != nil && item.Run.State == thread.RunNeedsYou && item.Run.Error == "" {
				item.Actions = append(item.Actions, "reply")
			}
		}
	}
	s.Complete = available && r.Configured && len(s.Problems) == 0
	if err := seal(&s); err != nil {
		return Snapshot{}, err
	}
	return s, nil
}

func stableItem(item Item) Item {
	if item.Run != nil {
		run := *item.Run
		run.CheckedAt = nil
		item.Run = &run
	}
	return item
}

// Revision tracks state changes, including source freshness and failures,
// without treating a poll timestamp as a new task or mutating the read model.
func seal(s *Snapshot) error {
	v := *s
	v.Revision, v.ObservedAt = "", time.Time{}
	v.Items = make([]Item, len(s.Items))
	for i, item := range s.Items {
		v.Items[i] = stableItem(item)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(b)
	s.Revision = hex.EncodeToString(hash[:])
	return nil
}
