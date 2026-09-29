package cli

import (
	"github.com/bilal-/sous/internal/backend"
)

// cmdBackend: `sous backend <name> <op> [args]` — the debugging front door and
// how the runner re-execs built-ins. Every built-in goes through backend.Ops,
// exactly as a third-party executable would.
func cmdBackend(e *Env, a argv) int {
	args := a.pos
	if len(args) < 2 {
		return fail(e, 2, "usage: sous backend <name> detect|file|status|close|url [args]")
	}
	impl, ok := backend.Builtin(args[0], e.Home, e.Cfg)
	if !ok {
		return fail(e, 2, "no built-in backend: %s", args[0])
	}
	op, ok := backend.Ops(impl)[args[1]]
	if !ok {
		return 2 // contract: unknown/unsupported op is exit 2
	}
	return op(args[2:], e.Stdin, e.Stdout, e.Stderr)
}
