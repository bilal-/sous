package plugin

import (
	"context"
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
