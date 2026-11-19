// Package board assembles and renders the cross-project board and per-project resume view.
package board

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/memory"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/session"
	"github.com/bilal-/sous/internal/signal"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/text"
	"github.com/bilal-/sous/internal/thread"
)

type Data struct {
	Efforts          []EffortSummary       `json:"efforts,omitempty"`
	ContextAttention []string              `json:"context_attention,omitempty"`
	Projects         []project.Project     `json:"projects"`
	Signals          []signal.Observed     `json:"signals"`
	Threads          []thread.View         `json:"threads"`
	Plugins          []signal.PluginStatus `json:"plugins"`
	Checked          int                   `json:"checked"`
	Unavailable      int                   `json:"unavailable"`
	RenderedAt       time.Time             `json:"rendered_at"`
	// Next is each project's next step (from STATUS.md first, falling back to
	// private sous handoff memory or the latest session), the project least
	// recently updated first: the one most easily forgotten.
	Next []NextStep `json:"next,omitempty"`
}

// NextStep is where a project's handoff says to pick up, and when that
// handoff last changed.
type NextStep struct {
	Project string    `json:"project"`
	Step    string    `json:"step"`
	Updated time.Time `json:"updated"`
	Source  string    `json:"source,omitempty"`
	Branch  string    `json:"branch,omitempty"`
}

// nextSteps reads each project's STATUS.md first, falling back privately to
// the newest open handoff memory or recorded session when STATUS.md is absent
// or has no Next step. Unreadable sources are reported on warn and skipped.
func nextSteps(ps []project.Project, s *store.Store, cfg *config.Config, warn io.Writer) []NextStep {
	var sessions map[string]session.Session
	var mems []memory.Record
	if s != nil {
		var err error
		if sessions, err = session.All(s); err != nil && warn != nil {
			fmt.Fprintf(warn, "sous: cannot read sessions: %v\n", err)
		}
		if mems, err = memory.All(s); err != nil && warn != nil {
			fmt.Fprintf(warn, "sous: cannot read memories: %v\n", err)
		}
	}
	var out []NextStep
	for _, p := range ps {
		step, at, err := project.NextStep(p.Path)
		if err != nil {
			if warn != nil {
				fmt.Fprintf(warn, "sous: cannot read %s in %s: %v\n", project.StatusFile, p.Path, err)
			}
			continue
		}
		if step != "" {
			out = append(out, NextStep{Project: p.Path, Step: step, Updated: at.UTC(), Source: project.StatusFile})
			continue
		}
		if fb, ok := privateNextStep(p, cfg, mems, sessions); ok {
			out = append(out, fb)
		}
	}
	slices.SortStableFunc(out, func(a, b NextStep) int { return a.Updated.Compare(b.Updated) })
	return out
}

func privateNextStep(p project.Project, cfg *config.Config, mems []memory.Record, sessions map[string]session.Session) (NextStep, bool) {
	sc := memory.ForProject(cfg, p)
	var best memory.Record
	hasHandoff := false
	for _, r := range mems {
		if r.ForgottenAt != nil || r.SupersededBy != "" || r.Kind != memory.Handoff {
			continue
		}
		if !r.VisibleTo(sc) {
			continue
		}
		if !hasHandoff || r.CreatedAt.After(best.CreatedAt) {
			best = r
			hasHandoff = true
		}
	}
	sess, hasSess := sessions[p.Path]
	if !hasSess {
		if main, ok := project.MainOf(p.Path); ok {
			sess, hasSess = sessions[main]
		}
	}
	sessText := ""
	if hasSess {
		sessText = sessionStep(sess)
	}
	if hasHandoff && (!hasSess || sessText == "" || !sess.Ended.After(best.CreatedAt)) {
		return NextStep{
			Project: p.Path,
			Step:    text.OneLine(best.Text),
			Updated: best.CreatedAt.UTC(),
			Source:  "handoff",
		}, true
	}
	if hasSess && sessText != "" {
		return NextStep{
			Project: p.Path,
			Step:    sessText,
			Updated: sess.Ended.UTC(),
			Source:  "session",
			Branch:  sess.Branch,
		}, true
	}
	return NextStep{}, false
}

func sessionStep(s session.Session) string {
	if s.LastMessage != nil && strings.TrimSpace(*s.LastMessage) != "" {
		return text.OneLine(*s.LastMessage)
	}
	if s.Prompt != nil && strings.TrimSpace(*s.Prompt) != "" {
		return text.OneLine(*s.Prompt)
	}
	return ""
}

