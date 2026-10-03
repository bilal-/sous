package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/board"

	"github.com/bilal-/sous/internal/testutil"
)

func TestBoardCLI(t *testing.T) {
	f := fixture(t)
	w := f.mkrepo("studio/billing", true)
	os.WriteFile(filepath.Join(w, "x"), nil, 0o644)
	f.mkrepo("acme/chime", true)
	ra := f.mkrepo("oss/android-app", true)
	q := f.mkrepo("oss/quiet", true)
	f.runIn(w, "note", "-k", "me", "need final copy for pricing")
	f.runIn(ra, "note", "-k", "them", "waiting on Play Console review")
	f.runIn(w, "note", "an idea")
	f.brokenGH(ra)

	out, _, code := f.run()
	if code != 0 {
		t.Fatalf("board: %d %q", code, out)
	}
	testutil.Contains(t, out, "? on you (github failed) · 1 found", "1 on others", "1 unfinished", "need final copy for pricing", "waiting on Play Console", "1 files uncommitted · main", "4 checked", "github: failed", "sous snooze")
	if strings.Contains(out, "an idea") || strings.Contains(out, "quiet") {
		t.Errorf("idea or quiet project leaked:\n%s", out)
	}
	out2, _, _ := f.runIn(q) // bare sous inside a repo is STILL the board
	if strings.SplitN(out2, "\n", 2)[0] != strings.SplitN(out, "\n", 2)[0] {
		t.Fatalf("bare sous must ignore cwd:\n%s", out2)
	}
	out, _, _ = f.run(filepath.Join(f.WS, "oss"))
	if strings.Contains(out, "work") || !strings.Contains(out, "2 checked") || !strings.Contains(out, "Play Console") {
		t.Fatalf("scoped board:\n%s", out)
	}
	j, _, _ := f.run("--json")
	testutil.Contains(t, j, `"configured": true`, `"on_you"`, `"on_others"`, `"unfinished"`, `"ideas"`, `"snoozed"`, `"attention"`, `"projects"`, `"plugins"`, `"checked"`, `"unavailable"`, `"as_of"`)
	if b, err := os.ReadFile(filepath.Join(f.SousHome, "cache.json")); err != nil || !strings.Contains(string(b), `"board"`) {
		t.Fatal("cache not written")
	}
	f.writeConfig("roots = [\"" + f.WS + "\", \"" + filepath.Join(f.WS, "gone") + "\"]\n")
	out, _, _ = f.run()
	if !strings.Contains(out, "1 unavailable") {
		t.Fatalf("missing root must show as unavailable:\n%s", out)
	}
}

func TestCachedAndRefresh(t *testing.T) {
	f := fixture(t)
	r := f.mkrepo("a/r", true)
	f.runIn(r, "note", "-k", "me", "thing")

	out, _, code := f.run("--cached")
	if code != 3 || !strings.Contains(out, "building") {
		t.Fatalf("no cache: %d %q", code, out)
	}
	// --cached spawned a bootstrap refresh; let it finish before the stale
	// scenario below spawns another, or two detached refreshes race.
	waitFor(t, func() bool { _, err := os.Stat(filepath.Join(f.SousHome, "cache.json")); return err == nil })
	if out, _, code := f.run("--refresh"); code != 0 || out != "" {
		t.Fatalf("refresh must be silent: %d %q", code, out)
	}
	out, _, _ = f.run("--cached")
	if !strings.Contains(out, "1 on you") || !strings.Contains(out, "(cached · just now)") {
		t.Fatalf("cached: %q", out)
	}
	// Stale cache: prints old board now, refreshes in background.
	f.run("snooze", "1")
	p := filepath.Join(f.SousHome, "cache.json")
	var c board.CacheDoc
	b, _ := os.ReadFile(p)
	json.Unmarshal(b, &c)
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c.RenderedAt = &old
	b, _ = json.Marshal(c)
	os.WriteFile(p, b, 0o644)
	out, _, _ = f.run("--cached")
	if !strings.Contains(out, "1 on you") {
		t.Fatalf("stale must print old board immediately: %q", out)
	}
	waitFor(t, func() bool { b, _ = os.ReadFile(p); return strings.Contains(string(b), "0 on you") })
}

// waitFor polls a condition for up to 10s; detached refreshes re-exec the
// test binary and can be slow under a full-suite run.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

func TestScopedBoardDoesNotOverwriteCache(t *testing.T) {
	f := fixture(t)
	f.mkrepo("a/one", true)
	f.mkrepo("b/two", true)
	f.run()
	f.run(filepath.Join(f.WS, "a"))
	out, _, _ := f.run("--cached")
	if !strings.Contains(out, "2 checked") {
		t.Fatalf("cache must still be the full board:\n%s", out)
	}
}

