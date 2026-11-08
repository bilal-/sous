// Package terminal opens a new tab, in a project's folder, in the terminal
// the person is using: herdr, tmux, Ghostty, iTerm2 or macOS Terminal. It
// can type one command into the new tab, so an agent starts there in the
// person's own shell. Which terminal is known from the environment sous
// was started in, or from the terminal setting; cli reads both and passes
// them in.
package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Kind is one terminal sous can open tabs in.
type Kind struct {
	Name  string // the setting's value: "ghostty"
	Title string // what a person calls it: "Ghostty"
	// Mac: it is driven with AppleScript, so only on macOS, and macOS asks
	// the person once to let sous control it.
	Mac bool
	// Tab: it opens a tab; false when it can only open a window.
	Tab  bool
	open func(ctx context.Context, run Runner, env Env, t Tab) error
}

// Tab is what to open: a folder, the tab's label, and a command to type
// into its shell ("" for none).
type Tab struct {
	Dir, Label, Type string
}

// Env reads the environment sous was started in.
type Env func(string) string

// Runner runs a program and gives its standard output; the error carries
// what it said on standard error.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// Exec runs programs for real.
func Exec(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) && len(ee.Stderr) > 0 {
		return out, fmt.Errorf("%s: %s", name, strings.TrimSpace(string(ee.Stderr)))
	}
	return out, err
}

// Kinds are the terminals sous knows, in the order Detect prefers them: a
// terminal inside another (herdr, tmux) is where the person is working.
var Kinds = []Kind{
	{Name: "herdr", Title: "herdr", Tab: true, open: herdr},
	{Name: "tmux", Title: "tmux", Tab: true, open: tmux},
	{Name: "ghostty", Title: "Ghostty", Mac: true, Tab: true, open: osa(ghosttyScript)},
	{Name: "iterm", Title: "iTerm2", Mac: true, Tab: true, open: osa(itermScript)},
	{Name: "terminal", Title: "Terminal", Mac: true, open: osa(terminalScript)},
}

// Names are the values the terminal setting takes, "auto" first.
func Names() []string {
	out := []string{"auto", "none"}
	for _, k := range Kinds {
		out = append(out, k.Name)
	}
	return out
}

// ByName is the terminal called name.
func ByName(name string) (Kind, bool) {
	for _, k := range Kinds {
		if k.Name == name {
			return k, true
		}
	}
	return Kind{}, false
}

// Detect is the terminal to open tabs in. setting is the terminal setting:
// "" or "auto" looks at the environment, "none" turns tabs off, and a name
// picks that terminal. ok is false when there is none to use, and why says
// what to do about it.
func Detect(setting, goos string, env Env) (k Kind, ok bool, why string) {
	switch setting {
	case "none":
		return Kind{}, false, "tabs are turned off (sous config terminal auto turns them on)"
	case "", "auto":
	default:
		k, ok := ByName(setting)
		switch {
		case !ok:
			return Kind{}, false, fmt.Sprintf("the terminal setting %q is not one sous knows (%s)", setting, strings.Join(Names(), ", "))
		case k.Mac && goos != "darwin":
			return Kind{}, false, k.Title + " tabs need macOS"
		}
		return k, true, ""
	}
	name := ""
	switch {
	case env("HERDR_ENV") == "1" && env("HERDR_SOCKET_PATH") != "":
		name = "herdr"
	case env("TMUX") != "":
		name = "tmux"
	case goos != "darwin":
	case env("TERM_PROGRAM") == "ghostty":
		name = "ghostty"
	case env("TERM_PROGRAM") == "iTerm.app":
		name = "iterm"
	case env("TERM_PROGRAM") == "Apple_Terminal":
		name = "terminal"
	}
	if name == "" {
		return Kind{}, false, "sous was not started in a terminal it can open tabs in; start sous serve in herdr, tmux, Ghostty, iTerm2 or Terminal, or set one with sous config terminal <name>"
	}
	k, _ = ByName(name)
	return k, true, ""
}

