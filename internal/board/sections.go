package board

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/signal"
	"github.com/bilal-/sous/internal/thread"
)

// Row is one line of the board, whatever produced it: a thread or a signal.
// Renderers only lay rows out; the classification happens once, here.
type Row struct {
	ID       string     `json:"id"`      // thread id as digits, or "s:…"
	Project  string     `json:"project"` // absolute path
	Text     string     `json:"text"`
	Age      string     `json:"-"`     // display only: Since against RenderedAt, "(stale)" appended; --json readers use since/stale
	Since    time.Time  `json:"since"` // when the thing began waiting (thread since / signal first_seen)
	ClosedAt *time.Time `json:"closed_at,omitempty"`
	ClosedBy string     `json:"closed_by,omitempty"` // "" (you) | "upstream"
	Kind     string     `json:"kind"`                // me | them | unfinished | idea
	Source   string     `json:"source,omitempty"`    // "human" | "agent" | plugin name
	Ref      *string    `json:"ref,omitempty"`
	// Upstream state from reconciliation: "" | open | closed | unknown | error.
	Upstream    string `json:"upstream,omitempty"`
	UpstreamErr string `json:"upstream_err,omitempty"`
	Snoozed     bool   `json:"snoozed,omitempty"`
	Stale       bool   `json:"stale,omitempty"`
}

// Sections is the board classified: what is on you, on others, and merely
// unfinished. Ideas never appear here (spec: they show on landing only).
type Sections struct {
	Me, Them, Unfinished []Row
	// Ideas and RecentlyClosed are filled only for a project view (here).
	Ideas, RecentlyClosed []Row
	// Why is non-empty when the picture is incomplete: failed plugins,
	// unavailable roots, nothing checked, stale rows. The headline must
	// carry a "?" whenever it is.
	Why []string
}

func (s Sections) Total() int { return len(s.Me) + len(s.Them) + len(s.Unfinished) }

// view is what differs between the board and a project's resume view; the
// routing itself is shared, so the two can never disagree about a kind.
type view struct {
	ideas           bool // ideas are listed (here) or left for landing (board)
	snoozedPromises bool // snoozed me/them rows still shown (here: this is where they belong)
}

var (
	boardView = view{}
	hereView  = view{ideas: true, snoozedPromises: true}
)

// classify routes threads and signals into sections. Snoozed unfinished
// rows are always hidden; unfinished sorts oldest-first (age is the only
// priority). Why gets failed plugins, then extra, then stale rows.
func classify(v view, now time.Time, threads []thread.View, signals []signal.Observed, plugins []signal.PluginStatus, extra ...string) Sections {
	var s Sections
	for _, t := range threads {
		switch t.Kind {
		case thread.Me:
			s.Me = append(s.Me, ThreadRow(t, now))
		case thread.Them:
			s.Them = append(s.Them, ThreadRow(t, now))
		default:
			if v.ideas {
				s.Ideas = append(s.Ideas, ThreadRow(t, now))
			}
		}
	}
	var unf []signal.Observed
	stale := false
	for _, o := range signals {
		if o.Snoozed && (o.Kind == signal.Unfinished || !v.snoozedPromises) {
			continue
		}
		stale = stale || o.Stale
		switch o.Kind {
		case signal.Me:
			s.Me = append(s.Me, signalRow(o, now))
		case signal.Them:
			s.Them = append(s.Them, signalRow(o, now))
		case signal.Unfinished:
			unf = append(unf, o)
		}
	}
	sort.SliceStable(unf, func(i, j int) bool { return unf[i].FirstSeen.Before(unf[j].FirstSeen) })
	for _, o := range unf {
		s.Unfinished = append(s.Unfinished, signalRow(o, now))
	}
	for _, p := range plugins {
		if p.Status != "ok" {
			s.Why = append(s.Why, p.Name+" "+p.Status)
		}
	}
	s.Why = append(s.Why, extra...)
	if stale {
		s.Why = append(s.Why, "stale rows")
	}
	return s
}

// Classify turns board data into sections. Ideas stay off the board.
func Classify(d *Data) Sections {
	var extra []string
	if d.Unavailable > 0 {
		extra = append(extra, fmt.Sprintf("%d root unavailable", d.Unavailable))
	}
	if d.Checked == 0 {
		extra = append(extra, "nothing checked")
	}
	return classify(boardView, d.RenderedAt, d.Threads, d.Signals, d.Plugins, extra...)
}

// Ages are computed here, against the data's rendered-at time, so the
// data model carries only timestamps (and --json stays cacheable).
func ThreadRow(t thread.View, now time.Time) Row {
	return Row{ID: fmt.Sprint(t.ID), Project: t.Project, Text: t.Text, Age: project.Age(now, t.Since), Since: t.Since, Kind: string(t.Kind), Source: t.Source,
		Ref: t.Ref, Upstream: t.Upstream, UpstreamErr: t.UpstreamErr, Snoozed: t.Snoozed, ClosedAt: t.Closed, ClosedBy: t.ClosedBy}
}

// closedRow: for recently-closed threads the interesting age is since the
// close, not since the note.
func ClosedRow(t thread.View, now time.Time) Row {
	r := ThreadRow(t, now)
	if t.Closed != nil {
		r.Age = project.Age(now, *t.Closed)
	}
	return r
}

func signalRow(o signal.Observed, now time.Time) Row {
	age := project.Age(now, o.FirstSeen)
	if o.Stale {
		age += " (stale)" // in the age column so text truncation can't eat it
	}
	return Row{ID: o.ID, Project: o.Project, Text: o.Text, Age: age, Since: o.FirstSeen, Kind: string(o.Kind), Source: o.Plugin, Ref: o.Ref, Snoozed: o.Snoozed, Stale: o.Stale}
}

// ClassifyHere is Classify for one project: ideas and snoozed obligations
// are shown (this is where they belong), obligations come from the same
// observed signals the board uses, and recently upstream-closed threads
// appear so the person sees it happened.
func ClassifyHere(h *HereData) Sections {
	s := classify(hereView, h.RenderedAt, h.Threads, h.Signals, h.Plugins)
	for _, t := range h.RecentlyClosed {
		s.RecentlyClosed = append(s.RecentlyClosed, ClosedRow(t, h.RenderedAt))
	}
	return s
}

// PluginFailures: the failed-plugin part of Why, with error detail, for footers.
func PluginFailures(d *Data) string {
	var b strings.Builder
	for _, p := range d.Plugins {
		if p.Status == "ok" {
			continue
		}
		fmt.Fprintf(&b, " · %s: %s", p.Name, p.Status)
		if p.Error != nil {
			fmt.Fprintf(&b, " (%s)", Ellipsize(*p.Error, 40))
		}
	}
	return b.String()
}