type Inputs struct {
	Store *store.Store
	Cfg   *config.Config
	Roots []string // overrides Cfg.Roots when set
	// Signals are the signal plugins to run: every one for the board,
	// the offline built ins for here (signal.Registry.All, .Offline).
	Signals []signal.Plugin
	Timeout time.Duration
	Warn    io.Writer
	Now     time.Time
	// Reconcile, when set, checks filed threads against their trackers
	// (closing what the tracker closed) before the board is built.
	Reconcile Reconciler
	// UserHome, when set, is where agents keep their own memory, read
	// into a project's context (agentmem). Empty reads none.
	UserHome string
}

// Reconciler is satisfied by *filing.Filer without board importing filing.
// It honours ctx: under the session hook the whole build shares one deadline.
type Reconciler func(ctx context.Context, views []thread.View) []thread.View

func Build(ctx context.Context, in Inputs) (*Data, error) {
	roots := in.Roots
	if len(roots) == 0 {
		roots = in.Cfg.Roots
	}
	if len(roots) == 0 {
		return nil, errors.New(config.NoRootsHint)
	}
	// Finding the projects reads only the filesystem; describing them runs
	// git in each, which overlaps the signal plugins' scan.
	paths, unavailable := project.DiscoverPaths(roots, in.Cfg.Ignore, in.Warn)
	described := make(chan []project.Project, 1)
	go func() { described <- project.DescribeAll(paths) }()
	col := signal.Collect(ctx, in.Signals, paths, in.Timeout)
	ps := <-described
	known := map[string]bool{}
	knownRemote := map[string]bool{}
	for _, p := range ps {
		known[p.Path] = true
		if p.Remote != nil {
			knownRemote[*p.Remote] = true
		}
	}
	obs, err := signal.Observe(in.Store, col, paths, in.Now)
	if err != nil {
		return nil, err
	}
	var sigs []signal.Observed
	for _, o := range obs {
		if known[o.Project] {
			sigs = append(sigs, o)
		}
	}
	ths, err := thread.Open(in.Store, in.Now)
	if err != nil {
		return nil, err
	}
	if in.Reconcile != nil {
		ths = in.Reconcile(ctx, ths)
	}
	es, problems := EffortSummaries(in.Store, in.Now, ths)
	// A note made in a worktree belongs to its repo's project, like the
	// worktree itself.
	for i := range ths {
		if main, ok := project.MainOf(ths[i].Project); ok {
			ths[i].Project = main
		}
	}
	// A scoped board (explicit folder) shows only that folder's threads. The
	// full board keeps every open thread: a note whose clone is gone is still
	// a commitment, shown rather than dropped.
	if len(in.Roots) > 0 {
		kept := ths[:0]
		for _, t := range ths {
			if known[t.Project] || (t.Remote != nil && knownRemote[*t.Remote]) {
				kept = append(kept, t)
			}
		}
		ths = kept
	}
	if len(in.Roots) > 0 {
		es, problems = scopeEfforts(es, problems, known)
	}
	return &Data{Projects: ps, Signals: sigs, Threads: ths, Plugins: col.Plugins, Checked: len(ps), Unavailable: unavailable, RenderedAt: in.Now.UTC(), Efforts: es, ContextAttention: problems, Next: nextSteps(ps, in.Store, in.Cfg, in.Warn)}, nil
}

// The width of a row's text on the board, and what the board says when it
// cut a note to fit.
const (
	lineText = 60
	cutNote  = "  (… cut short: sous show <n> for a whole note)"
)

// cut: the row's text is longer than the board shows.
func (r Row) cut() bool { return text.Ellipsize(r.Shown, lineText) != r.Shown }

// Line is a row as the board and the report lay it out.
func (r Row) Line() string {
	return fmt.Sprintf("  %-14s  %-24s  %-60s  %s", r.ID, text.Ellipsize(filepath.Base(r.Project), 24), text.Ellipsize(r.Shown, lineText), r.Age) + r.upstreamNote()
}

