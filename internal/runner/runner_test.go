package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRunner writes a sous-runner-fake whose calls are shell case arms.
func fakeRunner(t *testing.T, body string) Runner {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sous-runner-fake")
	os.WriteFile(p, []byte("#!/bin/sh\ncase \"$1\" in\n"+body+"\nesac\n"), 0o755)
	return Runners("", nil, []string{p})[0]
}

func TestClientReadsTheContract(t *testing.T) {
	ctx := context.Background()
	r := fakeRunner(t, `start) cat > "$(dirname "$0")/req"; echo fake:1;;
status) echo '{"v":0,"state":"needs_you","text":"which fixture?"}';;
reply) cat > "$(dirname "$0")/answer";;
stop) exit 0;;
clean) exit 2;;`)
	ref, err := Start(ctx, r, Request{ID: 7, UID: "0123456789ab", Project: "/code/acme/billing", Brief: "fix it"})
	if err != nil || ref != "fake:1" {
		t.Fatalf("start: %q %v", ref, err)
	}
	req, _ := os.ReadFile(filepath.Join(filepath.Dir(r.Argv[0]), "req"))
	if !strings.Contains(string(req), `"v":0`) || !strings.Contains(string(req), `"id":7`) || !strings.Contains(string(req), `"brief":"fix it"`) {
		t.Fatalf("request: %s", req)
	}
	st, err := GetStatus(ctx, r, "/code/acme/billing", ref)
	if err != nil || st.State != NeedsYou || st.Text != "which fixture?" {
		t.Fatalf("status: %+v %v", st, err)
	}
	if err := Reply(ctx, r, "/code/acme/billing", ref, "use main's"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(filepath.Dir(r.Argv[0]), "answer")); string(b) != "use main's" {
		t.Fatalf("answer: %q", b)
	}
	if err := Stop(ctx, r, "/code/acme/billing", ref); err != nil {
		t.Fatal(err)
	}
	if err := Clean(ctx, r, "/code/acme/billing", ref); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("clean unsupported: %v", err)
	}
}

// Never guess: junk, an unknown state, a newer v, or exit 1 are errors.
func TestStatusNeverGuesses(t *testing.T) {
	for name, body := range map[string]string{
		"junk":    `status) echo nope;;`,
		"unknown": `status) echo '{"v":0,"state":"sleeping"}';;`,
		"newer":   `status) echo '{"v":1,"state":"done"}';;`,
		"failed":  `status) echo "cannot reach it" >&2; exit 1;;`,
	} {
		r := fakeRunner(t, body)
		if st, err := GetStatus(context.Background(), r, "/p", "fake:1"); err == nil {
			t.Errorf("%s: %+v", name, st)
		}
	}
}

func TestStartNotSetUp(t *testing.T) {
	r := fakeRunner(t, `start) echo "fake is not installed" >&2; exit 3;;`)
	if _, err := Start(context.Background(), r, Request{ID: 1, UID: "u", Project: "/p", Brief: "b"}); !errors.Is(err, ErrNotSetUp) || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("%v", err)
	}
}

func TestByRef(t *testing.T) {
	rs := []Runner{{Name: "claude"}, {Name: "orchid"}}
	if r, err := ByRef(rs, "orchid:T3"); err != nil || r.Name != "orchid" {
		t.Fatal(r, err)
	}
	if _, err := ByRef(rs, "gone:1"); err == nil {
		t.Fatal("unknown runner")
	}
}
