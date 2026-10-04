package board

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"slices"
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
	Documents      []project.Document    `json:"documents"`
	DocumentError  string                `json:"document_error,omitempty"`
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
	if d.Documents, err = project.Documents(root); err != nil {
		d.DocumentError = err.Error()
	}
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
	s := classifyHere(d)
	renderHereHeader(w, d, s, now, brief)
	if len(d.Documents) > 0 {
		paths := []string{}
		for _, doc := range d.Documents {
			paths = append(paths, doc.Path)
		}
		fmt.Fprintln(w, "project docs · "+strings.Join(paths, " · "))
	}
	fmt.Fprintf(w, "on you: %d · on others: %d · ideas: %d\n", len(s.Me), len(s.Them), len(s.Ideas))
	// What an agent hears at session start stays short: a few rows of each
	// kind, then where the rest are.
	limit := 0
	if brief {
		limit = briefRows
	}
	cut := false
	list := func(rows []Row, suffix string) {
		shown := rows
		if limit > 0 && len(rows) > limit {
			// What waits now first: a snoozed row takes a slot last.
			shown = slices.Clone(rows)
			slices.SortStableFunc(shown, func(a, b Row) int { return cmp.Compare(boolInt(a.Snoozed), boolInt(b.Snoozed)) })
			shown = shown[:limit]
		}
		for _, r := range shown {
			line, c := r.hereLine(suffix)
			cut = cut || c && !signal.IsID(r.ID) // only a note can be shown whole
			fmt.Fprintln(w, line)
		}
		if len(shown) < len(rows) {
			fmt.Fprintf(w, "  … and %d more (sous %s)\n", len(rows)-len(shown), project.OrgName(d.Project))
		}
	}
	list(s.Me, "")
	list(s.Them, "  (them)")
	list(s.Ideas, "  (idea)")
	if cut {
		fmt.Fprintln(w, cutNote)
	}
	for _, r := range s.RecentlyClosed {
		fmt.Fprintf(w, "  ✓ %s  %s  (closed upstream %s)\n", r.ID, r.Shown, r.Age)
	}
	reviewHint(w, d.Threads, now, "sous review -p "+project.OrgName(d.Project))
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
		line := "  " + strings.Join(parts, " · ")
		if brief {
			line = text.Fit("", line, "")
		}
		fmt.Fprintln(w, line)
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
	if sess.LastMessage == nil || *sess.LastMessage == "" {
		return line
	}
	if brief {
		return text.Fit(line+` · ended: "`, *sess.LastMessage, `"`)
	}
	return line + fmt.Sprintf(" · ended: %q", *sess.LastMessage)
}

// hereLine is a row as here lists it, on one line: id, text, age, then
// what is known about it (snoozed, filed where, upstream trouble) and
// suffix. The text is cut to fit.
func (r Row) hereLine(suffix string) (line string, cut bool) {
	tail := "  " + r.Age
	if r.Snoozed {
		tail += " (snoozed)"
	}
	if r.Ref != nil {
		tail += "  → " + *r.Ref
	}
	tail += r.upstreamNote() + suffix
	head := fmt.Sprintf("  %s  ", r.ID)
	line = text.Fit(head, r.Shown, tail)
	return line, line != head+r.Shown+tail
}

// briefRows is how many rows of each kind the short form (what an agent
// hears at session start) lists.
const briefRows = 5

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
