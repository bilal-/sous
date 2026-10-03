package plugin

import (
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strings"
)

// Builtin is one built in plugin of an axis: its name, whether it stays on
// this machine, and its calls. Ops builds the calls only when one is made,
// from what the axis gives every built in (D), so listing the built ins
// costs nothing.
type Builtin[D any] struct {
	Name    string
	Offline bool
	Ops     func(D) map[string]Op
}

// Registry is an axis and its built ins, in the order sous asks them. A new
// built in is one more entry; the door, discovery and doctor read it.
type Registry[D any] struct {
	Axis     string
	Builtins []Builtin[D]
}

// Names: every built in, in order.
func (r Registry[D]) Names() []string {
	names := make([]string, len(r.Builtins))
	for i, b := range r.Builtins {
		names[i] = b.Name
	}
	return names
}

// All: every built in, then the plugin programs among thirdParty.
func (r Registry[D]) All(exe string, thirdParty []string) []Plugin {
	return r.Discover(exe, r.Names(), thirdParty)
}

// Offline: the built ins that never leave this machine, for a session
// start; no plugin program is assumed to stay offline.
func (r Registry[D]) Offline(exe string) []Plugin {
	var names []string
	for _, b := range r.Builtins {
		if b.Offline {
			names = append(names, b.Name)
		}
	}
	return r.Discover(exe, names, nil)
}

// Discover lists the named built ins (called as `exe <axis> <name>`, so
// they take the same door as any plugin) then the plugin programs among
// thirdParty named sous-<axis>-<name>.
func (r Registry[D]) Discover(exe string, builtins, thirdParty []string) []Plugin {
	var out []Plugin
	for _, n := range builtins {
		i := slices.IndexFunc(r.Builtins, func(b Builtin[D]) bool { return b.Name == n })
		out = append(out, Plugin{Name: n, Argv: []string{exe, r.Axis, n}, Offline: i >= 0 && r.Builtins[i].Offline})
	}
	prefix := Prefix(r.Axis)
	for _, p := range thirdParty {
		if name, ok := strings.CutPrefix(filepath.Base(p), prefix); ok && name != "" {
			out = append(out, Plugin{Name: name, Argv: []string{p}})
		}
	}
	return out
}

// Serve is the door to the built ins, `sous <axis> <name> <call> [args]`:
// it makes the call exactly as a plugin program would answer it.
func (r Registry[D]) Serve(d D, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	usage := fmt.Sprintf("sous %s <name> <call> [args]; the built ins are %s", r.Axis, strings.Join(r.Names(), ", "))
	if len(args) < 2 {
		return Usage(stderr, usage)
	}
	i := slices.IndexFunc(r.Builtins, func(b Builtin[D]) bool { return b.Name == args[0] })
	if i < 0 {
		fmt.Fprintf(stderr, "no built in %s: %s\n", r.Axis, args[0])
		return Usage(stderr, usage)
	}
	ops := r.Builtins[i].Ops(d)
	op, ok := ops[args[1]]
	if !ok {
		calls := slices.Sorted(maps.Keys(ops))
		return Usage(stderr, fmt.Sprintf("sous %s %s %s [args]", r.Axis, args[0], strings.Join(calls, "|")))
	}
	return op(args[2:], stdin, stdout, stderr)
}
