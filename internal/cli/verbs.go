package cli

import "strings"

// verb is one entry in the closed verb set. The table is the single source
// of truth for dispatch, for which verbs accept the read-side flags, and for
// the usage text — so a new verb (say, digest) is one line here and its
// handler, nothing else.
type verb struct {
	name  string
	run   func(e *Env, a argv) int
	read  bool     // accepts --json / --brief
	args  *argSpec // nil: an internal door, given its arguments raw
	usage string   // one line for `sous help`; "" hides it (internal doors)
}

// synopsis is the usage line up to its description.
func (v verb) synopsis() string {
	syn, _, _ := strings.Cut(v.usage, "  ")
	return syn
}

// exactly n positionals, plus the given flags.
func exactly(n int, bools, values []string) *argSpec {
	return &argSpec{bools: bools, values: values, min: n, max: n}
}

// verbs is filled in init to break the usage → verbs → cmdHelp → usage cycle.
var verbs []verb

func init() {
	verbs = []verb{
		{"here", cmdHere, true, &argSpec{max: 1}, "sous here [path]  where was I, for the current project"},
		{"report", cmdReport, true, exactly(0, []string{"week", "open"}, nil), "sous report [--week] [--open]   what changed since the last report; --open shows the page"},
		{"projects", cmdProjects, true, &argSpec{values: []string{"root", "path"}, max: 1}, "sous projects [term] [--root <dir>] [--path <term>]   every discovered project; --path prints one path"},
		{"note", cmdNote, false, exactly(1, []string{"file"}, []string{"p", "k"}), `sous note [-p <project>] [-k me|them|idea] [--file] "<text>"`},
		{"edit", cmdEdit, false, &argSpec{min: 2, max: 2, raw: true}, `sous edit <n> "<text>"`},
		{"kind", cmdKind, false, &argSpec{min: 2, max: 2, raw: true}, "sous kind <n> me|them|idea"},
		{"snooze", cmdSnooze, false, &argSpec{min: 1, max: 2}, "sous snooze <n|s:id> [days]"},
		{"done", cmdDone, false, exactly(1, []string{"close"}, nil), "sous done <n> [--close]"},
		{"file", cmdFile, false, exactly(1, []string{"force"}, nil), "sous file <n> [--force]    file a note in the project's tracker"},
		{"go", cmdGo, false, exactly(1, []string{"where"}, []string{"a|agent", "in"}), "sous go <project> [-a <launcher>] [--where] [--in <folder>]   start your agent in that project; --where only prints the folder"},
		{"setup", cmdSetup, false, &argSpec{bools: []string{"codex-session-end", "print-skill", "no-shell"}, max: -1}, "sous setup [folder...] [--no-shell] [--print-skill] [--codex-session-end]   set everything up; folders say where your projects are"},
		{"version", cmdVersion, false, exactly(0, nil, nil), "sous version"},
		{"help", cmdHelp, false, exactly(0, nil, nil), "sous help"},
		// Internal doors: how the runner re-execs built-ins, and hooks. Not in help.
		{"signal", cmdSignal, false, nil, ""},
		{"backend", cmdBackend, false, nil, ""},
		{"launcher", cmdLauncher, false, nil, ""},
		{"hook", cmdHook, false, nil, ""},
	}
}

func findVerb(name string) (verb, bool) {
	for _, v := range verbs {
		if v.name == name {
			return v, true
		}
	}
	return verb{}, false
}
