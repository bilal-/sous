package cli

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/bilal-/sous/internal/backend"
	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/launcher"
	"github.com/bilal-/sous/internal/plugin"
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
			v = config.ChangeList(k.Value(e.Cfg).([]string), list, a.has("add"))
		}
		if code := checkName(e, k.Name, v); code != 0 {
			return code
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
		if err := config.CheckPattern(term); err != nil {
			return fail(e, 2, "%v", err)
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
		if code := checkName(e, a.pos[0], value); code != 0 {
			return code
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

// named are the settings whose value names a plugin, and the names there
// are: a typo must be caught before it is written.
var named = map[string]func(e *Env) []plugin.Plugin{
	"agent":           func(e *Env) []plugin.Plugin { return launcher.Launchers(e.Exe, launcher.BuiltinNames(), e.Cfg.Plugins) },
	"runner":          func(e *Env) []plugin.Plugin { return runner.Runners(e.Exe, runner.Registry.Names(), e.Cfg.Plugins) },
	config.KeyBackend: func(e *Env) []plugin.Plugin { return backend.Backends(e.Exe, backend.Registry.Names(), e.Cfg.Plugins) },
}

// checkName refuses a value for key that names no plugin there is.
func checkName(e *Env, key string, value any) int {
	list, ok := named[key]
	if !ok {
		return 0
	}
	var names []string
	for _, p := range list(e) {
		names = append(names, p.Name)
	}
	if !slices.Contains(names, value.(string)) {
		return fail(e, 2, "no %s %q; choose one of: %s", key, value, strings.Join(names, ", "))
	}
	return 0
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
