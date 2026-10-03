package cli

import (
	"strings"

	"github.com/bilal-/sous/internal/harness"
)

// verb is one entry in the closed verb set. The table is the single source
// of truth for dispatch, for which verbs accept the read-side flags, and for
// the usage text — so a new verb (say, digest) is one line here and its
// handler, nothing else.
type verb struct {
	name  string
	run   func(e *Env, a argv) int
	json  bool     // accepts --json: it shows something, or says what it changed
	brief bool     // accepts --brief
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
		{"here", cmdHere, true, true, &argSpec{max: 1}, "sous here [path]  where was I, for the current project"},
		{"report", cmdReport, true, false, exactly(0, []string{"week", "open"}, nil), "sous report [--week] [--open]   what changed since the last report; --open shows the page"},
		{"projects", cmdProjects, true, false, &argSpec{values: []string{"root", "path"}, max: 1}, "sous projects [term] [--root <dir>] [--path <term>]   every discovered project; --path prints one path"},
		{"note", cmdNote, true, false, exactly(1, []string{"file"}, []string{"p", "k"}), `sous note [-p <project>] [-k me|them|idea] [--file] "<text>"`},
		{"edit", cmdEdit, true, false, &argSpec{min: 2, max: 2, raw: true}, `sous edit <n> "<text>"`},
		{"kind", cmdKind, true, false, &argSpec{min: 2, max: 2, raw: true}, "sous kind <n> me|them|idea"},
		{"snooze", cmdSnooze, true, false, &argSpec{min: 1, max: 2}, "sous snooze <n|s:id> [days]"},
		{"done", cmdDone, true, false, exactly(1, []string{"close", "clean"}, nil), "sous done <n> [--close] [--clean]   close a note; --close in its tracker too, --clean removes a run's worktree"},
		{"file", cmdFile, true, false, exactly(1, []string{"force"}, nil), "sous file <n> [--force]    file a note in the project's tracker"},
		{"config", cmdConfig, true, false, &argSpec{bools: []string{"unset", "add", "remove"}, values: []string{"p"}, max: -1}, "sous config [<key> <value...>] [-p <project>] [--unset] [--add] [--remove]   show or change settings; -p for one project or org/*"},
		{"go", cmdGo, true, false, exactly(1, []string{"where"}, []string{"a|agent", "in", "run", "key"}), "sous go <project> [-a <agent>] [--run <brief|->] [--key <text>] [--where] [--in <folder>]   start your agent there; --run hands it a task in the background"},
		{"show", cmdShow, true, false, exactly(1, nil, nil), "sous show <n>     one note in full; for a run, how it is going"},
		{"reply", cmdReply, true, false, &argSpec{min: 2, max: 2, raw: true}, `sous reply <n> "<answer>"   answer a run that needs you; it carries on`},
		{"setup", cmdSetup, false, false, &argSpec{bools: append([]string{"print-skill", "no-shell"}, harness.Flags()...), max: -1}, "sous setup [folder...] [--no-shell] [--print-skill]" + setupFlags() + "   set everything up; folders say where your projects are"},
		{"doctor", cmdDoctor, true, false, exactly(0, nil, nil), "sous doctor       check that sous is set up and working, and how to fix what is not"},
		{"version", cmdVersion, false, false, exactly(0, nil, nil), "sous version"},
		{"help", cmdHelp, false, false, exactly(0, nil, nil), "sous help"},
		// Internal doors: how the runner re-execs built-ins, and hooks. Not in help.
		{"signal", cmdSignal, false, false, nil, ""},
		{"backend", cmdBackend, false, false, nil, ""},
		{"launcher", cmdLauncher, false, false, nil, ""},
		{"runner", cmdRunner, false, false, nil, ""},
		{"hook", cmdHook, false, false, nil, ""},
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

// setupFlags are the optional hooks' flags as setup's usage shows them.
func setupFlags() string {
	var b strings.Builder
	for _, f := range harness.Flags() {
		b.WriteString(" [--" + f + "]")
	}
	return b.String()
}
