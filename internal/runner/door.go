package runner

import (
	"io"
	"time"

	"github.com/bilal-/sous/internal/harness"
	"github.com/bilal-/sous/internal/plugin"
)

// Deps is what every built in runner is made from.
type Deps struct {
	Home  string // SOUS_HOME: runs live in Home/runs/<uid>
	Exe   string // this sous, which runs the watcher
	Limit time.Duration
}

// Registry is the built in runners, one per harness that runs headless.
// They are offline: they read files on this machine.
var Registry = plugin.Registry[Deps]{Axis: plugin.AxisRunner}

func init() {
	for _, h := range harness.All {
		if h.Headless == nil {
			continue
		}
		Registry.Builtins = append(Registry.Builtins, plugin.Builtin[Deps]{Name: h.Name, Offline: true, Ops: func(d Deps) map[string]plugin.Op {
			a, _ := Builtin(h.Name, d.Home, d.Exe, d.Limit)
			ops := ops(a)
			ops["watch"] = watchOp(a)
			ops["own"] = ownerOp(a, false)
			ops["anchor"] = ownerOp(a, true)
			return ops
		}})
	}
}

// watchOp is `watch <uid> [--resume]`, the built ins' own watcher (not part
// of the contract). The run's lock arrives as fd 3 (launch hands it over).
// The agent inherits it too, so the run reads as working while either lives.
func watchOp(a *Agent) plugin.Op {
	return func(args []string, _ io.Reader, _, stderr io.Writer) int {
		if (len(args) != 3 && len(args) != 4) || args[1] != "--lock-fd" || args[2] != "3" || len(args) == 4 && args[3] != "--resume" {
			return plugin.Usage(stderr, "watch <dir> --lock-fd 3 [--resume]")
		}
		lock, err := ownedLock(args[0])
		if err != nil {
			return plugin.Exit(err, stderr)
		}
		defer lock.Close()
		return plugin.Exit(a.watch(args[0], len(args) == 4, lock), stderr)
	}
}

func ownerOp(a *Agent, isAnchor bool) plugin.Op {
	return func(args []string, stdin io.Reader, _, stderr io.Writer) int {
		if len(args) != 0 {
			return plugin.Usage(stderr, "private runner owner takes its request on stdin")
		}
		if isAnchor {
			return plugin.Exit(anchor(stdin), stderr)
		}
		return plugin.Exit(a.own(stdin), stderr)
	}
}
