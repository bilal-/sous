package runner

import (
	"fmt"
	"io"
	"time"
)

// Door is `sous runner <name> <call> [args]`: the contract's calls through
// Ops, plus watch, the built ins' own watcher (not part of the contract).
func Door(home, exe string, limit time.Duration, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) < 2 {
		fmt.Fprintln(stderr, "usage: sous runner <name> start|status|reply|stop|clean [args]")
		return 2
	}
	a, ok := Builtin(args[0], home, exe, limit)
	if !ok {
		fmt.Fprintf(stderr, "no built in runner: %s\n", args[0])
		return 2
	}
	if args[1] == "watch" && len(args) >= 3 {
		if err := a.Watch(args[2], len(args) == 4 && args[3] == "--resume"); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	op, ok := Ops(a)[args[1]]
	if !ok {
		return 2
	}
	return op(args[2:], stdin, stdout, stderr)
}
