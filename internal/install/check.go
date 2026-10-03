package install

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/harness"
)

// Result is one thing setup put in place, and whether it is still there.
type Result struct {
	Name   string // "Claude Code hook (session start)"
	OK     bool
	Detail string // what was found
	Fix    string // the command that puts it right; "" when OK
	// Optional: a part a person may have turned down (sous setup
	// --no-shell, or no --codex-session-end), so missing is not broken.
	Optional bool
}

// Check looks at everything setup installs, for the sous at exe, without
// changing anything: each agent hook, each skill folder, and the shell line.
func Check(home, sousHome, exe, shell, goos, zdotdir string) []Result {
	var out []Result
	for _, h := range hookSpecs() {
		r := checkHook(home, h, exe)
		if h.Flag != "" && r.Detail == "not installed" { // asked for with a flag
			r = Result{Name: r.Name, OK: true, Detail: "not installed (optional)"}
		}
		out = append(out, r)
	}
	for _, p := range skillPlaces(home) {
		out = append(out, checkSkill(home, p))
	}
	return append(out, checkShell(home, sousHome, exe, shell, goos, zdotdir))
}

func checkHook(home string, h hookSpec, exe string) Result {
	moment := "session start"
	if h.Role == harness.RoleEnd {
		moment = "session end"
	}
	r := Result{Name: h.Display + " hook (" + moment + ")", Fix: "sous setup"}
	file := h.file(home)
	cmds, err := h.Format.Commands(file, h.Event)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		r.Detail = "not installed"
		return r
	case err != nil:
		r.Detail, r.Fix = err.Error(), "fix "+file+" by hand, then sous setup"
		return r
	}
	want := harness.Cmd{Exe: exe, Role: h.Role, Agent: h.Name}
	r.Detail = "not installed"
	for _, c := range cmds {
		switch {
		case c == want.String():
			return Result{Name: r.Name, OK: true, Detail: "installed"}
		case harness.Cmd{Role: h.Role, Agent: h.Name}.Ours(c):
			r.Detail = "runs another sous: " + harness.Program(c)
		}
	}
	return r
}

func checkSkill(home string, p harness.SkillFolder) Result {
	r := Result{Name: "skill for " + p.Who, Fix: "sous setup"}
	b, err := os.ReadFile(skillFile(home, p))
	switch {
	case err != nil:
		r.Detail = "not installed"
	case !bytes.Equal(b, []byte(Skill)):
		r.Detail = "out of date"
	default:
		return Result{Name: r.Name, OK: true, Detail: "installed, " + config.Tilde(home, skillFile(home, p))}
	}
	return r
}

// hasLine: whether text has line, and whether it has an older sous line
// instead (which setup would replace).
func hasLine(text, line string) (has, older bool) {
	for _, l := range strings.Split(text, "\n") {
		if strings.TrimSpace(l) == line {
			return true, false
		}
		older = older || isSousLine(l)
	}
	return false, older
}

func checkShell(home, sousHome, exe, shell, goos, zdotdir string) Result {
	files, line, ok := shellTarget(home, sousHome, shell, goos, zdotdir)
	r := Result{Name: "shell (" + shell + ")"}
	if shell == "" {
		r.Name, r.OK, r.Detail = "shell", true, "$SHELL is not set, so there is no startup file to look at"
		return r
	}
	if !ok {
		r.OK, r.Detail = true, fmt.Sprintf("%s is not a shell sous sets up; run sous --ambient from its startup file to see the board in new shells", shell)
		return r
	}
	for _, f := range files {
		b, _ := os.ReadFile(f)
		switch has, older := hasLine(string(b), line); {
		case older:
			r.Detail, r.Fix = config.Tilde(home, f)+" has an older sous line", "sous setup"
			return r
		case !has:
			r.Detail, r.Fix = "new shells do not show the board ("+config.Tilde(home, f)+" has no sous line)", "sous setup"
			r.Optional = true
			return r
		}
	}
	if shell == "zsh" {
		if _, err := os.Stat(filepath.Join(sousHome, "sous.zsh")); err != nil {
			r.Detail, r.Fix = config.Tilde(home, filepath.Join(sousHome, "sous.zsh"))+" is missing, so the line does nothing", "sous setup"
			return r
		}
	}
	// The line runs the sous it finds on PATH.
	if _, err := exec.LookPath("sous"); err != nil {
		r.Detail, r.Fix = "sous is not on your PATH, so the line does nothing", "add "+filepath.Dir(exe)+" to PATH"
		return r
	}
	r.OK, r.Detail = true, "new shells show the board"
	return r
}
