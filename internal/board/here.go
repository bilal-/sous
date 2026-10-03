package board

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/session"
	"github.com/bilal-/sous/internal/signal"
	"github.com/bilal-/sous/internal/text"
	"github.com/bilal-/sous/internal/thread"
)

// HereData is the resume view for one project: git facts, the last agent
// session, every signal the plugins report for it (same door as the board),
// and its threads including ideas.
type HereData struct {
	Project        string                `json:"project"`
	Name           string                `json:"name"`
	Remote         *string               `json:"remote"`
	Facts          project.Facts         `json:"facts"`
	Session        *session.Session      `json:"session"`
	Signals        []signal.Observed     `json:"signals"`
	Plugins        []signal.PluginStatus `json:"plugins"`
	Threads        []thread.View         `json:"threads"`
	RecentlyClosed []thread.View         `json:"recently_closed"`
	RenderedAt     time.Time             `json:"rendered_at"`
}

// BuildHere assembles the resume view. It is local and bounded — it runs in
// the session hook and before `sous go` execs — so only the local built-in
// plugins run here (through the same door as the board, no in-process
// shortcut); everything the board observed from remote trackers is read
// back from observed.json. in.Signals is the list to run, normally
// signal.Registry.Offline.
func BuildHere(ctx context.Context, in Inputs, root string) (*HereData, error) {
	p := project.Describe(root)
	d := &HereData{Project: root, Name: p.Name, Remote: p.Remote, RenderedAt: in.Now.UTC()}
	facts, err := project.ReadFacts(root)
	if err != nil {
		return nil, err
	}
	d.Facts = facts
	col := signal.Collect(ctx, in.Signals, []string{root}, in.Timeout)
	if d.Signals, err = hereSignals(in, col, root); err != nil {
		return nil, err
	}
	d.Plugins = col.Plugins
	sessions, err := session.All(in.Store)
	if err != nil {
		return nil, err
	}
	if sess, ok := sessions[root]; ok {
		d.Session = &sess
	}
	remote := ""
	if p.Remote != nil {
		remote = *p.Remote
	}
	if d.Threads, err = thread.ForProject(in.Store, root, remote, in.Now); err != nil {
		return nil, err
	}
	if in.Reconcile != nil {
		d.Threads = in.Reconcile(ctx, d.Threads)
	}
	if d.RecentlyClosed, err = thread.RecentlyClosedUpstream(in.Store, root, remote, in.Now, 7*24*time.Hour); err != nil {
		return nil, err
	}
	return d, nil
}

// hereSignals: what the plugins run here found for root, joined with what
// the board learned last from the others (remote trackers), minus git's
// informational rows. Nothing but root's observations is touched.
func hereSignals(in Inputs, col signal.Collected, root string) ([]signal.Observed, error) {
	obs, err := signal.Observe(in.Store, col, []string{root}, in.Now) // only the plugins run here are settled
	if err != nil {
		return nil, err
	}
	known, err := signal.Known(in.Store, root)
	if err != nil {
		return nil, err
	}
	var out []signal.Observed
	seen := map[string]bool{}
	for _, o := range append(obs, known...) {
		if o.Project == root && o.Kind != signal.Info && !seen[o.ID] {
			seen[o.ID] = true
			out = append(out, o)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// RenderHere lays out the resume view: facts, session, then sections.
func RenderHere(w io.Writer, d *HereData, now time.Time, brief bool) {
	s := ClassifyHere(d)
	renderHereHeader(w, d, s, now, brief)
	fmt.Fprintf(w, "on you: %d · on others: %d · ideas: %d\n", len(s.Me), len(s.Them), len(s.Ideas))
	for _, r := range s.Me {
		fmt.Fprintln(w, r.hereLine())
	}
	for _, r := range s.Them {
		fmt.Fprintln(w, r.hereLine()+"  (them)")
	}
	shown := s.Ideas
	if brief && len(s.Ideas) > 5 {
		shown = s.Ideas[:5]
	}
	for _, r := range shown {
		fmt.Fprintln(w, r.hereLine())
	}
	if len(shown) < len(s.Ideas) {
		fmt.Fprintf(w, "  … and %d more (sous %s)\n", len(s.Ideas)-len(shown), filepath.Base(d.Project))
	}
	for _, r := range s.RecentlyClosed {
		fmt.Fprintf(w, "  ✓ %s  %s  (closed upstream %s)\n", r.ID, r.Shown, r.Age)
	}
	fmt.Fprintln(w, "\n  sous note \"…\" to add · sous done <n> to close · sous kind <n> me to escalate")
}

// renderHereHeader: the project, branch and last commit, work left in git,
// how the last agent session ended, and anything that makes the picture
// incomplete. Upstream state is not in the title: the git signal reports
// it as a row with an id, which can be snoozed.
func renderHereHeader(w io.Writer, d *HereData, s Sections, now time.Time, brief bool) {
	title := d.Name + " · " + d.Facts.Branch
	if d.Facts.LastCommit != nil {
		title += " · last commit " + text.Ago(now, *d.Facts.LastCommit)
	}
	fmt.Fprintln(w, title)
	if d.Facts.LastSubject != "" {
		fmt.Fprintf(w, "  %q\n", d.Facts.LastSubject)
	}
	if len(s.Unfinished) > 0 {
		parts := make([]string, len(s.Unfinished))
		for i, r := range s.Unfinished {
			parts[i] = r.Shown
			if r.Stale {
				parts[i] += " (stale)"
			}
		}
		fmt.Fprintf(w, "  %s\n", strings.Join(parts, " · "))
	}
	fmt.Fprintln(w, sessionLine(d.Session, brief))
	if len(s.Why) > 0 {
		fmt.Fprintf(w, "  (%s)\n", strings.Join(s.Why, " · "))
	}
}

func sessionLine(sess *session.Session, brief bool) string {
	if sess == nil {
		return "last session · none recorded"
	}
	line := fmt.Sprintf("last session · %s · %s", sess.Agent, sess.Ended.Format("2006-01-02"))
	if sess.LastMessage != nil && *sess.LastMessage != "" {
		msg := *sess.LastMessage
		if brief {
			msg = text.Ellipsize(msg, session.LastMessageRunes)
		}
		line += fmt.Sprintf(" · ended: %q", msg)
	}
	return line
}

// hereLine is a row as here lists it: id, text, age, then what is known
// about it (snoozed, filed where, upstream trouble).
func (r Row) hereLine() string {
	l := fmt.Sprintf("  %s  %s  %s", r.ID, r.Shown, r.Age)
	if r.Snoozed {
		l += " (snoozed)"
	}
	if r.Ref != nil {
		l += "  → " + *r.Ref
	}
	return l + r.upstreamNote()
}
