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
	"github.com/bilal-/sous/internal/runner"
)

// cmdConfig: sous config shows every setting; sous config <key> <value...>
// changes one; -p <project|org/*> works on one project's settings; --unset
// removes a setting.
func cmdConfig(e *Env, a argv) int {
	if e.cfgErr != nil {
		return fail(e, 1, "%s could not be read (%v); fix it by hand", config.Path(e.Home), e.cfgErr)
	}
	switch {
	case a.has("p"):
		return setProjectConfig(e, a)
	case len(a.pos) == 0 && !a.has("unset"):
		return showConfig(e)
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
		if a.has("add") || a.has("remove") {
			list, ok := v.([]string)
			if !ok {
				return fail(e, 2, "--add and --remove are for lists: %s is not one", k.Name)
			}
			v = changeList(k.Value(e.Cfg).([]string), list, a.has("add"))
		}
		if k.Name == "agent" && !slices.Contains(launcherNames(e), v.(string)) {
			return fail(e, 2, "no agent %q; choose one of: %s", v, strings.Join(launcherNames(e), ", "))
		}
		if k.Name == "runner" && !slices.Contains(runnerNames(e), v.(string)) {
			return fail(e, 2, "no runner %q; choose one of: %s", v, strings.Join(runnerNames(e), ", "))
		}
		value = v
	} else if len(a.pos) != 1 {
		return fail(e, 2, "--unset takes just the setting's name")
	} else if k.Name == "roots" {
		return fail(e, 2, "sous needs roots; change them with sous setup <folder> or sous config roots <folder...>")
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
	if strings.Contains(term, "*") {
		if org, rest, ok := strings.Cut(term, "/"); !ok || rest != "*" || org == "" || strings.Contains(org, "*") {
			return fail(e, 2, "%q: the only pattern sous knows is org/*, for every project in one org folder", term)
		}
	} else {
		p, code := resolveProject(e, term)
		if code != 0 {
			return code
		}
		key = project.OrgName(p.Path)
	}
	var value any
	switch {
	case len(a.pos) == 0 && !a.has("unset"):
		return showProjectConfig(e, key)
	case a.has("unset") && len(a.pos) == 1:
	case !a.has("unset") && len(a.pos) == 2:
		value = a.pos[1]
		if a.pos[0] == config.KeyBackend && !slices.Contains(backendNames(e), a.pos[1]) {
			return fail(e, 2, "no backend %q; choose one of: %s", a.pos[1], strings.Join(backendNames(e), ", "))
		}
	default:
		return fail(e, 2, "usage: sous config -p <project> <key> <value>, or -p <project> --unset <key>")
	}
	if err := config.Set(e.Home, []string{"projects", key}, a.pos[0], value); err != nil {
		return fail(e, 1, "%v", err)
	}
	if value != nil && !slices.Contains(config.ProjectKeys, a.pos[0]) {
		fmt.Fprintf(e.Stderr, "sous: note: sous itself does not read %q (it reads %s); a plugin may\n", a.pos[0], strings.Join(config.ProjectKeys, ", "))
	}
	return said(e, key, a.pos[0], value)
}

// changeList adds items not already there, or removes them, keeping order.
func changeList(have, items []string, add bool) []string {
	out := slices.Clone(have)
	for _, it := range items {
		switch i := slices.Index(out, it); {
		case add && i < 0:
			out = append(out, it)
		case !add && i >= 0:
			out = slices.Delete(out, i, i+1)
		}
	}
	return out
}

// said confirms a change in one line, or as JSON.
func said(e *Env, project, key string, value any) int {
	if e.JSON {
		return e.writeJSON(map[string]any{"project": project, "key": key, "value": value})
	}
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

func runnerNames(e *Env) []string {
	var names []string
	for _, r := range runner.Runners(e.Exe, runner.Registry.Names(), e.Cfg.Plugins) {
		names = append(names, r.Name)
	}
	return names
}

func backendNames(e *Env) []string {
	var names []string
	for _, b := range backend.Backends(e.Exe, backend.Registry.Names(), e.Cfg.Plugins) {
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
		if list, ok := s.Value.([]string); ok { // a copy: e.Cfg is not ours to change
			tilded := make([]string, len(list))
			for i := range list {
				tilded[i] = config.Tilde(e.UserHome, list[i])
			}
			shown = strings.Join(tilded, ", ")
		}
		switch {
		case k.Name == "runner" && shown == "":
			shown = e.Cfg.RunAgent() + " (the agent)"
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

// showProjectConfig: one project's (or org/*'s) settings as written.
func showProjectConfig(e *Env, key string) int {
	_, projects, err := config.Written(e.Home)
	if err != nil {
		return fail(e, 1, "%v", err)
	}
	settings := projects[key]
	if settings == nil {
		settings = map[string]string{}
	}
	if e.JSON {
		return e.writeJSON(map[string]any{"project": key, "settings": settings})
	}
	fmt.Fprintln(e.Stdout, key)
	if len(settings) == 0 {
		fmt.Fprintln(e.Stdout, "  no settings of its own")
	}
	names := make([]string, 0, len(settings))
	for k := range settings {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		fmt.Fprintf(e.Stdout, "  %s = %s\n", k, settings[k])
	}
	return 0
}
