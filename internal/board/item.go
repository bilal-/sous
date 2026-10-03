package board

import (
	"path/filepath"
	"time"

	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/session"
	"github.com/bilal-/sous/internal/signal"
	"github.com/bilal-/sous/internal/thread"
)

// The --json read model. Every command that lists things waiting lists
// Items, in sections named the way the board names them; one note looks
// the same wherever it shows up. These types are what agents read, kept
// apart from what sous stores (threads.json, cache.json), so either can
// change without the other.

// Item is one thing waiting: a note, or something a signal found.
type Item struct {
	ID      string    `json:"id"` // a note's number ("7"), or a signal's id ("s:3fa4c1d2e9b0")
	Project string    `json:"project"`
	Name    string    `json:"name"` // the project folder's name
	Kind    string    `json:"kind"` // me | them | idea | unfinished: whose move it is
	Text    string    `json:"text"` // as written: the note, or what the signal said
	Since   time.Time `json:"since"`
	// Source: who wrote a note (human, agent), or which plugin found it.
	Source   string        `json:"source"`
	Ref      *string       `json:"ref"`      // where it is filed or found: "github:acme/api#14"
	Upstream *UpstreamItem `json:"upstream"` // for a filed note, what its tracker said
	Run      *RunItem      `json:"run"`      // for a run, how it is going
	Snoozed  bool          `json:"snoozed"`
	// Stale: its source failed this time; this is what it said before.
	Stale    bool       `json:"stale"`
	ClosedAt *time.Time `json:"closed_at"`
	ClosedBy *string    `json:"closed_by"` // you | upstream
}

// UpstreamItem is what a filed note's tracker said about it.
type UpstreamItem struct {
	State string `json:"state"`           // open | closed | unknown (gone) | error (could not ask)
	Error string `json:"error,omitempty"` // why it could not ask
}

// RunItem is how a run is going.
type RunItem struct {
	Runner   string          `json:"runner"`
	State    thread.RunState `json:"state"` // starting | running | needs_you | done | failed
	Text     string          `json:"text"`  // its summary, or its question for you
	Branch   string          `json:"branch"`
	Worktree string          `json:"worktree"`
	Log      string          `json:"log"`
	Key      string          `json:"key"`   // go --key, or brief:<hash> when none was given: a retry with it finds this run
	Brief    string          `json:"brief"` // the whole task, to hand over again
	// CheckedAt: when the runner was last asked; null before it answered.
	CheckedAt *time.Time `json:"checked_at"`
	// LogTail: in show --json, the last lines of a failed run's log.
	LogTail []string `json:"log_tail,omitempty"`
	// Error: its state could not be read this time; State is the last known.
	Error string `json:"error,omitempty"`
}

// Item is r as --json shows it.
func (r Row) Item() Item {
	it := Item{ID: r.ID, Project: r.Project, Name: filepath.Base(r.Project), Kind: r.Kind, Text: r.Text, Since: r.Since,
		Source: r.Source, Ref: r.Ref, Snoozed: r.Snoozed, Stale: r.Stale, ClosedAt: r.ClosedAt}
	if r.Upstream != "" {
		it.Upstream = &UpstreamItem{State: r.Upstream, Error: r.UpstreamErr}
	}
	if r.Run != nil {
		it.Run = &RunItem{Runner: r.Run.Runner, State: r.Run.State, Text: r.Run.Text, Branch: r.Run.Branch, Worktree: r.Run.Worktree,
			Log: r.Run.Log, Key: r.Run.Key, Brief: r.Run.Brief, CheckedAt: r.Run.Checked, Error: r.RunErr}
	}
	if r.ClosedAt != nil {
		by := "you"
		if r.ClosedBy != "" {
			by = r.ClosedBy
		}
		it.ClosedBy = &by
	}
	return it
}

// Items is rows as --json shows them; never nil.
func Items(rows []Row) []Item {
	out := make([]Item, len(rows))
	for i, r := range rows {
		out[i] = r.Item()
	}
	return out
}

