package runner

import (
	"fmt"
	"io"
	"slices"
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
var Registry = plugin.Registry[Deps]{Axis: "runner"}

func init() {
	for _, h := range harness.All {
		if h.Headless == nil {
			continue
		}
		Registry.Builtins = append(Registry.Builtins, plugin.Builtin[Deps]{Name: h.Name, Offline: true, Ops: func(d Deps) map[string]plugin.Op {
			a, _ := Builtin(h.Name, d.Home, d.Exe, d.Limit)
			ops := Ops(a)
			ops["watch"] = watchOp(a)
			return ops
		}})
	}
}

// watchOp is `watch <uid> [--resume]`, the built ins' own watcher (not part
// of the contract). The run's lock arrives as fd 3 (launch hands it over).
// The agent inherits it too, so the run reads as working while either lives.
func watchOp(a *Agent) plugin.Op {
	return func(args []string, _ io.Reader, _, stderr io.Writer) int {
		if len(args) < 1 {
			return plugin.Usage(stderr, "watch <uid> [--resume]")
		}
		if err := a.Watch(args[0], slices.Contains(args, "--resume")); err != nil {
			fmt.Fprintln(stderr, err)
			return plugin.ExitFailed
		}
		return plugin.ExitOK
	}
}
