package backend

import (
	"bytes"
	"context"
	"errors"
	"io"
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

func TestBackendsNamingAndByRef(t *testing.T) {
	bs := Backends("/bin/sous", []string{"markdown"}, []string{"/x/sous-backend-jira", "/x/not-one"})
	if len(bs) != 2 || bs[0].Name != "markdown" || bs[0].Argv[1] != "backend" || bs[1].Name != "jira" || bs[1].Argv[0] != "/x/sous-backend-jira" {
		t.Fatalf("%+v", bs)
	}
	if b, err := ByRef(bs, "md:FOLLOWUPS.md:7:a1f3"); err != nil || b.Name != "markdown" {
		t.Fatal(b, err)
	}
	if b, err := ByRef(bs, "jira:WAS-12"); err != nil || b.Name != "jira" {
		t.Fatal(b, err)
	}
	if _, err := ByRef(bs, "beads:x"); !errors.Is(err, ErrUnknownBackend) {
		t.Fatal(err)
	}
}

func TestDetectOrderOverrideAndProbeFailure(t *testing.T) {
	dir := t.TempDir()
	no := script(t, dir, "sous-backend-no", `exit 1`)
	yes := script(t, dir, "sous-backend-yes", `[ "$1" = detect ] && [ "$2" = "/proj" ] && exit 0; exit 1`)
	broken := script(t, dir, "sous-backend-broken", `echo "cannot probe" >&2; exit 3`)
	bs := Backends("", nil, []string{broken, no, yes})
	var warn bytes.Buffer
	b, err := Detect(context.Background(), bs, "/proj", "", &warn)
	if err != nil || b.Name != "yes" || !strings.Contains(warn.String(), "broken") || !strings.Contains(warn.String(), "cannot probe") {
		t.Fatalf("first applying backend wins, probe failure warned: %+v %v %q", b, err, warn.String())
	}
	if b, _ := Detect(context.Background(), bs, "/proj", "no", nil); b.Name != "no" {
		t.Fatalf("override wins even if detect fails: %+v", b)
	}
	if _, err := Detect(context.Background(), bs, "/proj", "jira", nil); !errors.Is(err, ErrUnknownBackend) {
		t.Fatalf("declared-but-missing backend is an error, not local: %v", err)
	}
	if b, _ := Detect(context.Background(), Backends("", nil, []string{no}), "/proj", "", nil); b.Name != "local" {
		t.Fatalf("nothing applies → local: %+v", b)
	}
}

func TestOpsViaSubprocess(t *testing.T) {
	dir := t.TempDir()
	fake := script(t, dir, "sous-backend-fake", `
case "$1" in
  file) read -r body; out=$(printf '%s' "$body" | sed 's/.*"project":"\([^"]*\)".*/\1/'); echo "$body" > "$out.req"; echo "fake:42";;
  status) [ "$2" = /proj ] && [ "$3" = fake:42 ] && echo open || echo unknown;;
  close) [ "$3" = fake:42 ] && exit 0; echo "no such ref" >&2; exit 1;;
  url) exit 2;;
esac`)
	b := Backends("", nil, []string{fake})[0]
	reqOut := filepath.Join(dir, "out")
	ref, err := File(context.Background(), b, Request{ID: 7, UID: "000000000007", Project: reqOut, Text: "a <b> & \"c\"", Kind: "idea"})
	if err != nil || ref != "fake:42" {
		t.Fatal(ref, err)
	}
	got, _ := os.ReadFile(reqOut + ".req")
	if string(got) != `{"v":0,"id":7,"uid":"000000000007","project":"`+reqOut+`","text":"a <b> & \"c\"","kind":"idea"}`+"\n" {
		t.Fatalf("request JSON must not HTML-escape: %s", got)
	}
	if st, err := Status(context.Background(), b, "/proj", "fake:42"); err != nil || st != "open" {
		t.Fatal(st, err)
	}
	if st, _ := Status(context.Background(), b, "/proj", "other"); st != "unknown" {
		t.Fatal(st)
	}
	if err := Close(context.Background(), b, "/proj", "fake:42"); err != nil {
		t.Fatal(err)
	}
	if err := Close(context.Background(), b, "/proj", "nope"); err == nil || err.Error() != "no such ref" {
		t.Fatalf("stderr becomes the error: %v", err)
	}
	if _, ok, err := URL(context.Background(), b, "/proj", "fake:42"); ok || err != nil {
		t.Fatalf("exit 2 = unsupported: ok=%v err=%v", ok, err)
	}
	if _, err := File(context.Background(), Local, Request{}); err == nil {
		t.Fatal("Local has no upstream")
	}
}

func TestOpTimeoutKillsGroup(t *testing.T) {
	dir := t.TempDir()
	slow := script(t, dir, "sous-backend-slow", `sleep 30`)
	b := Backends("", nil, []string{slow})[0]
	old := timeout
	timeout = 300 * time.Millisecond
	defer func() { timeout = old }()
	start := time.Now()
	_, err := Status(context.Background(), b, "/p", "slow:1")
	if err == nil || !strings.Contains(err.Error(), "timed out") || time.Since(start) > 5*time.Second {
		t.Fatalf("err=%v after %v", err, time.Since(start))
	}
}

func TestURLSupported(t *testing.T) {
	dir := t.TempDir()
	b := Backends("", nil, []string{script(t, dir, "sous-backend-web", `[ "$1" = url ] && echo "https://x/$3"; exit 0`)})[0]
	u, ok, err := URL(context.Background(), b, "/p", "web:1")
	if err != nil || !ok || u != "https://x/web:1" {
		t.Fatal(u, ok, err)
	}
	if _, err := Detect(context.Background(), nil, "/p", "", nil); err != nil {
		t.Fatal("no backends → local, no error")
	}
}

// A backend that says "not applicable" with a reason (exit 1 + stderr) must
// get that reason to the user, or "no tracker" is a mystery.
func TestDetectSurfacesNotApplicableReason(t *testing.T) {
	dir := t.TempDir()
	why := script(t, dir, "sous-backend-why", `echo "github: gh not logged in" >&2; exit 1`)
	var warn bytes.Buffer
	if b, _ := Detect(context.Background(), Backends("", nil, []string{why}), "/p", "", &warn); b.Name != "local" || !strings.Contains(warn.String(), "gh not logged in") {
		t.Fatalf("%+v %q", b, warn.String())
	}
}

// Review F3: the file request carries the contract version, like a signal
// line; a backend refuses a version it does not speak.
func TestFileRequestCarriesContractVersion(t *testing.T) {
	var got bytes.Buffer
	var impl recordingImpl
	file := Ops(&impl)["file"]
	if code := file(nil, strings.NewReader(`{"v":1,"id":3,"project":"/p","text":"x","kind":"me"}`), &got, &got); code != 2 || !strings.Contains(got.String(), "v1") || impl.filed {
		t.Fatalf("future version: code=%d %q filed=%v", code, got.String(), impl.filed)
	}
	dir := t.TempDir()
	echo := script(t, dir, "sous-backend-echo", `cat > "`+dir+`/req"; echo echo:1`)
	File(context.Background(), Backends("", nil, []string{echo})[0], Request{ID: 3, UID: "000000000003", Project: "/p", Text: "x", Kind: "me"})
	if b, _ := os.ReadFile(filepath.Join(dir, "req")); !strings.HasPrefix(string(b), `{"v":0,`) {
		t.Fatalf("request must lead with v: %s", b)
	}
}

type recordingImpl struct{ filed bool }

func (r *recordingImpl) Detect(string, io.Writer) bool         { return true }
func (r *recordingImpl) File(Request) (string, error)          { r.filed = true; return "x:1", nil }
func (r *recordingImpl) Status(string, string) (string, error) { return "open", nil }
func (r *recordingImpl) Close(string, string) error            { return nil }
func (r *recordingImpl) URL(string, string) (string, error)    { return "", ErrUnsupported }
