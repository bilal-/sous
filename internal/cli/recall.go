package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/agentmem"
	"github.com/bilal-/sous/internal/board"
	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/effort"
	"github.com/bilal-/sous/internal/memory"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/session"
	"github.com/bilal-/sous/internal/text"
	"github.com/bilal-/sous/internal/thread"
)

func cmdRecall(e *Env, a argv) int {
	for _, key := range []string{"show", "p", "workspace", "effort", "kind"} {
		if a.has(key) && strings.TrimSpace(a.value(key)) == "" {
			return fail(e, exitUsage, "%s needs a nonempty value", key)
		}
	}
	if id := a.value("show"); a.has("show") {
		if len(a.pos) > 0 || len(a.set) != 1 {
			return fail(e, exitUsage, "--show takes only a memory id")
		}
		get := func() (memory.Record, error) { return memory.Get(e.store(), id) }
		switch {
		case agentmem.Owned(id):
			get = func() (memory.Record, error) { return agentmem.Get(e.UserHome, e.Cfg, id) }
		case isExtraRecallID(id):
			get = func() (memory.Record, error) {
				ps := discoveredProjects(e)
				for _, r := range recallExtras(e, ps) {
					if r.ID == id {
						return r, nil
					}
				}
				return memory.Record{}, fmt.Errorf("%w %s", memory.ErrNotFound, id)
			}
		}
		r, err := get()
		if err != nil {
			return memoryErr(e, err)
		}
		if e.JSON {
			return e.writeJSON(r)
		}
		fmt.Fprintf(e.Stdout, "%s · %s · %s\nproject · %s\nworkspace · %s\nsource · %s\nauthor · %s\n", r.ID, r.Kind, r.CreatedAt.Format(time.RFC3339), r.Scope.Project, r.Scope.Workspace, r.Source, r.Author)
		if r.ForgottenAt != nil {
			fmt.Fprintln(e.Stdout, "forgotten · content removed")
		} else {
			fmt.Fprintln(e.Stdout, r.Text)
		}
		if r.SupersededBy != "" {
			fmt.Fprintln(e.Stdout, "superseded by · "+r.SupersededBy)
		}
		if r.Supersedes != "" {
			fmt.Fprintln(e.Stdout, "supersedes · "+r.Supersedes)
		}
		switch {
		case r.Path != "":
			fmt.Fprintf(e.Stdout, "file · %s · %s keeps it; sous only reads it\n", config.Tilde(e.UserHome, r.Path), r.Author)
		case isExtraRecallID(r.ID):
			// Document, open note, and session entries have their own stores.
		case r.VerifiedAt != nil:
			fmt.Fprintf(e.Stdout, "verified · %s · %s\n", r.VerifiedAt.Format(time.RFC3339), r.VerificationSource)
		default:
			fmt.Fprintln(e.Stdout, "unverified · recorded context, not proof of completed work")
		}
		for _, target := range r.SharedWith {
			fmt.Fprintf(e.Stdout, "shared with · %s · %s\n", target.Workspace, target.Project)
		}
		if !isExtraRecallID(r.ID) {
			flag, value, label := memory.HistoryTarget(e.Cfg, r.Scope)
			fmt.Fprintf(e.Stdout, "sous recall %s %s --history for %s\n", flag, text.ShellArg(value), label)
		}
		return 0
	}
	selectors := 0
	for _, key := range []string{"p", "workspace", "all", "effort"} {
		if a.has(key) {
			selectors++
		}
	}
	if selectors > 1 {
		return fail(e, exitUsage, "choose one of -p, --workspace, --effort or --all")
	}
	q := memory.Query{All: a.has("all"), Workspace: a.value("workspace"), History: a.has("history"), Kind: memory.Kind(a.value("kind"))}
	if len(a.pos) > 0 {
		q.Text = a.pos[0]
	}
	if a.has("limit") {
		n, err := strconv.Atoi(a.value("limit"))
		if err != nil || n < 1 || n > 1000 {
			return fail(e, exitUsage, "--limit must be between 1 and 1000")
		}
		q.Limit = n
	}
	var scopeProjects []project.Project
	if a.has("effort") {
		group, err := effort.Get(e.store(), a.value("effort"))
		if err != nil {
			return memoryErr(e, err)
		}
		q.All = true
		q.IDs = []string{}
		for _, r := range group.Memories {
			q.IDs = append(q.IDs, r.ID)
		}
	} else if !q.All && q.Workspace == "" {
		proj, scope, code := resolveMemoryProject(e, a.value("p"))
		if code != 0 {
			return code
		}
		q.Scope = scope
		q.Also = agentmem.ForProject(e.UserHome, e.Cfg, scope.Project)
		scopeProjects = []project.Project{proj}
	}
	if q.All && !a.has("effort") || q.Workspace != "" {
		q.Also = agentmem.All(e.UserHome, e.Cfg)
		scopeProjects = discoveredProjects(e)
	}
	if !q.History && !a.has("effort") {
		q.Also = append(q.Also, recallExtras(e, scopeProjects)...)
	}
	result, err := memory.Find(e.store(), q)
	if err != nil {
		return memoryErr(e, err)
	}
	if e.JSON {
		return e.writeJSON(result)
	}
	board.RenderMemories(e.Stdout, result)
	return 0
}

