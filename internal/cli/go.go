package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bilal-/sous/internal/launcher"
)

// writeHereFile keeps one resume file per project under SOUS_HOME/here,
// replaced whole each time, so sous go never leaves temp files behind.
func writeHereFile(home, project string, body []byte) (string, error) {
	dir := filepath.Join(home, "here")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(project))
	name := filepath.Join(dir, hex.EncodeToString(sum[:6])+".txt")
	tmp, err := os.CreateTemp(dir, ".here-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	return name, os.Rename(tmp.Name(), name)
}

// cmdGo: sous go <project> [-a <launcher>]. Writes the resume context to a
// temp file named by SOUS_HERE_FILE (launchers that want it read it), then
// execs the launcher in the project. A dispatcher for the human, not for work.
func cmdGo(e *Env, a argv) int {
	agent := a.value("a")
	p, code := resolveProject(e, a.pos[0])
	if code != 0 {
		return code
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
	if name, err := writeHereFile(e.Home, p.Path, here.Bytes()); err == nil {
		env = append(env, "SOUS_HERE_FILE="+name)
	}
	fmt.Fprintf(e.Stderr, "→ %s in %s\n", l.Name, p.Path)
	e.Stdout.Write(here.Bytes())
	return execIn(e, p.Path, argv0, cmdline, env...)
}
