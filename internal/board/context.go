package board

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/effort"
	"github.com/bilal-/sous/internal/memory"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/text"
)

// Context joins private history to current work without changing either.
type Context struct {
	Available bool            `json:"available"`
	Scope     memory.Scope    `json:"scope"`
	Memories  []memory.Record `json:"memories"`
	Efforts   []EffortSummary `json:"efforts"`
	Attention []string        `json:"attention"`
}
type EffortSummary struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Projects    []memory.Scope `json:"projects"`
	Open        int            `json:"open"`
	Completed   int            `json:"completed"`
	Blocked     int            `json:"blocked"`
	Unavailable int            `json:"unavailable"`
	Stale       bool           `json:"stale"`
	Archived    bool           `json:"archived"`
	Attention   []string       `json:"attention"`
}
type Blocker struct {
	NoteID  int  `json:"note_id"`
	AfterID int  `json:"after_id"`
	Unknown bool `json:"unknown"`
}
type EffortDetail struct {
	EffortSummary
	Definition effort.Effort   `json:"definition"`
	Items      []Item          `json:"items"`
	Memories   []memory.Record `json:"memories"`
	Blockers   []Blocker       `json:"blockers"`
	AsOf       *time.Time      `json:"as_of"`
}

func EffortSummaries(s *store.Store, now time.Time) ([]EffortSummary, []string) {
	es, err := effort.All(s)
	if err != nil {
		var previous []EffortSummary
		if cache, e := ReadCache(s); e == nil && cache.Data != nil {
			previous = slices.Clone(cache.Data.Efforts)
			for i := range previous {
				previous[i].Stale = true
			}
		}
		return previous, []string{"efforts unavailable: " + err.Error()}
	}
	views := []EffortSummary{}
	problems := []string{}
	active := []effort.Effort{}
	for _, e := range es {
		if e.ArchivedAt == nil {
			active = append(active, e)
		}
	}
	for _, v := range DescribeEfforts(s, active, now) {
		views = append(views, v.EffortSummary)
		if len(v.Attention) > 0 {
			problems = append(problems, v.Name+": "+strings.Join(v.Attention, "; "))
		}
	}
	return views, problems
}

// Filter failures with their effort; a scoped board must not reveal unrelated
// project names in its headline. A store-wide read failure remains visible.
func scopeEfforts(views []EffortSummary, problems []string, known map[string]bool) ([]EffortSummary, []string) {
	kept := []EffortSummary{}
	issues := []string{}
	for _, p := range problems {
		if strings.HasPrefix(p, "efforts unavailable:") {
			issues = append(issues, p)
		}
	}
	for _, e := range views {
		for _, p := range e.Projects {
			if known[p.Project] {
				kept = append(kept, e)
				if len(e.Attention) > 0 {
					issues = append(issues, e.Name+": "+strings.Join(e.Attention, "; "))
				}
				break
			}
		}
	}
	return kept, issues
}

func ProjectContext(s *store.Store, sc memory.Scope, now time.Time) Context {
	c := Context{Available: true, Scope: sc, Memories: []memory.Record{}, Efforts: []EffortSummary{}, Attention: []string{}}
	records, err := memory.Search(s, memory.Query{Scope: sc})
	if err != nil {
		c.Available = false
		c.Attention = append(c.Attention, "memory unavailable: "+err.Error())
	} else {
		c.Memories = records
	}
	es, err := effort.All(s)
	if err != nil {
		c.Available = false
		c.Attention = append(c.Attention, "efforts unavailable: "+err.Error())
		if cached, loadErr := ReadCache(s); loadErr == nil && cached.Data != nil {
			for _, e := range cached.Data.Efforts {
				if slices.ContainsFunc(e.Projects, sc.Same) {
					e.Stale = true
					c.Efforts = append(c.Efforts, e)
				}
			}
		}
		return c
	}
	selected := []effort.Effort{}
	for _, e := range es {
		if e.ArchivedAt == nil && e.Includes(sc) {
			selected = append(selected, e)
		}
	}
	for _, v := range DescribeEfforts(s, selected, now) {
		c.Efforts = append(c.Efforts, v.EffortSummary)
		c.Attention = append(c.Attention, v.Attention...)
		if len(v.Attention) > 0 {
			c.Available = false
		}
	}
	return c
}

