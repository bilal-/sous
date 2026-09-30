package plugin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func script(t *testing.T, dir, name, body string) string {
	p := filepath.Join(dir, name)
	os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755)
	return p
}

func TestDiscover(t *testing.T) {
	got := Discover("/bin/sous", "backend", []string{"markdown"}, []string{"/x/sous-backend-jira", "/x/sous-signal-git", "/x/plain"})
	if len(got) != 2 || got[0].Name != "markdown" || strings.Join(got[0].Argv, " ") != "/bin/sous backend markdown" || got[1].Name != "jira" || got[1].Argv[0] != "/x/sous-backend-jira" {
		t.Fatalf("%+v", got)
	}
}

func TestExecOutcomes(t *testing.T) {
	dir := t.TempDir()
	ok := script(t, dir, "ok", `read -r x; echo "got $x $1"; echo warn >&2`)
	fails := script(t, dir, "fails", `echo boom >&2; exit 3`)
	forker := script(t, dir, "forker", `sleep 30`)
	r := Exec(context.Background(), []string{ok, "arg"}, []byte("in\n"), time.Second)
	if r.Err != nil || r.Code != 0 || strings.TrimSpace(r.Stdout) != "got in arg" || strings.TrimSpace(r.Stderr) != "warn" {
		t.Fatalf("%+v", r)
	}
	r = Exec(context.Background(), []string{fails}, nil, time.Second)
	if r.Code != 3 || r.Err != nil || strings.TrimSpace(r.Stderr) != "boom" {
		t.Fatalf("non-zero exit is a code, not an error: %+v", r)
	}
	start := time.Now()
	r = Exec(context.Background(), []string{forker}, nil, 300*time.Millisecond)
	if !r.TimedOut || time.Since(start) > 5*time.Second {
		t.Fatalf("timeout must kill the group: %+v after %v", r, time.Since(start))
	}
	r = Exec(context.Background(), []string{"/nope/binary"}, nil, time.Second)
	if r.Err == nil {
		t.Fatal("missing binary is an error")
	}
}

func TestCallReadsExitCodesOneWay(t *testing.T) {
	dir := t.TempDir()
	n := 0
	script := func(body string) []string {
		n++
		p := filepath.Join(dir, fmt.Sprintf("p%d", n))
		os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755)
		return []string{p}
	}
	ctx := context.Background()
	if out, code, err := Call(ctx, "x", "op", script("echo hi"), nil, time.Second); out != "hi" || code != 0 || err != nil {
		t.Fatalf("ok: %q %d %v", out, code, err)
	}
	if _, code, err := Call(ctx, "x", "op", script("exit 2"), nil, time.Second); code != 2 || err != nil {
		t.Fatalf("2: %d %v", code, err)
	}
	if _, _, err := Call(ctx, "x", "op", script("exit 1"), nil, time.Second); !errors.Is(err, ErrNo) {
		t.Fatalf("silent 1: %v", err)
	}
	if _, _, err := Call(ctx, "x", "op", script("echo nope >&2; exit 1"), nil, time.Second); err == nil || err.Error() != "nope" {
		t.Fatalf("1 with reason: %v", err)
	}
	if _, _, err := Call(ctx, "x", "op", script("exit 7"), nil, time.Second); err == nil || !strings.Contains(err.Error(), "exit 7") {
		t.Fatalf("7: %v", err)
	}
	if _, _, err := Call(ctx, "x", "op", script("sleep 5"), nil, 200*time.Millisecond); err == nil || !strings.Contains(err.Error(), "x op: timed out") {
		t.Fatalf("timeout: %v", err)
	}
}

func TestRunnerIsAnAxis(t *testing.T) {
	if !Named("/p/sous-runner-orchid") || Named("/p/sous-runner-") {
		t.Fatal("runner plugins are named sous-runner-<name>")
	}
}
