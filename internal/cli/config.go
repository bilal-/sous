package cli

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/bilal-/sous/internal/backend"
	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/launcher"
	"github.com/bilal-/sous/internal/project"
)

// cmdConfig: sous config shows every setting; sous config <key> <value...>
// changes one; -p <project|org/*> works on one project's settings; --unset
// removes a setting.
func cmdConfig(e *Env, a argv) int {
	if e.cfgErr != nil {
		return fail(e, 1, "%s could not be read (%v); fix it by hand", config.Path(e.Home), e.cfgErr)
	}
	switch {
	case len(a.pos) == 0 && !a.has("unset"):
		return showConfig(e)
	case a.has("p"):
		return setProjectConfig(e, a)
	}
	return setConfig(e, a)
}

func setConfig(e *Env, a argv) int {
	if len(a.pos) == 0 {
		return fail(e, 2, "which setting? one of: %s", config.KeyNames())
	}
	k, ok := config.KeyNamed(a.pos[0])
	if !ok {
		return fail(e, 2, "no setting %q; the settings are: %s (per project: sous config -p <project> <key> <value>)", a.pos[0], config.KeyNames())
	}
	var value any
	if !a.has("unset") {
		v, err := k.Parse(a.pos[1:])
		if err != nil {
			return fail(e, 2, "%v", err)
		}
		if k.Name == "agent" && !slices.Contains(launcherNames(e), v.(string)) {
			return fail(e, 2, "no agent %q; choose one of: %s", v, strings.Join(launcherNames(e), ", "))
		}
		value = v
	} else if len(a.pos) != 1 {
		return fail(e, 2, "--unset takes just the setting's name")
	}
	if k.Name == "roots" && value != nil {
		return setRoots(e, value.([]string))
	}
	if err := config.Set(e.Home, nil, k.Name, value); err != nil {
		return fail(e, 1, "%v", err)
	}
	return said(e, "", k.Name, value)
}

func setRoots(e *Env, roots []string) int {
	if err := config.SetRoots(e.Home, e.UserHome, roots); err != nil {
		return fail(e, 1, "%v", err)
	}
	return said(e, "", "roots", roots)
}

func setProjectConfig(e *Env, a argv) int {
	term := a.value("p")
	key := term // an org/* pattern names every project in that org
	if !strings.Contains(term, "*") {
		p, code := resolveProject(e, term)
		if code != 0 {
			return code
		}
		key = project.OrgName(p.Path)
	}
	var value any
	switch {
	case a.has("unset") && len(a.pos) == 1:
	case !a.has("unset") && len(a.pos) == 2:
		value = a.pos[1]
		if a.pos[0] == "backend" && !slices.Contains(backendNames(e), a.pos[1]) {
			return fail(e, 2, "no backend %q; choose one of: %s", a.pos[1], strings.Join(backendNames(e), ", "))
		}
	default:
		return fail(e, 2, "usage: sous config -p <project> <key> <value>, or -p <project> --unset <key>")
	}
	if err := config.Set(e.Home, []string{"projects", key}, a.pos[0], value); err != nil {
		return fail(e, 1, "%v", err)
	}
	return said(e, key, a.pos[0], value)
}

// said confirms a change in one line.
func said(e *Env, project, key string, value any) int {
	if project != "" {
		key = project + ": " + key
	}
	if value == nil {
		fmt.Fprintf(e.Stdout, "%s removed\n", key)
		return 0
	}
	if list, ok := value.([]string); ok {
		value = strings.Join(list, ", ")
	}
	fmt.Fprintf(e.Stdout, "%s = %v\n", key, value)
	return 0
}

func launcherNames(e *Env) []string {
	var names []string
	for _, l := range launcher.Launchers(e.Exe, launcher.BuiltinNames(), e.Cfg.Plugins) {
		names = append(names, l.Name)
	}
	return names
}

func backendNames(e *Env) []string {
	var names []string
	for _, b := range backend.Backends(e.Exe, backend.BuiltinNames(), e.Cfg.Plugins) {
		names = append(names, b.Name)
	}
	return names
}

// configView is sous config --json.
type configView struct {
	File     string                       `json:"file"`
	Settings map[string]settingView       `json:"settings"`
	Projects map[string]map[string]string `json:"projects"`
}

type settingView struct {
	Value   any  `json:"value"`
	Default bool `json:"default"`
}

func showConfig(e *Env) int {
	set, projects, err := config.Written(e.Home)
	if err != nil {
		return fail(e, 1, "%v", err)
	}
	v := configView{File: config.Tilde(e.UserHome, config.Path(e.Home)), Settings: map[string]settingView{}, Projects: projects}
	for _, k := range config.Keys() {
		v.Settings[k.Name] = settingView{Value: k.Value(e.Cfg), Default: !set[k.Name]}
	}
	if e.JSON {
		return e.writeJSON(v)
	}
	fmt.Fprintf(e.Stdout, "settings (%s)\n", v.File)
	for _, k := range config.Keys() {
		s := v.Settings[k.Name]
		shown := fmt.Sprint(s.Value)
		if list, ok := s.Value.([]string); ok {
			for i := range list {
				list[i] = config.Tilde(e.UserHome, list[i])
			}
			shown = strings.Join(list, ", ")
		}
		switch {
		case shown == "" || shown == "0":
			shown = "(none)"
		case s.Default:
			shown += " (default)"
		}
		fmt.Fprintf(e.Stdout, "  %-14s %s\n", k.Name, shown)
	}
	if len(projects) > 0 {
		fmt.Fprintln(e.Stdout, "\nprojects")
		names := make([]string, 0, len(projects))
		for n := range projects {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			var pairs []string
			for k, val := range projects[n] {
				pairs = append(pairs, k+" = "+val)
			}
			sort.Strings(pairs)
			fmt.Fprintf(e.Stdout, "  %-14s %s\n", n, strings.Join(pairs, " · "))
		}
	}
	fmt.Fprintln(e.Stdout, "\n  sous config <key> <value> to change · sous config -p <project> <key> <value> for one project")
	return 0
}
