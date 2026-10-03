package cli

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/bilal-/sous/internal/launcher"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/session"
)

// cmdGo: sous go <project> [-a <launcher>]. Writes the resume context to a
// temp file named by SOUS_HERE_FILE (launchers that want it read it), then
// execs the launcher in the project. A dispatcher for the human, not for work.
func cmdGo(e *Env, a argv) int {
	agent := a.value("a")
	var p project.Project
	if in := a.value("in"); in != "" { // already found (the shell wrapper)
		p = project.Describe(in)
	} else {
		var code int
		if p, code = resolveProject(e, a.pos[0]); code != 0 {
			return code
		}
	}
	if a.has("run") {
		cfg, code := e.config()
		if code != 0 {
			return code
		}
		return goRun(e, a, p, cfg)
	}
	if e.JSON {
		return fail(e, 2, "go takes --json only with --run")
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
	argv0, cmdline, err := launcher.Prepare(e.Exe, cfg.Plugins, agent, p.Path)
	switch {
	case errors.Is(err, launcher.ErrUnknown):
		return fail(e, 2, "%v", err)
	case err != nil:
		return fail(e, 1, "%v", err)
	}
	// Resume context: shown to the human, and handed to launchers via a file.
	var here bytes.Buffer
	sub := e.child(&here, e.Stderr, true)
	defer sub.close()
	cmdHere(sub, argv{pos: []string{p.Path}})
	var env []string
	if name, err := session.WriteContext(e.Home, p.Path, here.Bytes()); err == nil {
		env = append(env, "SOUS_HERE_FILE="+name)
	}
	fmt.Fprintf(e.Stderr, "→ %s in %s\n", agent, p.Path)
	e.Stdout.Write(here.Bytes())
	if err := execIn(p.Path, argv0, cmdline, env...); err != nil {
		return fail(e, 1, "starting %s: %v", agent, err)
	}
	return 0
}