func isExtraRecallID(id string) bool {
	return strings.HasPrefix(id, "d:") || strings.HasPrefix(id, "n:") || strings.HasPrefix(id, "s:")
}

func discoveredProjects(e *Env) []project.Project {
	if e.Cfg == nil || len(e.Cfg.Roots) == 0 {
		return nil
	}
	ps, _ := project.Discover(e.Cfg.Roots, e.Cfg.Ignore, io.Discard)
	return ps
}

func shortRecallHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:4])
}

// recallExtras projects repo STATUS.md / FOLLOWUPS.md docs, open sous notes,
// and rolling session history as read-only searchable memory records.
func recallExtras(e *Env, projects []project.Project) []memory.Record {
	var out []memory.Record
	byPath := map[string]project.Project{}
	for _, p := range projects {
		byPath[p.Path] = p
		hash := shortRecallHash(p.Path)
		sc := memory.ForProject(e.Cfg, p)
		if st, err := project.ReadStatus(p.Path); err == nil && st.Exists {
			body := strings.TrimSpace(st.Body)
			if body == "" {
				body = st.NextStep
			}
			if body != "" {
				out = append(out, memory.Record{
					ID:        "d:" + hash + ":STATUS.md",
					Scope:     sc,
					Kind:      memory.Handoff,
					Text:      body,
					Source:    st.Path,
					Author:    "repo",
					CreatedAt: st.Updated,
				})
			}
		}
		if items, at, err := project.FollowupsAll(p.Path); err == nil && len(items) > 0 {
			out = append(out, memory.Record{
				ID:        "d:" + hash + ":FOLLOWUPS.md",
				Scope:     sc,
				Kind:      memory.Handoff,
				Text:      "- " + strings.Join(items, "\n- "),
				Source:    project.FollowupsFile,
				Author:    "repo",
				CreatedAt: at.UTC(),
			})
		}
	}
	if threads, err := thread.Open(e.store(), time.Now().UTC()); err == nil {
		for _, t := range threads {
			p := project.Project{Path: t.Project, Remote: t.Remote}
			if known, ok := byPath[t.Project]; ok && p.Remote == nil {
				p.Remote = known.Remote
			}
			kind := memory.Handoff
			if t.Kind == thread.Them {
				kind = memory.Question
			}
			out = append(out, memory.Record{
				ID:        fmt.Sprintf("n:%d", t.ID),
				Scope:     memory.ForProject(e.Cfg, p),
				Kind:      kind,
				Text:      t.Text,
				Source:    fmt.Sprintf("note #%d (%s)", t.ID, t.Kind),
				Author:    "human",
				CreatedAt: t.Since.UTC(),
			})
		}
	}
	if hist, err := session.History(e.store()); err == nil {
		for _, ent := range hist {
			var parts []string
			if ent.LastMessage != nil && strings.TrimSpace(*ent.LastMessage) != "" {
				parts = append(parts, "ended: "+*ent.LastMessage)
			}
			if ent.Prompt != nil && strings.TrimSpace(*ent.Prompt) != "" {
				parts = append(parts, "prompt: "+*ent.Prompt)
			}
			if len(ent.Files) > 0 {
				parts = append(parts, "files: "+strings.Join(ent.Files, ", "))
			}
			if len(parts) == 0 {
				continue
			}
			p := project.Project{Path: ent.Project}
			if known, ok := byPath[ent.Project]; ok {
				p.Remote = known.Remote
			}
			source := "session · " + ent.Agent
			if ent.Branch != "" {
				source += " · " + ent.Branch
			}
			id := "s:" + shortRecallHash(fmt.Sprintf("%s\x00%s\x00%s\x00%s", ent.Project, ent.SessionID, ent.Branch, ent.Ended.Format(time.RFC3339Nano)))
			out = append(out, memory.Record{
				ID:        id,
				Scope:     memory.ForProject(e.Cfg, p),
				Kind:      memory.Handoff,
				Text:      strings.Join(parts, " · "),
				Source:    source,
				Author:    ent.Agent,
				CreatedAt: ent.Ended.UTC(),
			})
		}
	}
	return out
}
