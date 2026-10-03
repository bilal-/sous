package report

import (
	_ "embed"
	"fmt"
	"html/template"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/board"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/session"
	"github.com/bilal-/sous/internal/signal"
	"github.com/bilal-/sous/internal/text"
	"github.com/bilal-/sous/internal/thread"
	"github.com/bilal-/sous/internal/tracker"
)

// A report is the board looked at over time: what arrived, what closed,
// where work happened, and what needs attention — since a moment. Built
// from data sous already keeps; nothing new is fetched for it.

// Worked is one project with activity in the window.
type Worked struct {
	Name    string    `json:"name"`
	Project string    `json:"project"`
	Agent   string    `json:"agent"` // the last agent session, "" if none
	When    time.Time `json:"when"`
	Age     string    `json:"-"`    // display only; When is the data
	Note    string    `json:"note"` // the last session's last message, capped; "" if none
}

type Report struct {
	Since, Until                         time.Time
	NewMe, NewThem, NewIdeas, Closed     []board.Row
	Worked                               []Worked
	Attention                            []string
	OnYouNow, OnOthersNow, UnfinishedNow int
}

// JSON is sous report --json: the same window, its rows as Items.
type JSON struct {
	Since       time.Time    `json:"since"`
	Until       time.Time    `json:"until"`
	NewOnYou    []board.Item `json:"new_on_you"`
	NewOnOthers []board.Item `json:"new_on_others"`
	NewIdeas    []board.Item `json:"new_ideas"`
	Closed      []board.Item `json:"closed"`
	Worked      []Worked     `json:"worked"`
	Attention   []string     `json:"attention"`
	// Now: how many are waiting at the end of the window.
	Now struct {
		OnYou      int `json:"on_you"`
		OnOthers   int `json:"on_others"`
		Unfinished int `json:"unfinished"`
	} `json:"now"`
}

// JSON is r as --json shows it.
func (r Report) JSON() JSON {
	j := JSON{Since: r.Since.UTC(), Until: r.Until.UTC(), NewOnYou: board.Items(r.NewMe), NewOnOthers: board.Items(r.NewThem),
		NewIdeas: board.Items(r.NewIdeas), Closed: board.Items(r.Closed), Worked: r.Worked, Attention: append([]string{}, r.Attention...)}
	j.Now.OnYou, j.Now.OnOthers, j.Now.Unfinished = r.OnYouNow, r.OnOthersNow, r.UnfinishedNow
	return j
}

// Build slices board data by time. closed are the threads closed in the
// window, newest first (thread.ClosedSince); sessions are the session
// pointers.
func Build(d *board.Data, closed []thread.View, sessions map[string]session.Session, since, now time.Time) Report {
	s := board.Classify(d)
	return Report{
		Since: since, Until: now, Attention: s.Why,
		OnYouNow: len(s.Me), OnOthersNow: len(s.Them), UnfinishedNow: len(s.Unfinished),
		NewMe:    newSince(s.Me, since),
		NewThem:  newSince(s.Them, since),
		NewIdeas: ideasSince(d.Threads, since, now),
		Closed:   closedRows(closed, now),
		Worked:   worked(sessions, d.Projects, since, now),
	}
}

// newSince: the rows that began waiting in the window.
func newSince(rows []board.Row, since time.Time) []board.Row {
	var out []board.Row
	for _, r := range rows {
		if !r.Since.Before(since) {
			out = append(out, r)
		}
	}
	return out
}

func ideasSince(threads []thread.View, since, now time.Time) []board.Row {
	var out []board.Row
	for _, t := range threads {
		if t.Kind == thread.Idea && !t.Snoozed && !t.Since.Before(since) {
			out = append(out, board.ThreadRow(t, now))
		}
	}
	return out
}

// closedRows: the notes thread.ClosedSince gave for the window, as rows.
func closedRows(closed []thread.View, now time.Time) []board.Row {
	out := make([]board.Row, len(closed))
	for i, t := range closed {
		out[i] = board.ClosedRow(t, now)
	}
	return out
}

// worked: projects where an agent session ended or a commit landed in the
// window, most recent first, with the session's last words when there are
// some.
func worked(sessions map[string]session.Session, projects []project.Project, since, now time.Time) []Worked {
	byPath := map[string]*Worked{}
	for path, sess := range sessions {
		if sess.Ended.Before(since) {
			continue
		}
		w := &Worked{Name: filepath.Base(path), Project: path, Agent: sess.Agent, When: sess.Ended}
		if sess.LastMessage != nil {
			w.Note = text.Ellipsize(*sess.LastMessage, 120)
		}
		byPath[path] = w
	}
	for _, p := range projects {
		if p.LastCommit == nil || p.LastCommit.Before(since) {
			continue
		}
		if w, ok := byPath[p.Path]; ok {
			if p.LastCommit.After(w.When) {
				w.When = *p.LastCommit
			}
			continue
		}
		byPath[p.Path] = &Worked{Name: p.Name, Project: p.Path, When: *p.LastCommit}
	}
	var out []Worked
	for _, w := range byPath {
		w.Age = text.Age(now, w.When)
		out = append(out, *w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].When.After(out[j].When) })
	return out
}

