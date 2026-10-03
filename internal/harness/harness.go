// Package harness is every agent tool sous works with (Claude Code, Codex,
// ...), one file each: where its skills go, which hooks sous puts in its
// settings and how that file is laid out, what its hooks send, how to read
// its transcript, and how to run it headless for a run. install, hook,
// launcher, runner and doctor read this table; adding an agent is one file
// here and a line in All.
package harness

import (
	"os"
	"path/filepath"
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
	// Flag, when set, makes the hook optional: sous setup --<Flag> puts it
	// in, for agent versions that support the event.
	Flag string
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

// Flags are the setup flags that turn on optional hooks.
func Flags() []string {
	var out []string
	for _, h := range All {
		for _, k := range h.Hooks {
			if k.Flag != "" && !slices.Contains(out, k.Flag) {
				out = append(out, k.Flag)
			}
		}
	}
	return out
}

// SharedSkills are skill folders several agents read besides their own,
// for agents sous has no hooks for. When, if set, says whether the folder
// applies on this machine.
var SharedSkills = []SkillFolder{
	{Who: "Gemini CLI, Kimi, Cursor and other agents", Dir: func(home string) string { return filepath.Join(home, ".agents", "skills") },
		Says: "Gemini CLI, Kimi, Cursor and other agents: have the sous skill (in ~/.agents/skills, the shared folder they read)"},
	{Who: "Antigravity", Dir: func(home string) string { return filepath.Join(home, ".gemini", "antigravity", "skills") },
		When: func(home string) bool {
			st, err := os.Stat(filepath.Join(home, ".gemini", "antigravity"))
			return err == nil && st.IsDir()
		},
		Says: "Antigravity: has the sous skill"},
}

// SkillFolder is a folder agents read skills from.
type SkillFolder struct {
	Who  string
	Dir  func(home string) string
	When func(home string) bool // nil: always
	Says string                 // what setup reports once the skill is there
}
