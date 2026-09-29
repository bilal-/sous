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
// back from observed.json. in.Builtins is the list to run, normally
// signal.LocalBuiltins().
func BuildHere(ctx context.Context, in Inputs, root string) (*HereData, error) {
	p := project.Describe(root)
	d := &HereData{Project: root, Name: p.Name, Remote: p.Remote, RenderedAt: in.Now.UTC()}
	facts, err := project.ReadFacts(root)
	if err != nil {
		return nil, err
	}
	d.Facts = facts
	col := signal.Collect(ctx, signal.Plugins(in.Exe, in.Builtins, nil), []string{root}, in.Timeout)
	obs, err := signal.Observe(in.Store, col, nil, in.Now) // scope nil: touch no other plugin's observations
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, o := range obs {
		if o.Project == root && o.Kind != signal.Info {
			d.Signals = append(d.Signals, o)
			seen[o.ID] = true
		}
	}
	// What the board already learned from remote trackers for this project.
	known, err := signal.Known(in.Store, root)
	if err != nil {
		return nil, err
	}
	for _, o := range known {
		if !seen[o.ID] && o.Kind != signal.Info {
			d.Signals = append(d.Signals, o)
		}
	}
	sort.Slice(d.Signals, func(i, j int) bool { return d.Signals[i].ID < d.Signals[j].ID })
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

// Ellipsize shortens s to at most n runes, ending in "…" when cut. Counting
// runes, not bytes, keeps a cut from splitting a character.
func Ellipsize(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 0 {
		return ""
	}
	return string(r[:n-1]) + "…"
}

// RenderHere lays out the resume view: facts, session, then sections.
func RenderHere(w io.Writer, d *HereData, now time.Time, brief bool) {
	// Upstream state is not in the title: the git signal reports it as a
	// row with an id, which can be snoozed for intentional local branches.
	title := d.Name + " · " + d.Facts.Branch
	if d.Facts.LastCommit != nil {
		title += " · last commit " + project.Ago(now, *d.Facts.LastCommit)
	}
	fmt.Fprintln(w, title)
	if d.Facts.LastSubject != "" {
		fmt.Fprintf(w, "  %q\n", d.Facts.LastSubject)
	}
	s := ClassifyHere(d)
	if len(s.Unfinished) > 0 {
		var parts []string
		for _, r := range s.Unfinished {
			if r.Stale {
				parts = append(parts, r.Text+" (stale)")
			} else {
				parts = append(parts, r.Text)
			}
		}
		fmt.Fprintf(w, "  %s\n", strings.Join(parts, " · "))
	}
	if d.Session == nil {
		fmt.Fprintln(w, "last session · none recorded")
	} else {
		line := fmt.Sprintf("last session · %s · %s", d.Session.Agent, d.Session.Ended.Format("2006-01-02"))
		if d.Session.LastMessage != nil && *d.Session.LastMessage != "" {
			msg := *d.Session.LastMessage
			if brief {
				msg = Ellipsize(msg, 300)
			}
			line += fmt.Sprintf(" · ended: %q", msg)
		}
		fmt.Fprintln(w, line)
	}
	if len(s.Why) > 0 {
		fmt.Fprintf(w, "  (%s)\n", strings.Join(s.Why, " · "))
	}
	fmt.Fprintf(w, "on you: %d · on others: %d · ideas: %d\n", len(s.Me), len(s.Them), len(s.Ideas))
	line := func(r Row, suffix string) {
		l := fmt.Sprintf("  %s  %s  %s", r.ID, r.Text, r.Age)
		if r.Snoozed {
			l += " (snoozed)"
		}
		if r.Ref != nil {
			l += "  → " + *r.Ref
		}
		l += r.upstreamNote()
		fmt.Fprintln(w, l+suffix)
	}
	for _, r := range s.Me {
		line(r, "")
	}
	for _, r := range s.Them {
		line(r, "  (them)")
	}
	shown := s.Ideas
	if brief && len(s.Ideas) > 5 {
		shown = s.Ideas[:5]
	}
	for _, r := range shown {
		line(r, "")
	}
	if len(shown) < len(s.Ideas) {
		fmt.Fprintf(w, "  … and %d more (sous %s)\n", len(s.Ideas)-len(shown), filepath.Base(d.Project))
	}
	for _, r := range s.RecentlyClosed {
		fmt.Fprintf(w, "  ✓ %s  %s  (closed upstream %s)\n", r.ID, r.Text, r.Age)
	}
	fmt.Fprintln(w, "\n  sous note \"…\" to add · sous done <n> to close · sous kind <n> me to escalate")
}
