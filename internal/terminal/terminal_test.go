package terminal

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func env(kv ...string) Env {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return func(k string) string { return m[k] }
}

func TestDetectPrefersTheTerminalInsideAnother(t *testing.T) {
	for _, c := range []struct {
		setting, goos string
		env           Env
		want          string
	}{
		{"", "darwin", env("HERDR_ENV", "1", "HERDR_SOCKET_PATH", "/s", "TMUX", "x", "TERM_PROGRAM", "ghostty"), "herdr"},
		{"", "darwin", env("TMUX", "/t", "TERM_PROGRAM", "ghostty"), "tmux"},
		{"auto", "darwin", env("TERM_PROGRAM", "ghostty"), "ghostty"},
		{"", "darwin", env("TERM_PROGRAM", "iTerm.app"), "iterm"},
		{"", "darwin", env("TERM_PROGRAM", "Apple_Terminal"), "terminal"},
		{"", "linux", env("TERM_PROGRAM", "ghostty"), ""},
		{"", "linux", env("TMUX", "/t"), "tmux"},
		{"", "darwin", env("HERDR_ENV", "1"), ""}, // no socket: not inside herdr
		{"iterm", "darwin", env("TERM_PROGRAM", "ghostty"), "iterm"},
		{"none", "darwin", env("TERM_PROGRAM", "ghostty"), ""},
		{"ghostty", "linux", env(), ""},
		{"kitty", "darwin", env(), ""},
	} {
		k, ok, why := Detect(c.setting, c.goos, c.env)
		if k.Name != c.want || ok != (c.want != "") || (!ok && why == "") {
			t.Errorf("Detect(%q, %s) = %q %v %q, want %q", c.setting, c.goos, k.Name, ok, why, c.want)
		}
	}
}

type call []string

func recorder(answers map[string]string) (*[]call, Runner) {
	var calls []call
	return &calls, func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, append(call{name}, args...))
		return []byte(answers[name+" "+args[0]]), nil
	}
}

func TestHerdrOpensATabAndTypesIntoItsPane(t *testing.T) {
	calls, run := recorder(map[string]string{"/bin/herdr tab": `{"id":"x","result":{"root_pane":{"pane_id":"wD:p5"},"type":"tab_created"}}`})
	k, _ := ByName("herdr")
	if err := k.Open(context.Background(), run, env("HERDR_BIN_PATH", "/bin/herdr", "HERDR_WORKSPACE_ID", "wD"), Tab{Dir: "/code/acme/api", Label: "api", Type: "sous go ."}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"/bin/herdr tab create --cwd /code/acme/api --focus --label api --workspace wD",
		"/bin/herdr pane run wD:p5 sous go .",
	}
	if got := joined(*calls); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s", strings.Join(got, "\n"))
	}
}

func TestTmuxOpensAWindowAndTypesLiterally(t *testing.T) {
	calls, run := recorder(map[string]string{"tmux new-window": "%7\n"})
	k, _ := ByName("tmux")
	if err := k.Open(context.Background(), run, env(), Tab{Dir: "/code/acme/api", Label: "api", Type: "sous go ."}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"tmux new-window -c /code/acme/api -P -F #{pane_id} -n api",
		"tmux send-keys -t %7 -l sous go .",
		"tmux send-keys -t %7 Enter",
	}
	if got := joined(*calls); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s", strings.Join(got, "\n"))
	}
}

func TestAShellOnlyTabTypesNothing(t *testing.T) {
	for _, name := range []string{"tmux", "herdr"} {
		calls, run := recorder(nil)
		k, _ := ByName(name)
		if err := k.Open(context.Background(), run, env(), Tab{Dir: "/code/acme/api"}); err != nil {
			t.Fatal(err)
		}
		if len(*calls) != 1 {
			t.Errorf("%s: %v", name, *calls)
		}
	}
}

// The folder and the command go to AppleScript as arguments, never inside
// the script, so a folder name cannot change what runs.
func TestAppleScriptTerminalsTakeTheFolderAsAnArgument(t *testing.T) {
	for _, name := range []string{"ghostty", "iterm", "terminal"} {
		calls, run := recorder(nil)
		k, _ := ByName(name)
		dir := `/code/it's "odd"; rm -rf ~`
		if err := k.Open(context.Background(), run, env(), Tab{Dir: dir, Type: "sous go ."}); err != nil {
			t.Fatal(err)
		}
		c := (*calls)[0]
		if c[0] != "osascript" || c[1] != "-e" || strings.Contains(c[2], "odd") || c[3] != dir || c[4] != "sous go ." {
			t.Errorf("%s: %q", name, c)
		}
		if !strings.Contains(c[2], "item 1 of argv") {
			t.Errorf("%s does not read the folder from its arguments", name)
		}
	}
}

func TestOpenRefusesARelativeFolderOrTwoLines(t *testing.T) {
	_, run := recorder(nil)
	k, _ := ByName("tmux")
	if err := k.Open(context.Background(), run, env(), Tab{Dir: "code/api"}); err == nil {
		t.Error("a relative folder was opened")
	}
	if err := k.Open(context.Background(), run, env(), Tab{Dir: "/code/api", Type: "a\nb"}); err == nil {
		t.Error("two lines were typed")
	}
}

func TestMacOSRefusalSaysWhereToAllowIt(t *testing.T) {
	k, _ := ByName("ghostty")
	err := k.Open(context.Background(), func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("osascript: execution error: Not authorized to send Apple events to Ghostty. (-1743)")
	}, env(), Tab{Dir: "/code/api"})
	if err == nil || !strings.Contains(err.Error(), "Automation") {
		t.Fatal(err)
	}
}

func joined(cs []call) []string {
	var out []string
	for _, c := range cs {
		out = append(out, strings.Join(c, " "))
	}
	return out
}

// Each AppleScript compiles against the real app's scripting terms when
// that app is installed: a name that finds the app only while it runs (as
// "iTerm2" did, for iTerm.app), or a variable named after one of its words
// ("line"), fails here instead of on someone's click. Run it with the app
// closed to catch the first.
func TestAppleScriptsCompile(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("AppleScript is macOS only")
	}
	if _, err := exec.LookPath("osacompile"); err != nil {
		t.Skip("no osacompile")
	}
	for name, c := range map[string]struct{ app, script string }{
		"ghostty":  {"/Applications/Ghostty.app", ghosttyScript},
		"iterm":    {"/Applications/iTerm.app", itermScript},
		"terminal": {"/System/Applications/Utilities/Terminal.app", terminalScript},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := os.Stat(c.app); err != nil {
				t.Skip(c.app + " is not installed")
			}
			out, err := exec.Command("osacompile", "-o", filepath.Join(t.TempDir(), "s.scpt"), "-e", c.script).CombinedOutput()
			if err != nil {
				t.Fatalf("%v: %s", err, out)
			}
		})
	}
}
