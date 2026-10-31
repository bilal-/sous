package cli

import (
	"slices"
	"time"

	"github.com/bilal-/sous/internal/board"
	"github.com/bilal-/sous/internal/effort"
	"github.com/bilal-/sous/internal/memory"
)

func cmdEffort(e *Env, a argv) int {
	if len(a.pos) < 2 {
		if code := flagsOnly(e, a, "all", "project"); code != 0 {
			return code
		}
		if len(a.pos) == 1 {
			if slices.Contains([]string{"add", "link", "unlink", "depend", "undepend", "archive", "reopen"}, a.pos[0]) {
				return fail(e, exitUsage, "effort %s needs a name", a.pos[0])
			}
			if len(a.set) > 0 {
				return fail(e, exitUsage, "an effort detail takes only its name or id")
			}
			group, err := effort.Get(e.store(), a.pos[0])
			if err != nil {
				return memoryErr(e, err)
			}
			view := board.DescribeEffort(e.store(), group, time.Now())
			if e.JSON {
				return e.writeJSON(view)
			}
			board.RenderEffort(e.Stdout, view)
			return 0
		}
		groups, err := effort.All(e.store())
		if err != nil {
			return memoryErr(e, err)
		}
		var scopes []memory.Scope
		for _, name := range a.values("project") {
			_, scope, code := resolveMemoryProject(e, name)
			if code != 0 {
				return code
			}
			scopes = append(scopes, scope)
		}
		selected := []effort.Effort{}
		for _, group := range groups {
			if group.ArchivedAt != nil && !a.has("all") || len(scopes) > 0 && !slices.ContainsFunc(scopes, group.Includes) {
				continue
			}
			selected = append(selected, group)
		}
		views := []board.EffortSummary{}
		for _, view := range board.DescribeEfforts(e.store(), selected, time.Now()) {
			views = append(views, view.EffortSummary)
		}
		if e.JSON {
			return e.writeJSON(views)
		}
		board.RenderEfforts(e.Stdout, views)
		return 0
	}
	action, name := a.pos[0], a.pos[1]
	did := ""
	var err error
	switch action {
	case "add":
		if code := flagsOnly(e, a); code != 0 {
			return code
		}
		group, already, createErr := effort.Create(e.store(), name, time.Now())
		if createErr != nil {
			return memoryErr(e, createErr)
		}
		name = group.ID
		did = didEffortCreated
		if already {
			did = didAlreadyCreated
		}
	case "link", "unlink":
		if code := flagsOnly(e, a, "project", "note", "memory", "ref"); code != 0 {
			return code
		}
		changes := effort.Changes{Memories: a.values("memory"), Refs: a.values("ref")}
		for _, term := range a.values("project") {
			p, _, code := resolveMemoryProject(e, term)
			if code != 0 {
				return code
			}
			changes.Projects = append(changes.Projects, p)
		}
		for _, value := range a.values("note") {
			id, code := threadID(e, value)
			if code != 0 {
				return code
			}
			changes.Notes = append(changes.Notes, id)
		}
		err = effort.Link(e.store(), e.Cfg, name, changes, action == "unlink")
		did = didLinked
		if action == "unlink" {
			did = didUnlinked
		}
	case "depend", "undepend":
		if code := flagsOnly(e, a, "note", "after"); code != 0 {
			return code
		}
		if len(a.values("note")) != 1 || len(a.values("after")) != 1 {
			return fail(e, exitUsage, "dependency needs one --note <n> and --after <n>")
		}
		n, code := threadID(e, a.value("note"))
		if code != 0 {
			return code
		}
		after, code := threadID(e, a.value("after"))
		if code != 0 {
			return code
		}
		err = effort.Depend(e.store(), name, n, after, action == "undepend")
		did = didLinked
		if action == "undepend" {
			did = didUnlinked
		}
	case "archive", "reopen":
		if code := flagsOnly(e, a); code != 0 {
			return code
		}
		err = effort.Archive(e.store(), name, action == "archive", time.Now())
		did = didArchived
		if action == "reopen" {
			did = didReopened
		}
	default:
		return fail(e, exitUsage, "effort operation must be add, link, unlink, depend, undepend, archive or reopen")
	}
	if err != nil {
		return memoryErr(e, err)
	}
	group, err := effort.Get(e.store(), name)
	if err != nil {
		return memoryErr(e, err)
	}
	return e.changed(changedJSON{ID: group.ID, Did: did, Next: []string{"sous effort " + group.ID}}, group.Name+" · "+did)
}
