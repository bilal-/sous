package launcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bilal-/sous/internal/plugin"
)

func TestLaunchersAndExecArgv(t *testing.T) {
	bin := t.TempDir()
	third := filepath.Join(bin, "sous-launcher-editor")
	os.WriteFile(third, []byte("#!/bin/sh\n"), 0o755)
	ls := Registry.Discover("/bin/sous", []string{"claude", "codex"}, []string{third, filepath.Join(bin, "not-a-launcher")})
	if len(ls) != 3 || ls[0].Name != "claude" || ls[0].Argv[1] != "launcher" || ls[2].Name != "editor" || ls[2].Argv[0] != third {
		t.Fatalf("%+v", ls)
	}
	l, _ := plugin.Find(ls, "editor")
	argv0, argv, err := ExecArgv(l, "/proj")
	if err != nil || argv0 != third || strings.Join(argv, " ") != third+" run /proj" {
		t.Fatalf("%q %v %v", argv0, argv, err)
	}
	t.Setenv("PATH", bin)
	os.WriteFile(filepath.Join(bin, "sous"), []byte("#!/bin/sh\n"), 0o755)
	l, _ = plugin.Find(Registry.Discover("sous", []string{"claude"}, nil), "claude")
	if _, argv, err := ExecArgv(l, "/proj"); err != nil || strings.Join(argv[1:], " ") != "launcher claude run /proj" {
		t.Fatalf("%v %v", argv, err)
	}
	if _, _, err := ExecArgv(Launcher{Name: "gone", Argv: []string{"/nope/bin"}}, "/p"); err == nil || !strings.Contains(err.Error(), "gone") {
		t.Fatalf("missing binary must name the launcher: %v", err)
	}
	if _, ok := plugin.Find(ls, "nope"); ok {
		t.Fatal("unknown")
	}
}

// A built in launcher whose agent is not installed is not set up: exit 3,
// as the contract says, saying what is missing.
func TestBuiltinLauncherNotSetUp(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var out, errb strings.Builder
	code := Registry.Serve(Deps{Exec: func(string, string, []string) error { t.Fatal("must not exec"); return nil }},
		[]string{"claude", "run", "/p"}, nil, &out, &errb)
	if code != 3 || !strings.Contains(errb.String(), "claude not found") {
		t.Fatalf("%d %q", code, errb.String())
	}
}
