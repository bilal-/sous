package board

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/signal"
	"github.com/bilal-/sous/internal/text"
	"github.com/bilal-/sous/internal/thread"
)

// Row is one line of the board, whatever produced it: a thread or a signal.
// Renderers only lay rows out; the classification happens once, here.
// --json shows a row as an Item.
type Row struct {
	ID      string // thread id as digits, or "s:…"
	Project string // absolute path
	Text    string // as written
	Shown   string // what a person reads: Text, or for a run its state and Text
	Age     string // Since against the time of the board, "(stale)" appended
	Since   time.Time
	// ClosedAt, ClosedBy ("" for you, or "upstream"): a closed note.
	ClosedAt *time.Time
	ClosedBy string
	Kind     string // me | them | unfinished | idea: the section it belongs in
	Source   string // "human" | "agent" | plugin name
	Ref      *string
	// Upstream state from reconciliation: "" | open | closed | unknown | error.
	Upstream    string
	UpstreamErr string
	Snoozed     bool
	Stale       bool
	// Run: for a run, how it is going; RunErr why that could not be read
	// this time (the state is the last one known).
	Run    *thread.Run
	RunErr string
}

// Sections is the board classified: what is on you, on others, and merely
// unfinished. Ideas never appear here: they show where you land, in here.
type Sections struct {
	Me, Them, Unfinished []Row
	// Ideas are listed by here, and left off the board: they show where
	// you land, not every morning. RecentlyClosed is filled only for here.
	Ideas, RecentlyClosed []Row
	// Snoozed rows are hidden until the snooze ends; --json lists them.
	Snoozed []Row
	// Why is non-empty when the picture is incomplete: failed plugins,
	// unavailable roots, nothing checked, stale rows. The headline must
	// carry a "?" whenever it is.
	Why []string
}

func (s Sections) Total() int { return len(s.Me) + len(s.Them) + len(s.Unfinished) }

// view is what differs between the board and a project's resume view; the
// routing itself is shared, so the two can never disagree about a kind.
type view struct {
	snoozedPromises bool // snoozed me/them rows still shown (here: this is where they belong)
}

var (
	boardView = view{}
	hereView  = view{snoozedPromises: true}
)

// classify routes threads and signals into sections. Snoozed unfinished
// rows are always hidden; unfinished sorts oldest-first (age is the only
// priority). Why gets failed plugins, then extra, then stale rows.
func classify(v view, now time.Time, threads []thread.View, signals []signal.Observed, plugins []signal.PluginStatus, extra ...string) Sections {
	var s Sections
	if slices.ContainsFunc(threads, func(t thread.View) bool { return t.RunErr != "" }) {
		extra = append(extra, "run status unavailable")
	}
	s.routeThreads(v, now, threads)
	stale := s.routeSignals(v, now, signals)
	s.Why = why(plugins, stale, extra)
	return s
}

// routeThreads puts each note under on you, on others, or ideas. A run
// goes by its state: on others while it works, on you once it is done,
// needs you, or failed.
func (s *Sections) routeThreads(v view, now time.Time, threads []thread.View) {
	for _, t := range threads {
		r := ThreadRow(t, now)
		switch {
		case t.Snoozed && !v.snoozedPromises:
			s.Snoozed = append(s.Snoozed, r)
		case r.Kind == string(thread.Me):
			s.Me = append(s.Me, r)
		case r.Kind == string(thread.Them):
			s.Them = append(s.Them, r)
		default:
			s.Ideas = append(s.Ideas, r)
		}
	}
}

// routeSignals puts each signal under its section, hiding snoozed ones the
// view hides, and returns which plugins left stale rows. Unfinished sorts
// oldest first (age is the only priority).
func (s *Sections) routeSignals(v view, now time.Time, signals []signal.Observed) map[string]bool {
	stale := map[string]bool{}
	var unf []signal.Observed
	for _, o := range signals {
		if o.Stale { // counted even when hidden: a lost source is still named
			stale[o.Plugin] = true
		}
		if o.Snoozed && (o.Kind == signal.Unfinished || !v.snoozedPromises) {
			if o.Kind != signal.Info {
				s.Snoozed = append(s.Snoozed, signalRow(o, now))
			}
			continue
		}
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
	return stale
}

// why lists what makes the picture incomplete: failed plugins, then extra
// (missing roots, nothing checked), then stale rows. A plugin that is not
// set up is only a gap when it left stale rows behind.
func why(plugins []signal.PluginStatus, stale map[string]bool, extra []string) []string {
	var out []string
	for _, p := range plugins {
		switch {
		case p.Status == signal.StatusOff && stale[p.Name]:
			out = append(out, p.Name+" not set up")
		case p.Gap():
			out = append(out, p.Name+" "+string(p.Status))
		}
	}
	out = append(out, extra...)
	if len(stale) > 0 {
		out = append(out, "stale rows")
	}
	return out
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

// ThreadRow is a note as a row. Ages are computed here, against the
// data's rendered-at time, so the data model carries only timestamps (and
// --json stays cacheable). A run note is kind them while it works; once it
// waits on the person (done, needs you, failed) its row is kind me, and
// that is the section it goes in.
func ThreadRow(t thread.View, now time.Time) Row {
	r := Row{ID: fmt.Sprint(t.ID), Project: t.Project, Text: t.Text, Shown: t.Text, Age: text.Age(now, t.Since), Since: t.Since, Kind: string(t.Kind), Source: t.Source,
		Ref: t.Ref, Upstream: t.Upstream, UpstreamErr: t.UpstreamErr, Snoozed: t.Snoozed, ClosedAt: t.Closed, ClosedBy: t.ClosedBy}
	if t.Run != nil {
		r.Shown, r.Run, r.RunErr = runLine(t.Run, t.Text), t.Run, t.RunErr
		r.Kind = string(thread.Them)
		if !t.Run.State.Working() {
			r.Kind = string(thread.Me)
		}
	}
	return r
}

// ClosedRow: for recently-closed threads the interesting age is since the
// close, not since the note.
func ClosedRow(t thread.View, now time.Time) Row {
	r := ThreadRow(t, now)
	if t.Closed != nil {
		r.Age = text.Age(now, *t.Closed)
	}
	return r
}

func signalRow(o signal.Observed, now time.Time) Row {
	age := text.Age(now, o.FirstSeen)
	if o.Stale {
		age += " (stale)" // in the age column so text truncation can't eat it
	}
	return Row{ID: o.ID, Project: o.Project, Text: o.Text, Shown: o.Text, Age: age, Since: o.FirstSeen, Kind: string(o.Kind), Source: o.Plugin, Ref: o.Ref, Snoozed: o.Snoozed, Stale: o.Stale}
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
		if !p.Gap() {
			continue
		}
		fmt.Fprintf(&b, " · %s: %s", p.Name, p.Status)
		if p.Error != nil {
			fmt.Fprintf(&b, " (%s)", text.Ellipsize(*p.Error, 40))
		}
	}
	return b.String()
}
