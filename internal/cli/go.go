package cli

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/bilal-/sous/internal/launcher"
	"github.com/bilal-/sous/internal/plugin"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/session"
	"io"
)

// hereContext is where the person left off in p, short, as an agent is
// handed it, and the file it is saved in ("" when it could not be).
func hereContext(e *Env, p project.Project, stderr io.Writer) ([]byte, string) {
	var here bytes.Buffer
	sub := e.child(&here, stderr, true)
	defer sub.close()
	cmdHere(sub, argv{pos: []string{p.Path}})
	file, err := session.WriteContext(e.Home, p.Path, here.Bytes())
	if err != nil {
		file = ""
	}
	return here.Bytes(), file
}

// installHint follows "not set up": where the person learns what is missing.
const installHint = "sous doctor says what to install"

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
		return fail(e, exitUsage, "go takes --json only with --run")
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
		return fail(e, exitUsage, "%v", err)
	case errors.Is(err, plugin.ErrNotSetUp):
		return fail(e, exitNotReady, "%v (%s)", err, installHint)
	case err != nil:
		return fail(e, exitFailed, "%v", err)
	}
	// Resume context: shown to the human, and handed to launchers via a file.
	here, file := hereContext(e, p, e.Stderr)
	var env []string
	if file != "" {
		env = append(env, "SOUS_HERE_FILE="+file)
	}
	fmt.Fprintf(e.Stderr, "→ %s in %s\n", agent, p.Path)
	e.Stdout.Write(here)
	if err := execIn(p.Path, argv0, cmdline, env...); err != nil {
		return fail(e, exitFailed, "starting %s: %v", agent, err)
	}
	return 0
}
