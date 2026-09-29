package cli

import (
	"time"

	"github.com/bilal-/sous/internal/signal"
)

// cmdSignal is the debugging front door and how the runner re-execs built-ins:
// `sous signal <name> scan` reads paths on stdin, writes JSONL.
func cmdSignal(e *Env, a argv) int {
	args := a.pos
	if len(args) != 2 || args[1] != "scan" {
		return fail(e, 2, "usage: sous signal <name> scan  (paths on stdin)")
	}
	scan, ok := signal.Builtin(args[0], e.Cfg)
	if !ok {
		return fail(e, 2, "no built-in signal plugin: %s", args[0])
	}
	if err := scan(signal.ReadPaths(e.Stdin), e.Stdout, e.Stderr, time.Now().UTC()); err != nil {
		return 1
	}
	return 0
}
