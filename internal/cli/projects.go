package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/bilal-/sous/internal/project"
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
		return nil, 0, fail(e, 1, "no roots: add roots = [\"~/code\"] to %s/config.toml or pass --root", e.Home)
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
			return fail(e, 2, "%v", err)
		}
		fmt.Fprintln(e.Stdout, p.Path)
		return 0
	}
	if len(a.pos) > 0 {
		ps = project.Candidates(ps, a.pos[0])
		if len(ps) == 0 {
			return fail(e, 2, "no project matches '%s'", a.pos[0])
		}
	}
	if e.JSON {
		if ps == nil {
			ps = []project.Project{} // [] rather than null: empty is an answer
		}
		return e.writeJSON(ps)
	}
	if len(ps) == 0 {
		fmt.Fprintln(e.Stdout, "0 projects under your roots (set roots in ~/.sous/config.toml)")
		return 0
	}
	project.RenderTable(e.Stdout, ps, time.Now().UTC())
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
			return project.Project{}, fail(e, 1, "no roots: add roots = [\"~/code\"] to %s/config.toml", e.Home)
		}
		roots, ignore = cfg.Roots, cfg.Ignore
	}
	p, err := project.Resolve(roots, ignore, term, e.Cwd, e.UserHome, e.Stderr)
	switch {
	case err == nil:
		return p, 0
	case errors.Is(err, project.ErrNotInProject):
		return project.Project{}, fail(e, 2, "not inside a project; use -p <project>")
	}
	return project.Project{}, fail(e, 2, "%v", err)
}
