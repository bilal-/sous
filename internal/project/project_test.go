package project

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/testutil"
)

func mkrepo(t *testing.T, dir string) { testutil.Repo(t, dir, true, "") }

func TestDiscover(t *testing.T) {
	ws := t.TempDir()
	for _, r := range []string{"oss/app-next", "oss/app-mobile", "studio/billing", "personal/my project", "toplevel-repo", "scratch/ignored-repo"} {
		mkrepo(t, filepath.Join(ws, r))
	}
	mkrepo(t, filepath.Join(ws, "oss/app-next/vendor/nested")) // inside a repo
	os.MkdirAll(filepath.Join(ws, "oss/not-a-repo"), 0o755)
	os.MkdirAll(filepath.Join(ws, "oss/yarn--123"), 0o755)
	exec.Command("git", "-C", filepath.Join(ws, "studio/billing"), "remote", "add", "origin", "git@github.com:studio/billing.git").Run()
	exec.Command("git", "-C", filepath.Join(ws, "oss/app-next"), "remote", "add", "origin", "https://github.com/oss/app-next.git").Run()

	var warn bytes.Buffer
	ps, unavailable := Discover([]string{ws, filepath.Join(ws, "nope")}, []string{"scratch/*"}, &warn)
	if unavailable != 1 || !bytes.Contains(warn.Bytes(), []byte("nope")) {
		t.Fatalf("missing root: unavailable=%d warn=%q", unavailable, warn.String())
	}
	names := map[string]Project{}
	for _, p := range ps {
		names[p.Name] = p
	}
	for _, want := range []string{"app-next", "app-mobile", "billing", "my project", "toplevel-repo"} {
		if _, ok := names[want]; !ok {
			t.Errorf("missing %s in %v", want, names)
		}
	}
	for _, no := range []string{"ignored-repo", "nested", "not-a-repo", "yarn--123"} {
		if _, ok := names[no]; ok {
			t.Errorf("should not list %s", no)
		}
	}
	if len(ps) != 5 {
		t.Fatalf("want 5 projects, got %d", len(ps))
	}
	if r := names["billing"].Remote; r == nil || *r != "github.com/studio/billing" {
		t.Errorf("ssh remote: %v", r)
	}
	if r := names["app-next"].Remote; r == nil || *r != "github.com/oss/app-next" {
		t.Errorf("https remote: %v", r)
	}
	if names["app-mobile"].Remote != nil {
		t.Error("no remote should be nil")
	}
	if names["toplevel-repo"].Org != filepath.Base(ws) {
		t.Errorf("depth-1 org = %q", names["toplevel-repo"].Org)
	}
	if names["app-next"].LastCommit == nil {
		t.Error("last_commit missing")
	}
}

func TestForPath(t *testing.T) {
	ws := t.TempDir()
	repo := filepath.Join(ws, "a/repo")
	mkrepo(t, repo)
	os.MkdirAll(filepath.Join(repo, "src/deep"), 0o755)
	root, ok := ForPath(filepath.Join(repo, "src/deep"))
	realRepo, _ := filepath.EvalSymlinks(repo)
	if !ok || root != realRepo {
		t.Fatalf("ForPath: %q %v", root, ok)
	}
	if _, ok := ForPath(ws); ok {
		t.Fatal("ws is not in a repo")
	}
}