func TestCachedBootstrapsWhenEmpty(t *testing.T) {
	f := fixture(t)
	f.mkrepo("a/r", true)
	out, _, code := f.run("--cached")
	if code != 3 || !strings.Contains(out, "building") {
		t.Fatalf("no cache: code=%d out=%q", code, out)
	}
	p := filepath.Join(f.SousHome, "cache.json")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(p); err == nil && strings.Contains(string(b), "checked") {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("bootstrap refresh did not land")
}

func TestScopedBoardMatchesByRemoteAndRelativePath(t *testing.T) {
	f := fixture(t)
	old := f.mkrepo("a/old-name", true)
	f.git(old, "remote", "add", "origin", "git@github.com:o/r.git")
	f.runIn(old, "note", "-k", "me", "kept across rename")
	os.Rename(old, filepath.Join(f.WS, "a", "new-name"))
	out, _, _ := f.run(filepath.Join(f.WS, "a"))
	if !strings.Contains(out, "kept across rename") {
		t.Fatalf("scoped board must match by remote:\n%s", out)
	}
	out2, _, _ := f.runIn(f.WS, "a")
	if !strings.Contains(out2, "kept across rename") || !strings.Contains(out2, "1 checked") {
		t.Fatalf("relative folder must behave like absolute:\n%s", out2)
	}
}

func TestMenubarIsAPureReader(t *testing.T) {
	f := fixture(t)
	r := f.mkrepo("a/r", true)
	f.runIn(r, "note", "-k", "me", "thing")
	out, _, code := f.run("--menubar")
	if code != 0 || !strings.HasPrefix(out, "⚑ –\n---\n") || !strings.Contains(out, "no board yet") {
		t.Fatalf("no cache: %d %q", code, out)
	}
	time.Sleep(500 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(f.SousHome, "cache.json")); err == nil {
		t.Fatal("--menubar must not trigger a refresh")
	}
	f.brokenGH(r)
	f.run("--refresh")
	out, _, _ = f.run("--menubar")
	if !strings.HasPrefix(out, "⚑ 1?\n") || !strings.Contains(out, "thing · a/r · ") || !strings.Contains(out, "param2=a/r") || !strings.Contains(out, "github failed") {
		t.Fatalf("menubar from cache data (github fails in fixtures → ?):\n%s", out)
	}
	f.run("setup")
	if b, err := os.ReadFile(filepath.Join(f.SousHome, "sous.5m.sh")); err != nil || !strings.Contains(string(b), "--menubar") {
		t.Fatal("swiftbar plugin not written")
	}
}

func TestUnavailableRootWithFiledThreadDoesNotPanic(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("a/r", true)
	f.git(p, "remote", "add", "origin", "git@github.com:o/r.git")
	os.WriteFile(filepath.Join(p, "FOLLOWUPS.md"), []byte("# F\n"), 0o644)
	f.runIn(p, "note", "--file", "-k", "me", "filed")
	f.writeConfig("roots = [\"" + f.WS + "\", \"/Volumes/NotMounted/work\"]\n")
	for _, c := range [][]string{{}, {"here", p}, {"--refresh"}, {"file", "1"}} {
		if _, errs, code := f.run(c...); code != 0 || strings.Contains(errs, "panic") {
			t.Fatalf("%v: code=%d err=%q", c, code, errs)
		}
	}
}

// Obligations the board observed (review requests) show in `here` without
// `here` itself touching the network: it reads observed.json.
func TestHereShowsObservedObligationsWithoutScanning(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("a/r", true)
	f.git(p, "remote", "add", "origin", "git@github.com:o/r.git")
	calls := filepath.Join(f.Home, "gh-calls")
	f.bin("gh", `echo "$*" >> `+calls+`
case "$*" in
  "auth status") exit 0;;
  *--review-requested=@me*) printf '[{"repository":{"nameWithOwner":"o/r"},"number":5,"title":"look","updatedAt":"2026-09-25T10:00:00Z"}]';;
  *) printf '[]';;
esac`)
	f.run() // the board scans and observes
	os.Remove(calls)
	out, _, _ := f.run("here", p)
	if !strings.Contains(out, "on you: 1") || !strings.Contains(out, "review requested · PR #5 look") {
		t.Fatalf("%s", out)
	}
	if _, err := os.Stat(calls); err == nil {
		t.Fatal("here must not call gh")
	}
}

// The shell prints the board at most once per refresh window, and the
// window comes from config.toml alone.
func TestAmbientPrintsOncePerWindow(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/api", true)
	f.runIn(p, "note", "-k", "me", "thing")
	if out, _, code := f.run("--ambient"); code != 3 || !strings.Contains(out, "building") {
		t.Fatalf("no board yet: %d %q", code, out)
	}
	waitFor(t, func() bool { _, err := os.Stat(filepath.Join(f.SousHome, "cache.json")); return err == nil })
	f.run("--refresh")
	if out, _, code := f.run("--ambient"); code != 0 || !strings.Contains(out, "thing") {
		t.Fatalf("first: %d %q", code, out)
	}
	if out, _, code := f.run("--ambient"); code != 0 || out != "" {
		t.Fatalf("second, inside the window: %d %q", code, out)
	}
}

// The board asks trackers and runners at once: a slow one of each costs
// the time of one, not both.
func TestBoardAsksTrackersAndRunnersAtOnce(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/api", true)
	be := testutil.Script(t, t.TempDir(), "sous-backend-slow", `case "$1" in status) sleep 1; echo open;; *) exit 1;; esac`)
	ru := testutil.Script(t, t.TempDir(), "sous-runner-slow", `case "$1" in start) echo slow:1;; status) sleep 1; echo '{"v":0,"state":"running"}';; esac`)
	f.writeConfig("roots = [\"" + f.WS + "\"]\nplugins = [\"" + be + "\", \"" + ru + "\"]\n")
	f.runIn(p, "note", "-k", "me", "filed on the slow tracker")
	f.fileAs(1, "slow:1")
	if _, errs, code := f.run("go", "acme/api", "--run", "a slow task", "-a", "slow"); code != 0 {
		t.Fatal(errs)
	}
	start := time.Now()
	out, _, _ := f.run()
	if took := time.Since(start); took > 1800*time.Millisecond {
		t.Fatalf("one slow tracker and one slow runner took %v:\n%s", took, out)
	}
	testutil.Contains(t, out, "filed on the slow tracker", "running · a slow task")
}