// Waiting is the board's and here's sections.
type Waiting struct {
	OnYou      []Item `json:"on_you"`
	OnOthers   []Item `json:"on_others"`
	Unfinished []Item `json:"unfinished"`
	Ideas      []Item `json:"ideas"`
	// Snoozed: what is waiting but hidden until its snooze ends.
	Snoozed []Item `json:"snoozed"`
	// Attention: why the picture is incomplete (a source that failed, a
	// folder missing). Empty when it is whole.
	Attention []string `json:"attention"`
}

// waiting is the sections as --json lists them. One rule for every view:
// what is snoozed is under snoozed, wherever the text shows it.
func (s Sections) waiting() Waiting {
	snoozed := Items(s.Snoozed)
	awake := func(rows []Row) []Item {
		out := []Item{}
		for _, r := range rows {
			if r.Snoozed {
				snoozed = append(snoozed, r.Item())
			} else {
				out = append(out, r.Item())
			}
		}
		return out
	}
	return Waiting{OnYou: awake(s.Me), OnOthers: awake(s.Them), Unfinished: awake(s.Unfinished), Ideas: awake(s.Ideas),
		Snoozed: snoozed, Attention: append([]string{}, s.Why...)}
}

// BoardJSON is sous --json.
type BoardJSON struct {
	Configured bool `json:"configured"` // false: no project folders yet (sous setup)
	Waiting
	Projects    []project.Project     `json:"projects"`
	Plugins     []signal.PluginStatus `json:"plugins"`
	Checked     int                   `json:"checked"`
	Unavailable int                   `json:"unavailable"`
	AsOf        *time.Time            `json:"as_of"` // null before there is a board
}

// JSON is the board as --json shows it.
func (d *Data) JSON() BoardJSON {
	return BoardJSON{Configured: true, Waiting: Classify(d).waiting(), Projects: d.Projects, Plugins: d.Plugins,
		Checked: d.Checked, Unavailable: d.Unavailable, AsOf: &d.RenderedAt}
}

// HereJSON is sous here --json.
type HereJSON struct {
	Project string        `json:"project"`
	Name    string        `json:"name"`
	Remote  *string       `json:"remote"`
	Facts   project.Facts `json:"facts"`
	Session *SessionItem  `json:"session"` // the last agent session here, if one was recorded
	Waiting
	RecentlyClosed []Item                `json:"recently_closed"` // closed in their tracker this week
	Plugins        []signal.PluginStatus `json:"plugins"`
	AsOf           time.Time             `json:"as_of"`
}

// SessionItem is how the last agent session in a project ended.
type SessionItem struct {
	Agent       string    `json:"agent"`
	ID          string    `json:"id"`
	EndedAt     time.Time `json:"ended_at"`
	LastMessage *string   `json:"last_message"`
}

// JSON is the resume view as --json shows it.
func (h *HereData) JSON() HereJSON {
	s := ClassifyHere(h)
	out := HereJSON{Project: h.Project, Name: h.Name, Remote: h.Remote, Facts: h.Facts, Waiting: s.waiting(),
		RecentlyClosed: Items(s.RecentlyClosed), Plugins: h.Plugins, AsOf: h.RenderedAt}
	if h.Session != nil {
		out.Session = sessionItem(*h.Session)
	}
	return out
}

func sessionItem(s session.Session) *SessionItem {
	return &SessionItem{Agent: s.Agent, ID: s.SessionID, EndedAt: s.Ended, LastMessage: s.LastMessage}
}

// NoteJSON is sous show --json: one note in full.
type NoteJSON struct {
	Item
	UID          string     `json:"uid"`
	Remote       *string    `json:"remote"`
	SnoozedUntil *time.Time `json:"snoozed_until"`
	// Next: the commands that make sense for it now.
	Next []string `json:"next"`
}

// Note is one note as show --json gives it.
func Note(v thread.View, now time.Time, next []string) NoteJSON {
	n := NoteJSON{Item: ThreadRow(v, now).Item(), UID: v.UID, Remote: v.Remote, SnoozedUntil: v.SnoozedUntil, Next: next}
	if n.Run != nil {
		n.Run.LogTail = append([]string{}, v.LogTail...)
	}
	return n
}
