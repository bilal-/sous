package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/board"
	"github.com/bilal-/sous/internal/effort"
	"github.com/bilal-/sous/internal/memory"
	"github.com/bilal-/sous/internal/text"
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
		r, err := memory.Get(e.store(), id)
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
		if r.VerifiedAt != nil {
			fmt.Fprintf(e.Stdout, "verified · %s · %s\n", r.VerifiedAt.Format(time.RFC3339), r.VerificationSource)
		} else {
			fmt.Fprintln(e.Stdout, "unverified · recorded context, not proof of completed work")
		}
		for _, target := range r.SharedWith {
			fmt.Fprintf(e.Stdout, "shared with · %s · %s\n", target.Workspace, target.Project)
		}
		flag, value, label := memory.HistoryTarget(e.Cfg, r.Scope)
		fmt.Fprintf(e.Stdout, "sous recall %s %s --history for %s\n", flag, text.ShellArg(value), label)
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
		_, scope, code := resolveMemoryProject(e, a.value("p"))
		if code != 0 {
			return code
		}
		q.Scope = scope
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
