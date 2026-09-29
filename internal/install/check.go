package install

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/hook"
)

// Result is one thing setup put in place, and whether it is still there.
type Result struct {
	Name   string // "Claude Code hook (session start)"
	OK     bool
	Detail string // what was found
	Fix    string // the command that puts it right; "" when OK
}

// Check looks at everything setup installs, for the sous at exe, without
// changing anything: each agent hook, each skill folder, and the shell line.
func Check(home, sousHome, exe, shell, goos, zdotdir string) []Result {
	var out []Result
	for _, h := range hookSpecs(home, false) {
		out = append(out, checkHook(h, exe))
	}
	for _, p := range skillPlaces(home) {
		out = append(out, checkSkill(home, p))
	}
	return append(out, checkShell(home, sousHome, shell, goos, zdotdir))
}

func checkHook(h hookSpec, exe string) Result {
	moment := "session start"
	if h.Role == hook.RoleEnd {
		moment = "session end"
	}
	r := Result{Name: h.Agent + " hook (" + moment + ")", Fix: "sous setup"}
	b, err := os.ReadFile(h.File)
	if err != nil {
		r.Detail = "not installed"
		return r
	}
	var doc struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if json.Unmarshal(b, &doc) != nil {
		r.Detail, r.Fix = h.File+" is not valid JSON", "fix "+h.File+" by hand, then sous setup"
		return r
	}
	want := hook.Command(exe, h.Role, h.name)
	r.Detail = "not installed"
	for _, g := range doc.Hooks[h.Event] {
		for _, c := range g.Hooks {
			switch {
			case c.Command == want:
				return Result{Name: r.Name, OK: true, Detail: "installed"}
			case hook.IsOurs(c.Command, h.Role, h.name):
				r.Detail = "runs another sous: " + strings.TrimSuffix(c.Command, " hook "+h.Role+" "+h.name)
			}
		}
	}
	return r
}

func checkSkill(home string, p skillPlace) Result {
	r := Result{Name: "skill for " + p.Who, Fix: "sous setup"}
	b, err := os.ReadFile(p.file())
	switch {
	case err != nil:
		r.Detail = "not installed"
	case !bytes.Equal(b, []byte(Skill)):
		r.Detail = "out of date"
	default:
		return Result{Name: r.Name, OK: true, Detail: "installed, " + config.Tilde(home, p.file())}
	}
	return r
}

func checkShell(home, sousHome, shell, goos, zdotdir string) Result {
	files, line, ok := shellTarget(home, sousHome, shell, goos, zdotdir)
	r := Result{Name: "shell (" + shell + ")"}
	if !ok {
		r.OK, r.Detail = true, fmt.Sprintf("%s is not a shell sous sets up; run sous --ambient from its startup file to see the board in new shells", shell)
		return r
	}
	for _, f := range files {
		b, _ := os.ReadFile(f)
		found := false
		for _, l := range strings.Split(string(b), "\n") {
			if strings.TrimSpace(l) == line || isSousLine(l) {
				found = true
			}
		}
		if !found {
			r.Detail, r.Fix = "new shells do not show the board ("+config.Tilde(home, f)+" has no sous line)", "sous setup"
			return r
		}
	}
	r.OK, r.Detail = true, "new shells show the board"
	return r
}
