package cli

import (
	"bytes"
	"fmt"

	"github.com/bilal-/sous/internal/launcher"
)

// cmdGo: sous go <project> [-a <launcher>]. Writes the resume context to a
// temp file named by SOUS_HERE_FILE (launchers that want it read it), then
// execs the launcher in the project. A dispatcher for the human, not for work.
func cmdGo(e *Env, a argv) int {
	agent := a.value("a")
	p, code := resolveProject(e, a.pos[0])
	if code != 0 {
		return code
	}
	if a.has("where") { // for the shell wrapper: the folder, nothing started
		fmt.Fprintln(e.Stdout, p.Path)
		return 0
	}
	cfg, code := e.config()
	if code != 0 {
		return code
	}
	if agent == "" {
		agent = cfg.Agent
	}
	l, ok := launcher.Find(launcher.Launchers(e.Exe, launcher.BuiltinNames(), cfg.Plugins), agent)
	if !ok {
		return fail(e, 2, "no launcher named %s", agent)
	}
	argv0, cmdline, err := launcher.ExecArgv(l, p.Path)
	if err != nil {
		return fail(e, 1, "%v", err)
	}
	// Built-in adapters exec a binary that must exist; check before we
	// replace ourselves so the failure is a sous message, not a dead exec.
	if bin, isBuiltin := launcher.Builtins[l.Name]; isBuiltin {
		if _, err := lookPath(bin); err != nil {
			return fail(e, 1, "launcher %s: %s not found on PATH", l.Name, bin)
		}
	}
	// Resume context: shown to the human, and handed to launchers via a file.
	var here bytes.Buffer
	sub := e.child(&here, e.Stderr, true)
	defer sub.close()
	cmdHere(sub, argv{pos: []string{p.Path}})
	var env []string
	if name, err := launcher.WriteContext(e.Home, p.Path, here.Bytes()); err == nil {
		env = append(env, "SOUS_HERE_FILE="+name)
	}
	fmt.Fprintf(e.Stderr, "→ %s in %s\n", l.Name, p.Path)
	e.Stdout.Write(here.Bytes())
	return execIn(e, p.Path, argv0, cmdline, env...)
}
