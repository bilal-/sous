// Package harness is every agent tool sous works with (Claude Code, Codex,
// ...), one file each: where its skills go, which hooks sous puts in its
// settings and how that file is laid out, what its hooks send, how to read
// its transcript, and how to run it headless for a run. install, hook,
// launcher, runner and doctor read this table; adding an agent is one file
// here and a line in All.
package harness

import (
	"slices"
)

// Harness is one agent tool.
type Harness struct {
	Name    string // the hook's agent argument, and the launcher and runner name: "claude"
	Display string // how people know it: "Claude Code"
	Bin     string // its program on PATH
	// SkillDir is the folder it reads skills from.
	SkillDir func(home string) string
	// HookFile is the settings file its hooks live in; Format is how that
	// file is laid out.
	HookFile func(home string) string
	Format   Format
	Hooks    []Hook
	// Parse reads what its hook sends on standard input. It never fails:
	// what it cannot read is left empty.
	Parse func(stdin []byte) Input
	// Last is the last thing it said in a transcript, at most max runes;
	// "" when there is none.
	Last func(transcript string, max int) string
	// Headless runs it on a task with nobody watching; nil when it cannot.
	Headless *Headless
}

// Hook is one hook sous puts in a harness's settings.
type Hook struct {
	Role  string // RoleStart or RoleEnd
	Event string // the harness's name for the moment: "SessionStart"
	// Optional: installed only when the person asks (sous setup
	// --codex-session-end), so its absence is not a fault.
	Optional bool
}

// Input is what a hook call says, whatever shape the harness sent it in.
type Input struct {
	SessionID      string
	CWD            string // "" when it sent none: the hook's own folder is used
	TranscriptPath string
	// Fresh: a new session, not one resumed, cleared or compacted, so what
	// sous says at its start is news.
	Fresh bool
}

// Run is what a headless start or resume needs to know.
type Run struct {
	Prompt   string // the brief, with sous's preamble
	Session  string // the session to resume
	Answer   string // the person's reply, passed on when resuming
	Worktree string // where it works
	Dir      string // the run's own folder, beside the worktree
	GitDirs  []string
}

// Headless is how to run a harness on a task and read back what happened.
type Headless struct {
	Start   func(r Run) []string // arguments for a new run
	Resume  func(r Run) []string // arguments to carry on after a reply
	Session func(log []byte) string
	// Last is its final message, from what it printed or a file it wrote.
	Last func(r Run, log []byte) string
}

// All is every harness sous knows, in the order setup reports them.
var All = []Harness{claude, codex}

// Find is the harness called name.
func Find(name string) (Harness, bool) {
	i := slices.IndexFunc(All, func(h Harness) bool { return h.Name == name })
	if i < 0 {
		return Harness{}, false
	}
	return All[i], true
}

// Names lists every harness by name, sorted.
func Names() []string {
	names := make([]string, len(All))
	for i, h := range All {
		names[i] = h.Name
	}
	slices.Sort(names)
	return names
}
