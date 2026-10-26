package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/bilal-/sous/internal/plugin"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/runner"
)

const runnerCheckUsage = "sous runner check <program> [--project <folder>] [--wait <seconds>] [--json]"

// cmdRunnerCheck is `sous runner check`: runner.Check against a runner
// program, so a plugin in any language can be held to the contract. It
// really starts a run, in a scratch repository unless --project names one.
func cmdRunnerCheck(e *Env, args []string) int {
	var prog, proj string
	var wait time.Duration
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--json":
			e.JSON = true
		case (a == "--project" || a == "--wait") && i+1 < len(args):
			i++
			if a == "--project" {
				proj = args[i]
				continue
			}
			s, err := strconv.Atoi(args[i])
			if err != nil || s < 0 {
				return fail(e, exitUsage, "--wait takes whole seconds, not %q\nusage: %s", args[i], runnerCheckUsage)
			}
			wait = time.Duration(s) * time.Second
		case prog == "" && a != "" && a[0] != '-':
			prog = a
		default:
			return fail(e, exitUsage, "usage: %s", runnerCheckUsage)
		}
	}
	if prog == "" {
		return fail(e, exitUsage, "usage: %s", runnerCheckUsage)
	}
	rs := runner.Registry.Discover("", nil, []string{prog})
	if len(rs) == 0 {
		return fail(e, exitUsage, "%s is not named %s<name>\nusage: %s", prog, plugin.Prefix(plugin.AxisRunner), runnerCheckUsage)
	}
	if st, err := os.Stat(prog); err != nil || st.IsDir() || st.Mode()&0o111 == 0 {
		return fail(e, exitUsage, "%s is not a program sous can run\nusage: %s", prog, runnerCheckUsage)
	}
	r := rs[0]
	if proj == "" {
		dir, err := scratchRepo()
		if err != nil {
			return fail(e, exitFailed, "a scratch repository to run in: %v", err)
		}
		defer os.RemoveAll(dir)
		proj = dir
	}
	problems := runner.Check(context.Background(), r, proj, runner.CheckOptions{Wait: wait})
	if e.JSON {
		e.writeJSON(struct {
			Runner   string   `json:"runner"`
			OK       bool     `json:"ok"`
			Problems []string `json:"problems"`
		}{r.Name, len(problems) == 0, append([]string{}, problems...)})
	} else if len(problems) == 0 {
		fmt.Fprintf(e.Stdout, "%s follows the runner contract\n", r.Name)
	} else {
		fmt.Fprintf(e.Stdout, "%s breaks the runner contract:\n", r.Name)
		for _, p := range problems {
			fmt.Fprintln(e.Stdout, "  "+p)
		}
	}
	if len(problems) > 0 {
		return exitFailed
	}
	return 0
}

// runnerCheck: the one door call that shows something, and so takes --json.
func runnerCheck(cmd string, rest []string) bool {
	return cmd == plugin.AxisRunner && len(rest) > 0 && rest[0] == "check"
}

// scratchRepo is a throwaway git repository with one commit, for a run that
// should touch nothing real.
func scratchRepo() (string, error) {
	dir, err := os.MkdirTemp("", "sous-runner-check-")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("scratch\n"), 0o600); err != nil {
		return dir, err
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "README"}, {"-c", "user.name=sous", "-c", "user.email=sous@example.org", "commit", "-qm", "scratch"}} {
		if _, err := project.Git(dir, args...); err != nil {
			os.RemoveAll(dir)
			return "", err
		}
	}
	return dir, nil
}
