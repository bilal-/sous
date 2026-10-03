package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionAndHelp(t *testing.T) {
	f := fixture(t)
	out, _, code := f.run("version")
	if code != 0 || out != "sous "+Version+"\n" {
		t.Fatalf("version: code=%d out=%q", code, out)
	}
	out, _, code = f.run("help")
	if code != 0 || !strings.Contains(out, "sous note") || !strings.Contains(out, "sous here") {
		t.Fatalf("help: code=%d out=%q", code, out)
	}
	out, _, _ = f.run("--help")
	if !strings.Contains(out, "sous note") {
		t.Fatal("--help should print usage")
	}
}

func TestUnknownThingsExit2(t *testing.T) {
	f := fixture(t)
	_, errs, code := f.run("frobnicate")
	if code != 2 || !strings.Contains(errs, "frobnicate") {
		t.Fatalf("unknown subcommand: code=%d err=%q", code, errs)
	}
	_, errs, code = f.run("--frob")
	if code != 2 || !strings.Contains(errs, "--frob") {
		t.Fatalf("unknown flag: code=%d err=%q", code, errs)
	}
}

func TestProjects(t *testing.T) {
	f := fixture(t)
	f.mkrepo("oss/app-next", true)
	f.mkrepo("studio/billing", true)
	f.mkrepo("scratch/junk", true)
	f.writeConfig("roots = [\"" + f.WS + "\"]\nignore = [\"scratch/*\"]\n")

	out, _, code := f.run("projects")
	if code != 0 || !strings.Contains(out, "oss") || !strings.Contains(out, "app-next") || strings.Contains(out, "junk") {
		t.Fatalf("projects: %d %q", code, out)
	}
	out, _, _ = f.run("--json", "projects")
	if !strings.Contains(out, `"path"`) || !strings.Contains(out, `"remote": null`) {
		t.Fatalf("json: %q", out)
	}
	out, _, _ = f.run("projects", "--root", filepath.Join(f.WS, "studio"))
	if strings.Contains(out, "app") || !strings.Contains(out, "billing") {
		t.Fatalf("--root: %q", out)
	}
	os.Remove(filepath.Join(f.SousHome, "config.toml"))
	_, errs, code := f.run("projects")
	if code != 1 || !strings.Contains(errs, "sous setup") {
		t.Fatalf("no config no root: %d %q", code, errs)
	}
	_, _, code = f.run("projects", "--root", f.WS)
	if code != 0 {
		t.Fatal("explicit --root works without config")
	}
}

func TestProjectsFilterAndResolve(t *testing.T) {
	f := fixture(t)
	for _, r := range []string{"oss/app-next", "oss/app-mobile", "studio/billing"} {
		f.mkrepo(r, true)
	}
	out, _, code := f.run("projects", "blg")
	if code != 0 || !strings.Contains(out, "billing") || strings.Contains(out, "app") {
		t.Fatalf("filter: %d %q", code, out)
	}
	out, _, _ = f.run("projects", "app-")
	if strings.Count(out, "app") != 2 {
		t.Fatalf("ambiguous filter shows all: %q", out)
	}
	_, errs, code := f.run("projects", "zzz")
	if code != 2 || !strings.Contains(errs, "no project matches") {
		t.Fatalf("no match: %d %q", code, errs)
	}
}

// os.Executable returns the symlink when sous is installed via ln -s; hooks
// and the printed shell path must use the real binary's location.
// #2: a scoped board is not "the board"; it must not become what
// the zsh surface prints.
// #6: store failures are exit 1, not the usage code 2 — an agent
// seeing 2 would assume its arguments were wrong.
// #7: --json / --brief are accepted after the verb too.
// The session-start guard must cover repo resolution too, so a
// hanging git cannot hang the agent session.
// `--cached` with no board must bootstrap a refresh rather than
// tell the forgetting user to remember; exit 3 so the shell doesn't stamp.
// Scoped boards match threads by remote too, and relative folder
// arguments behave like absolute ones.
// AXI: fail loud on flags a verb does not take, such as --json on a
// write verb.
// A release binary has no checkout beside it: the zsh snippet is embedded
// and written to SOUS_HOME by setup, which prints that path.
// Ctrl-C must reach the agent, not a sous wrapper.
// An unavailable root plus one filed thread with a remote must not
// panic (Discover was called with a nil warn writer).
// A backend that fails or times out is "status unavailable", not
// "(ref missing)". The ticket may well exist.
// `sous file` can force a worktree and re-file a lost marker.
// A repo moved after a note was filed: the thread resolves by remote to the
// new path, so --close writes to the right FOLLOWUPS.md.
// In-process coverage for the parts of go/launcher that return before exec.
