package doctor

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/bilal-/sous/internal/plugin"
	"github.com/bilal-/sous/internal/runner"
)

// credentialVars are what lets a program publish as the person, which a run
// started from a phone would inherit with the rest of sous's environment.
var credentialVars = []string{"GH_TOKEN", "GITHUB_TOKEN", "GITLAB_TOKEN", "GL_TOKEN", "SSH_AUTH_SOCK", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"}

// remoteRunChecks are for runs a host starts with no one at the computer:
// only when a project allows them. Each allowed project's runner must
// exist, and should describe itself and have a check; credentials in the
// environment are worth knowing about.
func remoteRunChecks(in Inputs) []Check {
	var allowed []string
	for name, keys := range in.Cfg.Projects {
		if keys["remote_runs"] == "true" {
			allowed = append(allowed, name)
		}
	}
	if len(allowed) == 0 {
		return nil
	}
	sort.Strings(allowed)
	out := []Check{{Name: "remote runs", Status: OK, Detail: "a host may start runs in " + strings.Join(allowed, ", ")}}
	runners := runner.Registry.All(in.Exe, in.Cfg.Plugins)
	for _, name := range allowed {
		r, ok := plugin.Find(runners, in.Cfg.RunAgentFor(name))
		c := Check{Name: "remote runs in " + name, Status: OK}
		switch {
		case !ok:
			c.Status, c.Detail = Bad, fmt.Sprintf("its runner %q is neither built in nor a listed plugin, so a run there cannot start", in.Cfg.RunAgentFor(name))
			c.Fix = fmt.Sprintf("sous config -p '%s' runner <name>", name)
		case r.Offline:
			c.Detail = "runner " + r.Name
		default:
			if caps := runner.Describe(context.Background(), r); caps.Described && slices.Contains(caps.Fields, "phase") {
				c.Detail = "runner " + r.Name + ", which reports phases"
			} else {
				c.Status, c.Detail = Warn, "runner "+r.Name+" does not describe itself, so a host sees only running, needs you, done or failed"
			}
		}
		if c.Status == OK && in.Cfg.Project(name).Check == "" {
			c.Status = Warn
			c.Detail += "; no check command, so a run is ready without one"
			c.Fix = fmt.Sprintf("sous config -p '%s' check \"make test\"", name)
		}
		out = append(out, c)
	}
	var found []string
	for _, kv := range in.Env {
		k, v, _ := strings.Cut(kv, "=")
		if v != "" && slices.Contains(credentialVars, k) {
			found = append(found, k)
		}
	}
	c := Check{Name: "run credentials", Status: OK, Detail: "no publishing credentials in sous's environment"}
	if len(found) > 0 {
		sort.Strings(found)
		c.Status = Warn
		c.Detail = "runs inherit " + strings.Join(found, ", ") + ": an agent could publish with them. The built in runners block git pushes, not every tool"
		c.Fix = "start sous's host without them, or keep remote runs to projects you trust the agent with"
	}
	return append(out, c)
}
