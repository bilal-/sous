package cli

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/bilal-/sous/internal/testutil"
)

func sousCmd(f *fx, dir string, args ...string) *exec.Cmd {
	exe, _ := os.Executable()
	c := exec.Command(exe, args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "SOUS_TEST_AS_BINARY=1")
	return c
}

func TestGoExecsAgentInProject(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("a/ios-app", true)
	real, _ := filepath.EvalSymlinks(p)
	f.bin("claude", "echo \"claude in $PWD\"; [ -n \"$SOUS_HERE_FILE\" ] && head -1 \"$SOUS_HERE_FILE\"")
	f.bin("codex", "echo \"codex in $PWD\"; exit 7")

	out, err := sousCmd(f, f.Home, "go", "ios").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "claude in "+real) || !strings.Contains(string(out), "→ claude") || !strings.Contains(string(out), "ios-app ·") {
		t.Fatalf("%v\n%s", err, out)
	}
	c := sousCmd(f, f.Home, "go", "ios", "-a", "codex")
	out, err = c.CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 7 || !strings.HasSuffix(strings.TrimSpace(string(out)), "codex in "+real) {
		t.Fatalf("exit passthrough / nothing after agent: %v\n%s", err, out)
	}
	f.writeConfig("roots = [\"" + f.WS + "\"]\nagent = \"codex\"\n")
	if err := sousCmd(f, f.Home, "go", "ios").Run(); !errors.As(err, &ee) || ee.ExitCode() != 7 {
		t.Fatal("config agent honoured")
	}
	if err := sousCmd(f, f.Home, "go", "ios", "-a", "nope").Run(); !errors.As(err, &ee) || ee.ExitCode() != 2 {
		t.Fatal("unknown launcher exit 2")
	}
	// A missing agent is not set up (exit 3), whatever this machine has
	// installed: PATH is the fixture's bin and git alone.
	os.Remove(filepath.Join(f.Home, "bin", "codex"))
	testutil.OnlyGit(t)
	t.Setenv("PATH", filepath.Join(f.Home, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err = sousCmd(f, f.Home, "go", "ios").CombinedOutput()
	if !errors.As(err, &ee) || ee.ExitCode() != 3 || !strings.Contains(string(out), "codex") {
		t.Fatalf("missing binary: %v %s", err, out)
	}
}

func TestGoCtrlCReachesAgent(t *testing.T) {
	f := fixture(t)
	f.mkrepo("a/r", true)
	started := filepath.Join(f.Home, "agent-started")
	f.bin("claude", "trap 'echo trapped' INT\ntouch "+started+"\nsleep 2\necho done")
	c := sousCmd(f, f.Home, "go", "r")
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var out bytes.Buffer
	c.Stdout, c.Stderr = &out, &out
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	// Wait until the agent is actually running (sous has exec'd), then
	// interrupt the whole group the way a terminal does.
	waitFor(t, func() bool { _, err := os.Stat(started); return err == nil })
	syscall.Kill(-c.Process.Pid, syscall.SIGINT)
	err := c.Wait()
	if err != nil || !strings.Contains(out.String(), "trapped") || !strings.Contains(out.String(), "done") {
		t.Fatalf("agent must survive SIGINT: err=%v\n%s", err, out.String())
	}
}

func TestProjectsPath(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("a/app-next", true)
	f.mkrepo("a/app-mobile", true)
	real, _ := filepath.EvalSymlinks(p)
	out, _, code := f.run("projects", "--path", "app-n")
	if code != 0 || strings.TrimSpace(out) != real {
		t.Fatalf("%d %q", code, out)
	}
	if _, _, code := f.run("projects", "--path", "app-"); code != 2 {
		t.Fatal("ambiguous → 2")
	}
}

func TestGoAndLauncherUsageErrors(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("a/r", true)
	for _, c := range [][]string{{"go"}, {"go", "r", "extra"}, {"go", "r", "-a"}, {"go", "r", "--bogus"}, {"launcher", "claude"}, {"launcher", "nope", "run", p}} {
		if _, _, code := f.run(c...); code != 2 {
			t.Errorf("%v should exit 2, got %d", c, code)
		}
	}
	t.Setenv("PATH", filepath.Join(f.Home, "bin"))
	os.Remove(filepath.Join(f.Home, "bin", "claude"))
	if _, errs, code := f.run("launcher", "claude", "run", p); code != 3 || !strings.Contains(errs, "claude not found") {
		t.Errorf("missing binary: %d %q", code, errs)
	}
	f.bin("claude", "")
	if _, errs, code := f.run("launcher", "claude", "run", filepath.Join(f.Home, "nope")); code != 1 || errs == "" {
		t.Errorf("bad dir must fail before exec: %d %q", code, errs)
	}
	if _, _, code := f.run("go", "r", "-a=nope"); code != 2 {
		t.Error("-a=x form")
	}
}

// Sous go started from inside an earlier sous go session must hand
// the agent this project's context, not the inherited file.
func TestGoReplacesInheritedHereFile(t *testing.T) {
	f := fixture(t)
	f.mkrepo("a/ios-app", true)
	f.bin("claude", "echo \"file=$SOUS_HERE_FILE\"")
	c := sousCmd(f, f.Home, "go", "ios")
	c.Env = append(c.Env, "SOUS_HERE_FILE=/stale/from-parent")
	out, err := c.CombinedOutput()
	if err != nil || strings.Contains(string(out), "/stale/from-parent") || !strings.Contains(string(out), "/here/") {
		t.Fatalf("%v\n%s", err, out)
	}
}

// sous go keeps one resume file per project under SOUS_HOME, replaced each
// time, instead of leaving a new temp file behind on every run.
func TestGoReusesOneHereFilePerProject(t *testing.T) {
	f := fixture(t)
	f.mkrepo("a/ios-app", true)
	f.bin("claude", "echo \"file=$SOUS_HERE_FILE\"")
	var files []string
	for range 2 {
		out, err := sousCmd(f, f.Home, "go", "ios").CombinedOutput()
		if err != nil {
			t.Fatalf("%v %s", err, out)
		}
		_, after, _ := strings.Cut(string(out), "file=")
		files = append(files, strings.TrimSpace(after))
	}
	if files[0] != files[1] || !strings.HasPrefix(files[0], filepath.Join(f.SousHome, "here")) {
		t.Fatalf("%q", files)
	}
}

func TestGoDotMeansThisProject(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("a/ios-app", true)
	f.bin("claude", "echo \"claude in $PWD\"")
	out, err := sousCmd(f, p, "go", ".").CombinedOutput()
	real, _ := filepath.EvalSymlinks(p)
	if err != nil || !strings.Contains(string(out), "claude in "+real) {
		t.Fatalf("%v\n%s", err, out)
	}
}

// sous go --where prints the project folder sous go would use, parsed the
// same way, and starts nothing. The zsh wrapper relies on it.
func TestGoWherePrintsTheProjectOnly(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("a/ios-app", true)
	real, _ := filepath.EvalSymlinks(p)
	for _, args := range [][]string{{"go", "--where", "ios"}, {"go", "-a", "codex", "--where", "ios"}, {"go", "ios", "--agent=codex", "--where"}} {
		out, errs, code := f.run(args...)
		if code != 0 || strings.TrimSpace(out) != real {
			t.Errorf("%v: %d %q %q", args, code, out, errs)
		}
	}
	if _, _, code := f.run("go", "--where", "--bogus", "ios"); code != 2 {
		t.Fatal("bad arguments fail as they would for go")
	}
}

// go --in uses the folder given and looks nothing up.
func TestGoInUsesTheFolderGiven(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("a/ios-app", true)
	f.bin("claude", "echo \"claude in $PWD\"")
	out, err := sousCmd(f, f.Home, "go", "--in", p, "no-such-name").CombinedOutput()
	real, _ := filepath.EvalSymlinks(p)
	if err != nil || !strings.Contains(string(out), "claude in "+real) {
		t.Fatalf("%v\n%s", err, out)
	}
}