func renderContext(w io.Writer, c Context, brief bool) {
	if len(c.Memories) > 0 {
		recall := "sous recall -p " + text.ShellArg(c.Scope.Project)
		fmt.Fprintf(w, "\nmemory · %d current\n  %s --history for earlier context\n", len(c.Memories), recall)
		records := c.Memories
		if brief {
			records = briefMemories(records, briefRows)
		}
		for _, r := range records {
			renderMemory(w, r)
		}
		if len(records) < len(c.Memories) {
			fmt.Fprintf(w, "  … and %d more · %s\n", len(c.Memories)-len(records), recall)
		}
		fmt.Fprintln(w, "  sous recall --show <m:id> for a complete record and its source")
	}
	if len(c.Efforts) > 0 {
		fmt.Fprintln(w, "\nefforts · sous effort <name> for linked work and dependencies")
		shown := c.Efforts
		if brief && len(shown) > briefRows {
			shown = shown[:briefRows]
		}
		for _, e := range shown {
			renderEffortSummary(w, e)
		}
		if len(shown) < len(c.Efforts) {
			fmt.Fprintf(w, "  … and %d more · sous effort\n", len(c.Efforts)-len(shown))
		}
	}
}

// Keep the newest of each kind before filling remaining slots by recency.
// A busy stream of handoffs must not hide a standing constraint or decision.
func briefMemories(records []memory.Record, limit int) []memory.Record {
	if len(records) <= limit {
		return records
	}
	selected := map[int]bool{}
	for _, kind := range []memory.Kind{memory.Constraint, memory.Decision, memory.Question, memory.Handoff, memory.Lesson} {
		for i, r := range records {
			if r.Kind == kind && len(selected) < limit {
				selected[i] = true
				break
			}
		}
	}
	for i := range records {
		if len(selected) >= limit {
			break
		}
		selected[i] = true
	}
	out := make([]memory.Record, 0, limit)
	for i, r := range records {
		if selected[i] {
			out = append(out, r)
		}
	}
	return out
}

func renderMemory(w io.Writer, r memory.Record) {
	state := "unverified"
	if r.VerifiedAt != nil {
		state = "verified"
	}
	if r.SupersededBy != "" {
		state = "superseded by " + r.SupersededBy
	}
	if r.ForgottenAt != nil {
		state = "forgotten"
	}
	head := fmt.Sprintf("  %s %s · ", r.ID, r.Kind)
	if r.Scope.Project != "" {
		head += project.OrgName(r.Scope.Project) + " · "
	}
	source := r.Source
	if source == "" {
		source = "direct capture by " + r.Author
	}
	fmt.Fprintln(w, text.Fit(head, text.OneLine(r.Text), " · "+text.Ellipsize(source, 35)+" · "+state))
}
func RenderMemories(w io.Writer, result memory.Result) {
	fmt.Fprintf(w, "%d memories", result.TotalCount)
	if result.Truncated {
		fmt.Fprintf(w, " · showing %d · repeat without --limit for all matches", len(result.Records))
	}
	fmt.Fprintln(w, " · sous recall --show <m:id> for a full record")
	for _, r := range result.Records {
		renderMemory(w, r)
	}
}
func renderEffortSummary(w io.Writer, e EffortSummary) {
	tail := fmt.Sprintf(" · %d linked open · %d completed · %d blocked", e.Open, e.Completed, e.Blocked)
	if e.Unavailable > 0 {
		tail += fmt.Sprintf(" · %d unavailable", e.Unavailable)
	}
	if e.Stale {
		tail += " · stale"
	}
	if e.Archived {
		tail += " · archived"
	}
	fmt.Fprintln(w, text.Fit("  "+e.ID+" ", e.Name, tail))
}
func RenderEfforts(w io.Writer, es []EffortSummary) {
	fmt.Fprintf(w, "%d efforts · sous effort <name> for details · sous effort add <name> to create\n", len(es))
	for _, e := range es {
		renderEffortSummary(w, e)
	}
}
func RenderEffort(w io.Writer, v EffortDetail) {
	renderEffortSummary(w, v.EffortSummary)
	for _, p := range v.Projects {
		fmt.Fprintf(w, "  project · %s · workspace %s\n", project.OrgName(p.Project), p.Workspace)
	}
	for _, item := range v.Items {
		state := item.Kind
		if item.ClosedAt != nil {
			state = "closed"
		}
		fmt.Fprintln(w, text.Fit("  "+item.ID+" ", item.Text, " · "+state))
	}
	for _, b := range v.Blockers {
		suffix := ""
		if b.Unknown {
			suffix = " (state unavailable)"
		}
		fmt.Fprintf(w, "  %d waits for %d%s\n", b.NoteID, b.AfterID, suffix)
	}
	for _, r := range v.Memories {
		renderMemory(w, r)
	}
	for _, ref := range v.Definition.Refs {
		fmt.Fprintln(w, "  reference · "+ref)
	}
	for _, problem := range v.Attention {
		fmt.Fprintln(w, "  unavailable · "+problem)
	}
	if v.AsOf != nil {
		fmt.Fprintln(w, "  tracker snapshot · "+v.AsOf.Format(time.RFC3339))
	}
	fmt.Fprintln(w, "  sous show <n> for current work · sous effort link <name> --note <n> to connect it")
}
