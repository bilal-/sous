package board

import (
	"fmt"
	"os"
	"time"

	"github.com/bilal-/sous/internal/effort"
	"github.com/bilal-/sous/internal/memory"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/thread"
)

// DescribeEffort reads private notes and cached tracker observations only.
func DescribeEffort(s *store.Store, group effort.Effort, now time.Time) EffortDetail {
	return DescribeEfforts(s, []effort.Effort{group}, now)[0]
}

// DescribeEfforts reads each linked store once for this projection. The snapshot
// lasts only for this call; the next view reads current private state again.
func DescribeEfforts(s *store.Store, groups []effort.Effort, now time.Time) []EffortDetail {
	out := make([]EffortDetail, 0, len(groups))
	if len(groups) == 0 {
		return out
	}
	r := readEffortSnapshot(s, groups)
	for _, group := range groups {
		out = append(out, r.describe(group, now))
	}
	return out
}

type effortSnapshot struct {
	previous  map[string]thread.View
	asOf      *time.Time
	cacheErr  error
	notes     map[int]thread.Thread
	notesErr  error
	memories  map[string]memory.Record
	memoryErr error
}

func readEffortSnapshot(s *store.Store, groups []effort.Effort) effortSnapshot {
	r := effortSnapshot{previous: map[string]thread.View{}, notes: map[int]thread.Thread{}, memories: map[string]memory.Record{}}
	cached, err := ReadCache(s)
	r.cacheErr = err
	if err == nil && cached.Data != nil {
		r.asOf = cached.RenderedAt
		for _, view := range cached.Data.Threads {
			r.previous[view.UID] = view
		}
	}
	needNotes, needMemories := false, false
	for _, group := range groups {
		needNotes = needNotes || len(group.Notes) > 0
		needMemories = needMemories || len(group.Memories) > 0
	}
	if needNotes {
		notes, err := thread.All(s)
		r.notesErr = err
		for _, note := range notes {
			if _, found := r.notes[note.ID]; !found {
				r.notes[note.ID] = note
			}
		}
	}
	if needMemories {
		records, err := memory.All(s)
		r.memoryErr = err
		for _, record := range records {
			if _, found := r.memories[record.ID]; !found {
				r.memories[record.ID] = record
			}
		}
	}
	return r
}

func (r effortSnapshot) describe(e effort.Effort, now time.Time) EffortDetail {
	v := EffortDetail{EffortSummary: EffortSummary{ID: e.ID, Name: e.Name, Projects: e.Projects, Archived: e.ArchivedAt != nil, Attention: []string{}}, Definition: e, Items: []Item{}, Memories: []memory.Record{}, Blockers: []Blocker{}}
	v.AsOf = r.asOf
	if r.cacheErr != nil {
		v.Attention = append(v.Attention, "cached tracker state unavailable: "+r.cacheErr.Error())
	}
	notes := map[string]thread.Thread{}
	unknown := map[string]bool{}
	ids := map[string]int{}
	for _, ref := range e.Notes {
		ids[ref.UID] = ref.ID
		n, found := r.notes[ref.ID]
		if r.notesErr != nil || !found || n.UID != ref.UID {
			v.Unavailable++
			v.Attention = append(v.Attention, fmt.Sprintf("note %d unavailable", ref.ID))
			continue
		}
		notes[n.UID] = n
		view := thread.View{Thread: n, Snoozed: n.SnoozedUntil != nil && now.Before(*n.SnoozedUntil)}
		if old, ok := r.previous[n.UID]; ok && n.Ref != nil && old.Ref != nil && *n.Ref == *old.Ref {
			view.Upstream, view.UpstreamErr = old.Upstream, old.UpstreamErr
		}
		if n.Ref != nil && view.Upstream == "" && n.Closed == nil {
			view.Upstream, view.UpstreamErr = "error", "not checked; run sous --refresh"
		}
		item := ThreadRow(view, now).Item()
		if item.Upstream != nil && (item.Upstream.State == "error" || item.Upstream.State == "unknown") {
			unknown[n.UID] = true
			v.Unavailable++
			v.Attention = append(v.Attention, fmt.Sprintf("note %d tracker status unavailable", n.ID))
		}
		v.Items = append(v.Items, item)
		if n.Closed == nil {
			v.Open++
		} else {
			v.Completed++
		}
	}
	blocked := map[string]bool{}
	for _, dep := range e.Dependencies {
		n, nok := notes[dep.NoteUID]
		after, aok := notes[dep.AfterUID]
		if nok && n.Closed != nil || aok && after.Closed != nil {
			continue
		}
		v.Blockers = append(v.Blockers, Blocker{NoteID: ids[dep.NoteUID], AfterID: ids[dep.AfterUID], Unknown: !nok || !aok || unknown[dep.NoteUID] || unknown[dep.AfterUID]})
		blocked[dep.NoteUID] = true
	}
	v.Blocked = len(blocked)
	for _, ref := range e.Memories {
		record, found := r.memories[ref.ID]
		if r.memoryErr != nil || !found {
			v.Unavailable++
			v.Attention = append(v.Attention, "memory "+ref.ID+" unavailable")
			continue
		}
		v.Memories = append(v.Memories, record)
	}
	for _, p := range e.Projects {
		if st, err := os.Stat(p.Project); err != nil || !st.IsDir() {
			v.Unavailable++
			v.Attention = append(v.Attention, "project unavailable: "+p.Project)
		}
	}
	return v
}
