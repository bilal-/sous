package board

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/text"
	"github.com/bilal-/sous/internal/thread"
)

// runLook is how a run in one state reads, and what to do about it. One
// table, so the board, show, --json and the session start all agree.
type runLook struct {
	label  string                     // what the board says first: "run needs you"
	detail func(r *thread.Run) string // what follows the note: its question, its branch
	next   []string                   // commands, with {n} for the note's number and {project} its org/name
}

var runLooks = map[thread.RunState]runLook{
	thread.RunStarting: {label: "run starting", next: []string{"sous show {n}", "sous done {n}"}},
	thread.RunRunning:  {label: "running", next: []string{"sous show {n}", "sous done {n}"}},
	thread.RunNeedsYou: {label: "run needs you", detail: func(r *thread.Run) string { return fmt.Sprintf(" · %q", r.Text) },
		next: []string{`sous reply {n} "<answer>"`, "sous done {n}"}},
	thread.RunDone: {label: "run done, review it", detail: func(r *thread.Run) string { return text.Suffix(r.Branch) },
		next: []string{"sous done {n} --clean"}},
	thread.RunFailed: {label: "run failed", detail: func(r *thread.Run) string { return text.Suffix(r.Text) },
		next: []string{"sous show {n}", "sous go {project} --run - --key retry-{n}", "sous done {n} --clean"}},
}

// look is r's entry; a state sous does not know reads like running.
func look(r *thread.Run) runLook {
	if l, ok := runLooks[r.State]; ok {
		return l
	}
	return runLooks[thread.RunRunning]
}

// runLine is how a run reads on the board: its state, the note, and what
// the runner said.
func runLine(r *thread.Run, note string) string {
	l := look(r)
	line := l.label + " · " + note
	if l.detail != nil {
		line += l.detail(r)
	}
	return line
}

// Next is the commands that make sense for a note in its state.
func Next(v thread.View) []string {
	switch {
	case v.Closed != nil:
		return []string{"sous"} // it is done: back to what else waits
	case v.Run == nil:
		return []string{fmt.Sprintf("sous done %d", v.ID)}
	}
	// org/name: a folder name alone may match more than one project.
	fill := strings.NewReplacer("{n}", fmt.Sprint(v.ID), "{project}", project.OrgName(v.Project))
	var out []string
	for _, c := range look(v.Run).next {
		out = append(out, fill.Replace(c))
	}
	return out
}

// RenderNote is sous show: one note in full, and for a run how it is
// going, with what to do next. home shortens paths for reading.
func RenderNote(w io.Writer, v thread.View, now time.Time, home string) {
	state := ThreadRow(v, now).Kind + " · " + text.Ago(now, v.Since)
	if v.Closed != nil {
		state = "closed " + text.Ago(now, *v.Closed)
	}
	fmt.Fprintf(w, "%d  %s  %s  (%s)\n", v.ID, project.Describe(v.Project).Name, v.Text, state)
	if v.Ref != nil {
		fmt.Fprintf(w, "   filed: %s\n", *v.Ref)
	}
	if r := v.Run; r != nil {
		line := "   " + look(r).label
		if r.Text != "" {
			line += " · " + r.Text
		}
		fmt.Fprintf(w, "%s (%s)\n", line, r.Runner)
		if v.RunErr != "" {
			fmt.Fprintf(w, "   status unavailable: %s\n", v.RunErr)
		}
		for _, l := range v.LogTail {
			fmt.Fprintf(w, "   | %s\n", l)
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

// RunsWaiting lists the runs, in every project, that wait on the user
// (done, needs them, failed) and are not snoozed: what an agent should hear
// about first when a session starts. "" when there are none.
func RunsWaiting(views []thread.View) string {
	var b strings.Builder
	for _, v := range views {
		if v.Run == nil || v.Run.State.Working() || v.Snoozed {
			continue
		}
		name := filepath.Base(v.Project) // no git: this runs while a session starts
		// One line each: sous show tells the rest, and what to do next.
		fmt.Fprintln(&b, text.Fit(fmt.Sprintf("  %d %s: ", v.ID, name), runLine(v.Run, v.Text), " · "+Next(v)[0]))
	}
	if b.Len() == 0 {
		return ""
	}
	return "[sous] Runs waiting on the user:\n" + b.String()
}
