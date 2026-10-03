package signal

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/store"
)

// ObsEntry is what sous remembers about a signal: not the signal itself
// (plugins own that) but when it was first seen and whether it's snoozed.
type ObsEntry struct {
	Plugin      string    `json:"plugin"`
	Project     string    `json:"project"`
	Kind        Kind      `json:"kind"`
	Text        string    `json:"text"`
	Ref         *string   `json:"ref"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
	Hash        string    `json:"hash"`
	SnoozedHash *string   `json:"snoozed_hash"`
	// Stale: kept only because its plugin could not check it last time.
	Stale bool `json:"stale,omitempty"`
}

type ObsDoc struct {
	Version int                 `json:"version"`
	Signals map[string]ObsEntry `json:"signals"`
}

type ObsMigrator struct{}

func (ObsMigrator) Empty() []byte { return []byte(`{"version":1,"signals":{}}`) }
func (ObsMigrator) Current() int  { return 1 }
func (ObsMigrator) Migrate(from int, raw []byte) ([]byte, error) {
	return nil, fmt.Errorf("no migration from v%d", from)
}

type Observed struct {
	Tagged
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	Snoozed   bool      `json:"snoozed"`
	Stale     bool      `json:"stale"`
}

var ErrUnknownSignal = errors.New("no such signal (run sous first)")

// PruneAfter: an observation not seen for this long is dropped, whatever
// the reason it stopped being seen (project gone, plugin removed, scope).
const PruneAfter = 30 * 24 * time.Hour

// hashOf is the content hash a snooze is tied to, distinct from ID. With no
// state it is the hash earlier versions stored. A plugin that starts
// sending a state (git's dirty row did in 0.1.1) ends its old snoozes once.
func hashOf(s Signal) string {
	if s.State == "" {
		return ID(s.Text, string(s.Kind))
	}
	return ID(s.Text+"\t"+s.State, string(s.Kind))
}

// Observe merges a collection with observed.json for the projects that were
// scanned this run: keeps first_seen for a known id (the condition began
// then, whatever its text now says), clears a snooze when the text changes,
// drops signals that vanished from an ok plugin, re-emits last-known
// signals of failed plugins as stale, and leaves untouched every
// observation for a project outside `scanned` or from a plugin that did not
// run (a scoped board, or here running only git, must not wipe or stale
// what it never looked at).
func Observe(s *store.Store, c Collected, scanned []string, now time.Time) ([]Observed, error) {
	now = now.UTC()
	okPlugins, ran := map[string]bool{}, map[string]bool{}
	for _, p := range c.Plugins {
		ran[p.Name] = true
		if p.Status == StatusOK {
			okPlugins[p.Name] = true
		}
	}
	inScope := map[string]bool{}
	for _, p := range scanned {
		inScope[p] = true
	}
	var out []Observed
	_, err := store.Modify[ObsDoc](s, "observed", ObsMigrator{}, func(d *ObsDoc) error {
		fresh := map[string]ObsEntry{}
		for _, t := range c.Signals {
			e := merge(d.Signals[t.ID], t, now)
			fresh[t.ID] = e
			out = append(out, Observed{Tagged: t, FirstSeen: e.FirstSeen, LastSeen: now,
				Snoozed: e.SnoozedHash != nil && *e.SnoozedHash == e.Hash})
		}
		for id, prev := range d.Signals {
			if _, ok := fresh[id]; ok || now.Sub(prev.LastSeen) > PruneAfter {
				continue // refreshed now, or unvouched for a month: let it go
			}
			switch {
			case !inScope[prev.Project] || !ran[prev.Plugin]:
				fresh[id] = prev // not scanned this run: keep as is, don't render
			case !okPlugins[prev.Plugin]:
				prev.Stale = true // plugin failed or is off: keep, and say so
				fresh[id] = prev
				out = append(out, prev.observed(id))
			}
		}
		d.Signals = fresh
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, err
}

// merge is a signal seen now, joined with what was known about it. Age is
// how long the thing has been waiting, not when sous noticed: first seen is
// kept, and moved earlier when the plugin knows better (a PR's updatedAt).
// A snooze holds while the content is unchanged.
func merge(prev ObsEntry, t Tagged, now time.Time) ObsEntry {
	h := hashOf(t.Signal)
	e := ObsEntry{Plugin: t.Plugin, Project: t.Project, Kind: t.Kind, Text: t.Text, Ref: t.Ref, FirstSeen: now, LastSeen: now, Hash: h}
	if !prev.FirstSeen.IsZero() {
		e.FirstSeen = prev.FirstSeen
		if prev.Hash == h {
			e.SnoozedHash = prev.SnoozedHash
		}
	}
	if !t.Observed.IsZero() && t.Observed.Before(e.FirstSeen) {
		e.FirstSeen = t.Observed.UTC()
	}
	return e
}

// observed is the entry as last seen: what a view shows when no plugin ran
// this time to vouch for it.
func (e ObsEntry) observed(id string) Observed {
	return Observed{
		Tagged:    Tagged{Signal: Signal{ID: id, Project: e.Project, Kind: e.Kind, Text: e.Text, Observed: e.LastSeen, Ref: e.Ref}, Plugin: e.Plugin},
		FirstSeen: e.FirstSeen, LastSeen: e.LastSeen,
		Snoozed: e.SnoozedHash != nil && *e.SnoozedHash == e.Hash,
		Stale:   e.Stale,
	}
}

// Known is what earlier scans observed for one project, as they last saw
// it, sorted by id. A view that runs only some plugins itself (here) reads
// the rest from here.
func Known(s *store.Store, project string) ([]Observed, error) {
	d, err := store.Load[ObsDoc](s, "observed", ObsMigrator{})
	if err != nil {
		return nil, err
	}
	var out []Observed
	for id, e := range d.Signals {
		if e.Project == project {
			out = append(out, e.observed(id))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Snooze hides a signal until its text changes.
// A unique prefix of an id is enough ("s:0a70").
func Snooze(s *store.Store, id string) error {
	_, err := store.Modify[ObsDoc](s, "observed", ObsMigrator{}, func(d *ObsDoc) error {
		if _, ok := d.Signals[id]; !ok {
			var hits []string
			for full := range d.Signals {
				if strings.HasPrefix(full, id) {
					hits = append(hits, full)
				}
			}
			sort.Strings(hits)
			switch {
			case len(id) < len(IDPrefix)+3:
				return fmt.Errorf("%s is too short; give at least 3 characters of the id after s", id)
			case len(hits) == 0:
				return fmt.Errorf("%w: %s", ErrUnknownSignal, id)
			case len(hits) > 1:
				return fmt.Errorf("%s matches %d rows: %s", id, len(hits), strings.Join(hits, ", "))
			}
			id = hits[0]
		}
		e := d.Signals[id]
		h := e.Hash
		e.SnoozedHash = &h
		d.Signals[id] = e
		return nil
	})
	return err
}
