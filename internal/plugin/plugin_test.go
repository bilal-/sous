package plugin

import (
	"context"
	"errors"
	"fmt"
	"github.com/bilal-/sous/internal/testutil"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var reg = Registry[string]{Axis: "backend", Builtins: []Builtin[string]{
	{Name: "markdown", Offline: true, Ops: func(d string) map[string]Op {
		return map[string]Op{"status": func(args []string, _ io.Reader, stdout, _ io.Writer) int {
			fmt.Fprintln(stdout, d, strings.Join(args, " "))
			return ExitOK
		}}
	}},
	{Name: "github"},
}}

func TestRegistryDiscover(t *testing.T) {
	got := reg.Discover("/bin/sous", reg.Names(), []string{"/x/sous-backend-jira", "/x/sous-signal-git", "/x/plain", "/x/sous-backend-"})
	want := []Plugin{
		{Name: "markdown", Argv: []string{"/bin/sous", "backend", "markdown"}, Offline: true},
		{Name: "github", Argv: []string{"/bin/sous", "backend", "github"}},
		{Name: "jira", Argv: []string{"/x/sous-backend-jira"}},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %+v", got)
	}
	if off := reg.Offline("/bin/sous"); len(off) != 1 || off[0].Name != "markdown" || !off[0].Offline {
		t.Fatal(off)
	}
	if all := reg.All("/bin/sous", []string{"/x/sous-backend-jira"}); fmt.Sprint(all) != fmt.Sprint(want) {
		t.Fatal(all)
	}
	if p, ok := ByRef(got, "jira:OPS-12"); !ok || p.Name != "jira" {
		t.Fatal(p, ok)
	}
	for _, ref := range []string{"beads:1", "no-colon"} {
		if _, ok := ByRef(got, ref); ok {
			t.Errorf("%s matched", ref)
		}
	}
}

// The door answers a built in's call as a program would, and refuses
// anything else with exit 2 and a usage line naming what there is.
func TestRegistryServe(t *testing.T) {
	var out, errb strings.Builder
	if code := reg.Serve("dep", []string{"markdown", "status", "/p", "md:1"}, nil, &out, &errb); code != 0 || out.String() != "dep /p md:1\n" {
		t.Fatalf("%d %q %q", code, out.String(), errb.String())
	}
	for _, args := range [][]string{{}, {"markdown"}, {"jira", "status"}, {"markdown", "close"}} {
		errb.Reset()
		if code := reg.Serve("dep", args, nil, &out, &errb); code != ExitRefused || !strings.Contains(errb.String(), "usage: sous backend") {
			t.Errorf("%v: %d %q", args, code, errb.String())
		}
	}
	errb.Reset()
	reg.Serve("dep", []string{"markdown", "close"}, nil, &out, &errb)
	if !strings.Contains(errb.String(), "markdown status") {
		t.Errorf("an unknown call lists the calls there are: %q", errb.String())
	}
}

func TestExit(t *testing.T) {
	for _, tc := range []struct {
		err    error
		code   int
		stderr string
	}{
		{nil, 0, ""},
		{ErrUnsupported, 2, ""},
		{fmt.Errorf("reply: %w", ErrUnsupported), 2, ""},
		{ErrNo, 1, ""},
		{errors.New("boom"), 1, "boom\n"},
		{fmt.Errorf("%w: claude is not installed", ErrNotSetUp), 3, "not set up: claude is not installed\n"},
	} {
		var errb strings.Builder
		if code := Exit(tc.err, &errb); code != tc.code || errb.String() != tc.stderr {
			t.Errorf("%v: %d %q", tc.err, code, errb.String())
		}
	}
}

func TestRequestDoesNotEscapeHTML(t *testing.T) {
	if got := string(Request(map[string]string{"text": "a <b> & c"})); got != `{"text":"a <b> & c"}`+"\n" {
		t.Fatal(got)
	}
}

func TestExecOutcomes(t *testing.T) {
	dir := t.TempDir()
	ok := testutil.Script(t, dir, "ok", `read -r x; echo "got $x $1"; echo warn >&2`)
	fails := testutil.Script(t, dir, "fails", `echo boom >&2; exit 3`)
	forker := testutil.Script(t, dir, "forker", `sleep 30`)
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
	if a, err := Call(ctx, "x", "op", script("echo hi"), nil, time.Second); a.Out != "hi" || a.Code != 0 || err != nil {
		t.Fatalf("ok: %+v %v", a, err)
	}
	if a, err := Call(ctx, "x", "op", script("echo why >&2; exit 2"), nil, time.Second); a.Code != 2 || a.Reason != "why" || err != nil {
		t.Fatalf("2: %+v %v", a, err)
	}
	if _, err := Call(ctx, "x", "op", script("exit 1"), nil, time.Second); !errors.Is(err, ErrNo) {
		t.Fatalf("silent 1: %v", err)
	}
	if _, err := Call(ctx, "x", "op", script("echo nope >&2; exit 1"), nil, time.Second); err == nil || err.Error() != "nope" {
		t.Fatalf("1 with reason: %v", err)
	}
	if _, err := Call(ctx, "x", "op", script("exit 7"), nil, time.Second); err == nil || !strings.Contains(err.Error(), "exit 7") {
		t.Fatalf("7: %v", err)
	}
	if _, err := Call(ctx, "x", "op", script("sleep 5"), nil, 200*time.Millisecond); err == nil || !strings.Contains(err.Error(), "x op: timed out") {
		t.Fatalf("timeout: %v", err)
	}
}

func TestRunnerIsAnAxis(t *testing.T) {
	if !Named("/p/sous-runner-orchid") || Named("/p/sous-runner-") {
		t.Fatal("runner plugins are named sous-runner-<name>")
	}
}

// A request is refused with usage when it is not JSON or not valid, and
// with the version when it is newer than this contract.
func TestDecodeRequest(t *testing.T) {
	type req struct {
		V  int    `json:"v"`
		ID string `json:"id"`
	}
	for _, tc := range []struct {
		in, stderr string
		ok         bool
	}{
		{`{"v":0,"id":"a"}`, "", true},
		{`not json`, "usage: file", false},
		{`{"v":0}`, "usage: file", false},
		{`{"v":1,"id":"a"}`, "file: contract v1 is newer than this one speaks (v0)", false},
	} {
		var r req
		var errb strings.Builder
		code, ok := DecodeRequest(strings.NewReader(tc.in), &r, func() int { return r.V }, func() bool { return r.ID != "" }, "file", "file", &errb)
		if ok != tc.ok || ok != (code == ExitOK) || !strings.Contains(errb.String(), tc.stderr) {
			t.Errorf("%s: %d %v %q", tc.in, code, ok, errb.String())
		}
	}
}

func TestRefAnswer(t *testing.T) {
	if ref, err := RefAnswer("jira", "file", Answer{Out: "jira:OPS-1"}, nil); err != nil || ref != "jira:OPS-1" {
		t.Fatal(ref, err)
	}
	if _, err := RefAnswer("jira", "file", Answer{Code: 2, Reason: "no project key"}, nil); err == nil || !strings.Contains(err.Error(), "no project key") {
		t.Fatal(err)
	}
	if _, err := RefAnswer("jira", "file", Answer{}, nil); err == nil || !strings.Contains(err.Error(), "printed no ref") {
		t.Fatal(err)
	}
	if Names() != "sous-signal-, sous-backend-, sous-launcher- or sous-runner-" {
		t.Fatal(Names())
	}
}

// Exit 3 is not set up, said once whether or not the plugin said it.
func TestCallNotSetUpIsSaidOnce(t *testing.T) {
	dir := t.TempDir()
	for _, body := range []string{`echo "claude is not installed" >&2; exit 3`, `echo "not set up: claude is not installed" >&2; exit 3`} {
		_, err := Call(context.Background(), "claude", "start", []string{testutil.Script(t, dir, "p", body)}, nil, time.Second)
		if !errors.Is(err, ErrNotSetUp) || err.Error() != "not set up: claude is not installed" {
			t.Errorf("%q", err)
		}
	}
}