// upstreamNote says when a filed note's tracker could not vouch for it:
// missing data is shown, never passed off as fine.
func (r Row) upstreamNote() string {
	switch {
	case r.Upstream == "unknown":
		return " (ref missing)"
	case r.Upstream == "error":
		return " (status unavailable: " + r.UpstreamErr + ")"
	case r.RunErr != "":
		return " (status unavailable: " + r.RunErr + ")"
	case len(r.WaitsOn) > 0:
		return " (waits on " + waitsOn(r.WaitsOn, r.WaitsOn) + ")"
	}
	return ""
}

// waitsOn lists the notes in after, saying which of them are closed (not
// in open) and so no longer block.
func waitsOn(after, open []int) string {
	var out []string
	for _, n := range after {
		s := strconv.Itoa(n)
		if !slices.Contains(open, n) {
			s += " (closed)"
		}
		out = append(out, s)
	}
	return strings.Join(out, ", ")
}

// Render lays out the classified board. The headline is what the eye reads:
// it must not say zero when an obligation source failed or nothing was
// checked.
// Header is the line sous prints above the board in a terminal: which sous
// this is (exe, with ~ for home) and what it does.
func Header(exe string) string {
	return "sous · " + exe + " · one memory across all your projects"
}

func Render(w io.Writer, d *Data) { RenderSaved(w, d, d.RenderedAt) }

// RenderSaved draws a saved board as seen at now, so a board from another
// day says which day.
func RenderSaved(w io.Writer, d *Data, now time.Time) {
	s := Classify(d)
	g := groupRuns(&s)
	switch {
	case d.Checked == 0:
		fmt.Fprintln(w, "sous · nothing checked · no projects in your project folders")
	case s.Total()+g.total() == 0 && len(s.Why) == 0:
		fmt.Fprintln(w, "sous · 0 on you · nothing waiting")
	case len(s.Why) > 0:
		fmt.Fprintf(w, "sous · %s? on you (%s) · %d found · %d on others · %d unfinished\n", g.counts(), strings.Join(s.Why, ", "), len(s.Me), len(s.Them), len(s.Unfinished))
	default:
		fmt.Fprintln(w, "sous · "+headline(g, s))
	}
	cut := false
	section := func(title, hint string, rows []Row) {
		if len(rows) == 0 {
			return
		}
		if hint != "" {
			title += " · " + hint
		}
		fmt.Fprintf(w, "\n  %s\n", title)
		for _, r := range rows {
			fmt.Fprintln(w, r.Line())
			cut = cut || r.cut() && !signal.IsID(r.ID)
		}
	}
	section("needs you", `sous reply <n> "<answer>"`, g.needsYou)
	section("in progress", "", g.inProgress)
	section("ready to review", "sous show <n>", g.ready)
	section("on you", "", s.Me)
	section("on others", "", s.Them)
	section("unfinished", "", s.Unfinished)
	if cut {
		fmt.Fprintln(w, cutNote)
	}
	renderNext(w, d.Next, now)
	reviewHint(w, d.Threads, now, "sous review")
	if len(d.Efforts) > 0 {
		fmt.Fprintln(w, "\n  efforts · sous effort <name> for details")
		for _, e := range d.Efforts {
			renderEffortSummary(w, e)
		}
	}

	fmt.Fprintf(w, "\n  %d checked · %d unavailable%s · as of %s · sous snooze <id> to hide a row\n",
		d.Checked, d.Unavailable, pluginFailures(d), text.AsOf(d.RenderedAt, now))
}

// NextShown is how many projects' next steps the board lists.
const NextShown = 6

// renderNext lists where each project's handoff says to pick up, the
// longest untouched first, so a project not opened for weeks still says
// what comes next.
func renderNext(w io.Writer, next []NextStep, now time.Time) {
	if len(next) == 0 {
		return
	}
	hasFallback := false
	for _, n := range next {
		if n.Source != "" && n.Source != project.StatusFile {
			hasFallback = true
			break
		}
	}
	if hasFallback {
		fmt.Fprintln(w, "\n  next up · from STATUS.md and recent work")
	} else {
		fmt.Fprintln(w, "\n  next up · from each project's STATUS.md")
	}
	for i, n := range next {
		if i == NextShown {
			fmt.Fprintf(w, "  … %d more · sous --json lists them all\n", len(next)-NextShown)
			break
		}
		step := n.Step
		if n.Source != "" && n.Source != project.StatusFile {
			tag := n.Source
			if n.Branch != "" {
				tag += " · " + n.Branch
			}
			step = "[" + tag + "] " + step
		}
		fmt.Fprintf(w, "  %-14s  %-24s  %-60s  %s\n", "", text.Ellipsize(filepath.Base(n.Project), 24), text.Ellipsize(step, lineText), text.Age(now, n.Updated))
	}
}

