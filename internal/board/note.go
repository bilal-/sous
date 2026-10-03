package board

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/thread"
)

// Next is the commands that make sense for a note in its state.
func Next(v thread.View) []string {
	if v.Closed != nil {
		return []string{}
	}
	id := v.ID
	if v.Run == nil {
		return []string{fmt.Sprintf("sous done %d", id)}
	}
	switch v.Run.State {
	case "needs_you":
		return []string{fmt.Sprintf(`sous reply %d "<answer>"`, id), fmt.Sprintf("sous done %d", id)}
	case "done", "failed":
		return []string{fmt.Sprintf("sous done %d --clean", id)}
	}
	return []string{fmt.Sprintf("sous show %d", id), fmt.Sprintf("sous done %d", id)}
}

// RenderNote is sous show: one note in full, and for a run how it is
// going, with what to do next. home shortens paths for reading.
func RenderNote(w io.Writer, v thread.View, now time.Time, home string) {
	state := string(v.Kind) + " · " + project.Ago(now, v.Since)
	if v.Closed != nil {
		state = "closed " + project.Ago(now, *v.Closed)
	}
	fmt.Fprintf(w, "%d  %s  %s  (%s)\n", v.ID, project.Describe(v.Project).Name, v.Text, state)
	if v.Ref != nil {
		fmt.Fprintf(w, "   filed: %s\n", *v.Ref)
	}
	if r := v.Run; r != nil {
		line := "   run: " + strings.ReplaceAll(r.State, "_", " ")
		if r.Text != "" {
			line += " · " + r.Text
		}
		fmt.Fprintf(w, "%s (%s)\n", line, r.Runner)
		if v.RunErr != "" {
			fmt.Fprintf(w, "   status unavailable: %s\n", v.RunErr)
		}
		var where []string
		for _, kv := range [][2]string{{"branch", r.Branch}, {"worktree", r.Worktree}, {"log", r.Log}} {
			if kv[1] != "" {
				where = append(where, kv[0]+" "+config.Tilde(home, kv[1]))
			}
		}
		if len(where) > 0 {
			fmt.Fprintf(w, "   %s\n", strings.Join(where, " · "))
		}
	}
	if next := Next(v); len(next) > 0 {
		fmt.Fprintf(w, "   next: %s\n", strings.Join(next, " · "))
	}
}

// RunsWaiting lists the runs, in every project, that are done, need the
// user, or failed: what an agent should hear about first when a session
// starts. "" when there are none.
func RunsWaiting(views []thread.View) string {
	var b strings.Builder
	for _, v := range views {
		if v.Run == nil {
			continue
		}
		name := project.Describe(v.Project).Name
		switch v.Run.State {
		case "needs_you":
			fmt.Fprintf(&b, "  %d %s: needs you · %s · answer with: sous reply %d \"<answer>\"\n", v.ID, name, v.Run.Text, v.ID)
		case "done":
			fmt.Fprintf(&b, "  %d %s: done, review it%s · then: sous done %d\n", v.ID, name, Suffix(v.Run.Branch), v.ID)
		case "failed":
			fmt.Fprintf(&b, "  %d %s: failed%s · sous show %d\n", v.ID, name, Suffix(v.Run.Text), v.ID)
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "[sous] Runs waiting on the user:\n" + b.String()
}