// IdeaGroup is new ideas for one project.
type IdeaGroup struct {
	Name string
	Rows []board.Row
}

// IdeaGroups: new ideas by project, largest group first.
func (r Report) IdeaGroups() []IdeaGroup {
	by := map[string]*IdeaGroup{}
	var order []string
	for _, row := range r.NewIdeas {
		name := filepath.Base(row.Project)
		if by[name] == nil {
			by[name] = &IdeaGroup{Name: name}
			order = append(order, name)
		}
		by[name].Rows = append(by[name].Rows, row)
	}
	out := make([]IdeaGroup, 0, len(order))
	for _, n := range order {
		out = append(out, *by[n])
	}
	sort.SliceStable(out, func(i, j int) bool { return len(out[i].Rows) > len(out[j].Rows) })
	return out
}

func (r Report) sinceLabel() string { return text.When(r.Since) }

// RenderReport is the terminal layout.
func Render(w io.Writer, r Report) error {
	ew := &errWriter{w: w}
	render(ew, r)
	return ew.err
}

// errWriter keeps the first write error, so a renderer that prints many
// lines can report whether they all got out.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) Write(p []byte) (int, error) {
	if e.err != nil {
		return 0, e.err
	}
	n, err := e.w.Write(p)
	e.err = err
	return n, err
}

func render(w io.Writer, r Report) {
	head := fmt.Sprintf("sous report · since %s · %d new on you · %d closed", r.sinceLabel(), len(r.NewMe), len(r.Closed))
	if len(r.Attention) > 0 {
		head += fmt.Sprintf(" · %d needs attention", len(r.Attention))
	}
	fmt.Fprintln(w, head)
	if len(r.Attention) > 0 {
		fmt.Fprintf(w, "\n  needs attention\n  %s\n", strings.Join(r.Attention, " · "))
	}
	section := func(title string, rows []board.Row) {
		if len(rows) == 0 {
			return
		}
		fmt.Fprintf(w, "\n  %s\n", title)
		for _, row := range rows {
			fmt.Fprintln(w, row.Line())
		}
	}
	section("new on you", r.NewMe)
	section("new on others", r.NewThem)
	// Ideas are low-urgency by design: counted per project, not listed.
	if groups := r.IdeaGroups(); len(groups) > 0 {
		fmt.Fprintln(w, "\n  new ideas")
		for _, g := range groups {
			fmt.Fprintf(w, "  %-22.22s  %d\n", g.Name, len(g.Rows))
		}
	}
	if len(r.Closed) > 0 {
		fmt.Fprintln(w, "\n  closed")
		for _, row := range r.Closed {
			by := ""
			if row.ClosedBy == thread.ClosedByUpstream {
				by = " (upstream)"
			}
			row.ID = "✓ " + row.ID
			fmt.Fprintln(w, row.Line()+by)
		}
	}
	if len(r.Worked) > 0 {
		fmt.Fprintln(w, "\n  worked")
		for _, wk := range r.Worked {
			line := fmt.Sprintf("  %-22.22s  %s", wk.Name, wk.Age)
			if wk.Agent != "" {
				line += " · " + wk.Agent
			}
			if wk.Note != "" {
				line += fmt.Sprintf(" · %q", wk.Note)
			}
			fmt.Fprintln(w, line)
		}
	}
	fmt.Fprintf(w, "\n  now: %s · sous report --open for the page\n", board.Counts(r.OnYouNow, r.OnOthersNow, r.UnfinishedNow))
}

// WebURL turns a ref into a link, when the tracker has one.
func WebURL(ref *string) string {
	if ref == nil {
		return ""
	}
	r, ok := tracker.ParseRef(*ref)
	if !ok {
		return ""
	}
	return r.WebURL()
}

//go:embed report.html.tmpl
var reportHTML string

var reportTmpl = template.Must(template.New("report").Funcs(template.FuncMap{
	"base": filepath.Base,
	"url":  WebURL,
	"isThread": func(id string) bool {
		return id != "" && !signal.IsID(id)
	},
	"when":   text.When,
	"counts": board.Counts,
}).Parse(reportHTML))

// RenderReportHTML writes one self-contained page: inline CSS (Material
// Design 3 color roles and type scale), no scripts, no external loads.
func RenderHTML(w io.Writer, r Report) error { return reportTmpl.Execute(w, r) }