// Open opens t in k.
func (k Kind) Open(ctx context.Context, run Runner, env Env, t Tab) error {
	if t.Dir == "" || !strings.HasPrefix(t.Dir, "/") {
		return fmt.Errorf("a tab opens in a folder named by its whole path, not %q", t.Dir)
	}
	if strings.ContainsAny(t.Type, "\n\r") {
		return errors.New("the command to type is one line")
	}
	return k.open(ctx, run, env, t)
}

// herdr opens a tab in the current herdr workspace, then types the
// command into its first pane.
func herdr(ctx context.Context, run Runner, env Env, t Tab) error {
	bin := env("HERDR_BIN_PATH")
	if bin == "" {
		bin = "herdr"
	}
	args := []string{"tab", "create", "--cwd", t.Dir, "--focus"}
	if t.Label != "" {
		args = append(args, "--label", t.Label)
	}
	if ws := env("HERDR_WORKSPACE_ID"); ws != "" {
		args = append(args, "--workspace", ws)
	}
	out, err := run(ctx, bin, args...)
	if err != nil || t.Type == "" {
		return err
	}
	var created struct {
		Result struct {
			RootPane struct {
				PaneID string `json:"pane_id"`
			} `json:"root_pane"`
		} `json:"result"`
	}
	if json.Unmarshal(out, &created) != nil || created.Result.RootPane.PaneID == "" {
		return errors.New("herdr opened the tab but did not say which pane it has")
	}
	_, err = run(ctx, bin, "pane", "run", created.Result.RootPane.PaneID, t.Type)
	return err
}

// tmux opens a window in the session sous was started in.
func tmux(ctx context.Context, run Runner, _ Env, t Tab) error {
	args := []string{"new-window", "-c", t.Dir, "-P", "-F", "#{pane_id}"}
	if t.Label != "" {
		args = append(args, "-n", t.Label)
	}
	out, err := run(ctx, "tmux", args...)
	if err != nil || t.Type == "" {
		return err
	}
	pane := strings.TrimSpace(string(out))
	if pane == "" {
		return errors.New("tmux opened the window but did not say which pane it has")
	}
	_, err = run(ctx, "tmux", "send-keys", "-t", pane, "-l", t.Type)
	if err == nil {
		_, err = run(ctx, "tmux", "send-keys", "-t", pane, "Enter")
	}
	return err
}

// osa runs an AppleScript with the folder and the command as its
// arguments, never pasted into the script, so neither can change it.
func osa(script string) func(context.Context, Runner, Env, Tab) error {
	return func(ctx context.Context, run Runner, _ Env, t Tab) error {
		_, err := run(ctx, "osascript", "-e", script, t.Dir, t.Type)
		if err != nil && strings.Contains(err.Error(), "-1743") {
			return fmt.Errorf("macOS has not let sous control this terminal; allow it in System Settings, Privacy & Security, Automation (%w)", err)
		}
		return err
	}
}

// Ghostty 1.3 and later: a new tab in the front window, or a new window
// when it has none, with the folder and the command as typed input.
const ghosttyScript = `on run argv
	tell application "Ghostty"
		set cfg to new surface configuration
		set initial working directory of cfg to item 1 of argv
		if item 2 of argv is not "" then set initial input of cfg to (item 2 of argv) & linefeed
		if (count of windows) is 0 then
			new window with configuration cfg
		else
			new tab in front window with configuration cfg
		end if
		activate
	end tell
end run`

// iTerm2: a new tab in the current window, or a new window, then cd and
// the command, typed into the shell.
const itermScript = `on run argv
	set line to "cd " & quoted form of (item 1 of argv) & " && clear"
	if item 2 of argv is not "" then set line to line & " && " & (item 2 of argv)
	tell application "iTerm2"
		if (count of windows) is 0 then
			create window with default profile
		else
			tell current window to create tab with default profile
		end if
		tell current session of current window to write text line
		activate
	end tell
end run`

// Terminal opens a window: it has no scripting for tabs.
const terminalScript = `on run argv
	set line to "cd " & quoted form of (item 1 of argv) & " && clear"
	if item 2 of argv is not "" then set line to line & " && " & (item 2 of argv)
	tell application "Terminal"
		do script line
		activate
	end tell
end run`