// Cache: the last rendered board, so the zsh surface can print in ~5 ms and
// say how old what it printed is.
type CacheDoc struct {
	Version    int        `json:"version"`
	RenderedAt *time.Time `json:"rendered_at"`
	Board      *string    `json:"board"`
	Data       *Data      `json:"data,omitempty"` // additive: --menubar renders from it
}

// cacheFile is how the file is read and upgraded.
var cacheFile = store.V1(`{"version":1,"rendered_at":null,"board":null}`)

// ReadCache is the last board written (empty before the first).
func ReadCache(s *store.Store) (*CacheDoc, error) {
	return store.Load[CacheDoc](s, "cache", cacheFile)
}

// ReadCurrentCache keeps the remote snapshot but reads notes from their
// local source. A completed or kept note must not wait for a network refresh
// before the shell, session summary or menu bar reflects that change.
func ReadCurrentCache(s *store.Store, now time.Time) (*CacheDoc, error) {
	c, err := ReadCache(s)
	if err != nil || c.Data == nil {
		return c, err
	}
	c.Data.Efforts, c.Data.ContextAttention = EffortSummaries(s, now, nil)
	views, err := thread.Open(s, now)
	if err != nil {
		return nil, err
	}
	previous := map[int]thread.View{}
	for _, v := range c.Data.Threads {
		previous[v.ID] = v
	}
	for i := range views {
		v := &views[i]
		old, ok := previous[v.ID]
		if !ok || old.UID != v.UID {
			continue
		}
		if old.Ref != nil && v.Ref != nil && *old.Ref == *v.Ref {
			v.Upstream, v.UpstreamErr = old.Upstream, old.UpstreamErr
		}
		if old.Run != nil && v.Run != nil && old.Run.Revision == v.Run.Revision && old.Run.Ref == v.Run.Ref && old.Run.State == v.Run.State &&
			((old.Run.Checked == nil && v.Run.Checked == nil) || (old.Run.Checked != nil && v.Run.Checked != nil && old.Run.Checked.Equal(*v.Run.Checked))) {
			v.RunErr = old.RunErr
		}
	}
	c.Data.Threads = views
	snoozed, err := signal.Snoozes(s)
	if err != nil {
		return nil, err
	}
	for i := range c.Data.Signals {
		if hidden, ok := snoozed[c.Data.Signals[i].ID]; ok {
			c.Data.Signals[i].Snoozed = hidden
		}
	}
	return c, nil
}

func WriteCache(s *store.Store, d *Data) error {
	var b strings.Builder
	Render(&b, d)
	text := b.String()
	_, err := store.Modify[CacheDoc](s, "cache", cacheFile, func(c *CacheDoc) error {
		c.RenderedAt = &d.RenderedAt
		c.Board = &text
		c.Data = d
		return nil
	})
	return err
}

// Ambient is the at-most-once-per-window rule for new shells.
type Ambient struct{ Home string }

func (a Ambient) stamp() string { return filepath.Join(a.Home, ".ambient-stamp") }

// Run calls show when the window since the last showing has passed. When
// show reports nothing was shown (no board yet), the window does not
// restart, so the next shell tries again.
//
// Shells opened together take turns, so the board prints once.
func (a Ambient) Run(now time.Time, window time.Duration, show func() bool) {
	(&store.Store{Home: a.Home}).Locked("ambient", func() error {
		if st, err := os.Stat(a.stamp()); err == nil && now.Sub(st.ModTime()) < window {
			return nil
		}
		if show() {
			store.WriteFile(a.stamp(), nil, 0o644)
		}
		return nil
	})
}

// Summary is the saved board in one line, for an agent starting a session
// outside any project: how much waits, where, as of when, and how to see
// it now.
func Summary(d *Data, now time.Time) string {
	s := Classify(d)
	line := s.Counts() + " across " + text.Plural(d.Checked, "project")
	if len(s.Why) > 0 {
		line += " (" + strings.Join(s.Why, ", ") + ")"
	}
	return line + " · as of " + text.Ago(now, d.RenderedAt) + " · sous for the board"
}