func TestRemoteForms(t *testing.T) {
	cases := map[string]string{
		"git@github.com:Org/Repo.git":      "github.com/Org/Repo",
		"https://github.com/org/repo":      "github.com/org/repo",
		"https://user@gitlab.com/o/r.git":  "gitlab.com/o/r",
		"ssh://git@github.com/o/r.git":     "github.com/o/r",
		"ssh://git@github.com:22/o/r.git":  "github.com/o/r",
		"https://git.example.org:8443/a/b": "git.example.org/a/b",
		"":                                 "",
	}
	for in, want := range cases {
		if got := normalizeRemote(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

// Review fix #3: symlinked repos and org folders under a root are projects.
func TestDiscoverFollowsSymlinks(t *testing.T) {
	ws := t.TempDir()
	elsewhere := t.TempDir()
	mkrepo(t, filepath.Join(elsewhere, "linked-repo"))
	mkrepo(t, filepath.Join(elsewhere, "org", "inner"))
	os.MkdirAll(filepath.Join(ws, "org1"), 0o755)
	os.Symlink(filepath.Join(elsewhere, "linked-repo"), filepath.Join(ws, "org1", "linked"))
	os.Symlink(filepath.Join(elsewhere, "org"), filepath.Join(ws, "orglink"))
	ps, _ := Discover([]string{ws}, nil, io.Discard)
	names := map[string]bool{}
	for _, p := range ps {
		names[p.Name] = true
	}
	if !names["linked"] || !names["inner"] {
		t.Fatalf("symlinked entries missing: %v", names)
	}
}

// Review 2 C2 / I6: relative roots must be made absolute (ids and thread
// matching key on absolute paths); a root that is itself a repo is a project.
func TestDiscoverAbsoluteAndRootRepo(t *testing.T) {
	ws := t.TempDir()
	mkrepo(t, filepath.Join(ws, "org", "r"))
	wd, _ := os.Getwd()
	os.Chdir(ws)
	defer os.Chdir(wd)
	ps, _ := Discover([]string{"."}, nil, io.Discard)
	if len(ps) != 1 || !filepath.IsAbs(ps[0].Path) {
		t.Fatalf("relative root must yield absolute paths: %+v", ps)
	}
	single := filepath.Join(ws, "org", "r")
	ps, _ = Discover([]string{single}, nil, io.Discard)
	if len(ps) != 1 || ps[0].Name != "r" {
		t.Fatalf("a root that is a repo is a project: %+v", ps)
	}
}

func TestAgeBuckets(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	cases := map[time.Duration]string{
		-time.Hour: "now", 0: "0m", 59 * time.Minute: "59m", time.Hour: "1h", 23 * time.Hour: "23h",
		24 * time.Hour: "1d", 29 * 24 * time.Hour: "29d", 30 * 24 * time.Hour: "1mo",
		364 * 24 * time.Hour: "12mo", 365 * 24 * time.Hour: "1y",
	}
	for d, want := range cases {
		if got := Age(now, now.Add(-d)); got != want {
			t.Errorf("Age(-%v)=%q want %q", d, got, want)
		}
	}
}

func TestRenderTable(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	r := "gitlab.com/o/r"
	lc := now.Add(-2 * 24 * time.Hour)
	var b bytes.Buffer
	RenderTable(&b, []Project{{Org: "o", Name: "r", Remote: &r, LastCommit: &lc}, {Org: "personal", Name: "local-only"}}, now)
	out := b.String()
	if !strings.Contains(out, "gitlab") || !strings.Contains(out, "2d") || !strings.Contains(out, "local") || !strings.Contains(out, "no commits") {
		t.Fatalf("%s", out)
	}
}

func TestAmbiguousErrorLists(t *testing.T) {
	err := &AmbiguousError{Term: "na", Hits: fake("a/app", "b/nabu")}
	if !strings.Contains(err.Error(), "na matches 2 projects") || !strings.Contains(err.Error(), "a/app") || !strings.Contains(err.Error(), "b/nabu") {
		t.Fatalf("%q", err.Error())
	}
}

func TestFacts(t *testing.T) {
	ws := t.TempDir()
	r := filepath.Join(ws, "a", "r")
	mkrepo(t, r)
	f, err := ReadFacts(r)
	if err != nil || f.Branch != "main" || f.HasUpstream || f.LastSubject != "init" || f.LastCommit == nil {
		t.Fatalf("%+v %v", f, err)
	}
	exec.Command("git", "-C", r, "checkout", "-q", "-b", "feat/x").Run()
	f, _ = ReadFacts(r)
	if f.Branch != "feat/x" || f.HasUpstream {
		t.Fatalf("%+v", f)
	}
	e := filepath.Join(ws, "empty")
	os.MkdirAll(e, 0o755)
	exec.Command("git", "-C", e, "init", "-q", "-b", "main").Run()
	f, err = ReadFacts(e)
	if err != nil || f.LastCommit != nil || f.Branch != "main" {
		t.Fatalf("empty repo: %+v %v", f, err)
	}
	if _, err := ReadFacts(ws); err == nil {
		t.Fatal("not a repo must error")
	}
}

// An SSH host alias from ~/.ssh/config reads as the real host.
func TestRemoteResolvesSSHHostAlias(t *testing.T) {
	testutil.FakeBin(t, "ssh", `[ "$1 $2" = "-G gh-work" ] && printf 'user git\nhostname github.com\nport 22\n' || printf 'hostname %s\n' "$2"`)
	if got := normalizeRemote("git@gh-work:acme/api.git"); got != "github.com/acme/api" {
		t.Fatalf("%q", got)
	}
	if got := normalizeRemote("git@github.com:acme/api.git"); got != "github.com/acme/api" {
		t.Fatalf("%q", got)
	}
}

// A git repo at $HOME (dotfiles) must not swallow every folder under it.
func TestForPathIgnoresARepoAtHome(t *testing.T) {
	home := t.TempDir()
	testutil.Repo(t, home, true, "")
	t.Setenv("HOME", home)
	sub := filepath.Join(home, "notes")
	os.MkdirAll(sub, 0o755)
	if root, ok := ForPath(sub); ok {
		t.Fatalf("resolved to %q", root)
	}
	inner := filepath.Join(home, "code", "api")
	testutil.Repo(t, inner, true, "")
	if root, ok := ForPath(inner); !ok || filepath.Base(root) != "api" {
		t.Fatalf("a real project under home still resolves: %q %v", root, ok)
	}
}

func TestRenderTableHasHeaderAndNeverBlankHost(t *testing.T) {
	odd := "/srv/mirrors/api"
	var b bytes.Buffer
	RenderTable(&b, []Project{{Org: "acme", Name: "api", Remote: &odd}}, time.Now())
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "org") || !strings.Contains(lines[0], "host") || !strings.Contains(lines[1], "other") {
		t.Fatalf("%q", b.String())
	}
}

func TestAgo(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	if Ago(now, now.Add(time.Minute)) != "just now" || Ago(now, now.Add(-3*time.Hour)) != "3h ago" {
		t.Fatal(Ago(now, now.Add(time.Minute)), Ago(now, now.Add(-3*time.Hour)))
	}
}
