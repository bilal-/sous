package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/text"
)

// loadProjects applies --root overrides else config roots. Errors are exit codes.
func loadProjects(e *Env, roots []string) ([]project.Project, int, int) {
	cfg, code := e.config()
	if code != 0 {
		return nil, 0, code
	}
	if len(roots) == 0 {
		roots = cfg.Roots
	}
	if len(roots) == 0 {
		return nil, 0, fail(e, exitFailed, "%s (or pass --root)", config.NoRootsHint)
	}
	ps, unavailable := project.Discover(roots, cfg.Ignore, e.Stderr)
	return ps, unavailable, 0
}

// cmdProjects: --root (repeatable) scans those roots instead of config's;
// --path prints the single project matching a term, for shell wrappers.
func cmdProjects(e *Env, a argv) int {
	ps, _, code := loadProjects(e, a.values("root"))
	if code != 0 {
		return code
	}
	if pathOf := a.value("path"); pathOf != "" {
		p, err := project.Match(ps, pathOf)
		if err != nil {
			return fail(e, exitUsage, "%v", err)
		}
		fmt.Fprintln(e.Stdout, p.Path)
		return 0
	}
	if len(a.pos) > 0 {
		ps = project.Candidates(ps, a.pos[0])
		if len(ps) == 0 {
			return fail(e, exitUsage, "no project matches '%s'", a.pos[0])
		}
	}
	if e.JSON {
		if ps == nil {
			ps = []project.Project{} // [] rather than null: empty is an answer
		}
		return e.writeJSON(ps)
	}
	if len(ps) == 0 {
		fmt.Fprintln(e.Stdout, "0 projects in your project folders")
		return 0
	}
	renderProjects(e.Stdout, ps, time.Now().UTC())
	return 0
}

// resolveProject maps project.Resolve's errors to exit codes.
func resolveProject(e *Env, term string) (project.Project, int) {
	if term == "." {
		term = "" // "." is the project you are in
	}
	// No term means "the repo I'm in" — capture must work even when
	// config.toml is broken or absent. Only a term needs roots.
	var roots, ignore []string
	if term != "" {
		cfg, code := e.config()
		if code != 0 {
			return project.Project{}, code
		}
		if len(cfg.Roots) == 0 {
			return project.Project{}, fail(e, exitFailed, "%s", config.NoRootsHint)
		}
		roots, ignore = cfg.Roots, cfg.Ignore
	}
	p, err := project.Resolve(roots, ignore, term, e.Cwd, e.UserHome, e.Stderr)
	switch {
	case err == nil:
		return p, 0
	case errors.Is(err, project.ErrNotInProject):
		return project.Project{}, fail(e, exitUsage, "not inside a project; use -p <project>")
	}
	return project.Project{}, fail(e, exitUsage, "%v", err)
}

func renderProjects(w io.Writer, ps []project.Project, now time.Time) {
	ow, nw := 3, 4
	for _, p := range ps {
		if len(p.Org) > ow {
			ow = len(p.Org)
		}
		if len(p.Name) > nw {
			nw = len(p.Name)
		}
	}
	fmt.Fprintf(w, "%-*s   %-*s   %-8s   %s\n", ow, "org", nw, "name", "host", "last commit")
	for _, p := range ps {
		host, last := "local", "no commits"
		if p.Remote != nil {
			host = strings.TrimSuffix(strings.SplitN(*p.Remote, "/", 2)[0], ".com")
			if host == "" {
				host = "other" // a path or other non-URL remote
			}
		}
		if p.LastCommit != nil {
			last = text.Age(now, *p.LastCommit)
		}
		fmt.Fprintf(w, "%-*s   %-*s   %-8s   %s\n", ow, p.Org, nw, p.Name, host, last)
	}
}
