package cli

import (
	"fmt"
	"strings"
)

// argSpec is what a verb accepts. Flags may appear anywhere, spelled -x or
// --x, with a value as the next argument or after "="; "--" ends flags so a
// note can start with a dash. Names like "a|agent" are aliases; the first
// is the canonical key.
type argSpec struct {
	bools    []string
	values   []string // repeatable: every occurrence is kept
	min, max int      // positional count; max < 0 is unbounded
	raw      bool     // no flags at all: every argument is text ("-2 regressions")
}

// argv is a parsed command line: positionals in order, flags by canonical
// name (a bool flag is present with one "" value).
type argv struct {
	pos []string
	set map[string][]string
}

func (a argv) has(name string) bool { _, ok := a.set[name]; return ok }

// value is the last occurrence of a value flag, or "".
func (a argv) value(name string) string {
	vs := a.set[name]
	if len(vs) == 0 {
		return ""
	}
	return vs[len(vs)-1]
}

func (a argv) values(name string) []string { return a.set[name] }

func parseArgs(s argSpec, in []string) (argv, error) {
	canon := map[string]string{}
	takesValue := map[string]bool{}
	declare := func(names []string, value bool) {
		for _, n := range names {
			aliases := strings.Split(n, "|")
			for _, alias := range aliases {
				canon[alias], takesValue[alias] = aliases[0], value
			}
		}
	}
	declare(s.bools, false)
	declare(s.values, true)
	a := argv{set: map[string][]string{}}
	if s.raw {
		a.pos = in
		if len(in) > 0 && in[0] == "--" {
			a.pos = in[1:]
		}
		in = nil
	}
	for i := 0; i < len(in); i++ {
		arg := in[i]
		if arg == "--" {
			a.pos = append(a.pos, in[i+1:]...)
			break
		}
		if len(arg) < 2 || arg[0] != '-' {
			a.pos = append(a.pos, arg)
			continue
		}
		spelled, val, hasVal := strings.Cut(arg, "=")
		name := strings.TrimLeft(spelled, "-")
		key, ok := canon[name]
		switch {
		case !ok:
			return argv{}, fmt.Errorf("unknown flag: %s", arg)
		case !takesValue[name] && hasVal:
			return argv{}, fmt.Errorf("%s takes no value", spelled)
		case !takesValue[name]:
			val = ""
		case !hasVal:
			if i+1 >= len(in) {
				return argv{}, fmt.Errorf("%s needs a value", spelled)
			}
			i++
			val = in[i]
		}
		a.set[key] = append(a.set[key], val)
	}
	switch {
	case len(a.pos) < s.min:
		return argv{}, fmt.Errorf("missing arguments")
	case s.max >= 0 && len(a.pos) > s.max:
		return argv{}, fmt.Errorf("too many arguments")
	}
	return a, nil
}
