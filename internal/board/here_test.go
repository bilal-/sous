package board

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/session"
	"github.com/bilal-/sous/internal/signal"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/thread"

	"github.com/bilal-/sous/internal/testutil"
)

func TestRenderHere(t *testing.T) {
	now := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	msg := "tests pass, next is the widget layout for the home screen"
	lc := now.Add(-6 * 7 * 24 * time.Hour)
	d := &HereData{
		Project: "/ws/ios-app", Name: "ios-app", RenderedAt: now,
		Facts:   project.Facts{Branch: "feature/widget", LastCommit: &lc, LastSubject: "Wire the home widget"},
		Signals: []signal.Observed{{Tagged: signal.Tagged{Signal: signal.Signal{ID: "s:1", Project: "/ws/ios-app", Kind: signal.Unfinished, Text: "2 files uncommitted · feature/widget"}}, FirstSeen: now.Add(-1 * time.Hour)}},
		Session: &session.Session{Agent: "claude", Ended: now.Add(-24 * time.Hour), LastMessage: &msg},
		Threads: []thread.View{
			{Thread: thread.Thread{ID: 3, Kind: thread.Me, Text: "ask Sam about cert", Since: now.Add(-1 * 24 * time.Hour)}},
			{Thread: thread.Thread{ID: 1, Kind: thread.Idea, Text: "home screen layout", Since: now.Add(-6 * 7 * 24 * time.Hour)}, Snoozed: true},
			{Thread: thread.Thread{ID: 2, Kind: thread.Idea, Text: "offline cache", Since: now.Add(-9 * 7 * 24 * time.Hour)}},
		},
	}
	var b bytes.Buffer
	RenderHere(&b, d, now, false)
	out := b.String()
	testutil.Contains(t, out, "ios-app · feature/widget · last commit 1mo ago",
		`  "Wire the home widget"`, "2 files uncommitted",
		"last session · claude · 2026-09-26 · ended: \"tests pass",
		"on you: 1 · on others: 0 · ideas: 2", "  3  ask Sam about cert  1d", "home screen layout  1mo (snoozed)",
		"sous note", "sous done <n>")
	d.Session = nil
	b.Reset()
	RenderHere(&b, d, now, false)
	if !strings.Contains(b.String(), "last session · none recorded") {
		t.Error("no session line")
	}
	long := strings.Repeat("x", 500)
	d.Session = &session.Session{Agent: "claude", Ended: now, LastMessage: &long} // not "codex": it contains an x
	for i := 0; i < 7; i++ {
		d.Threads = append(d.Threads, thread.View{Thread: thread.Thread{ID: 10 + i, Kind: thread.Idea, Text: "idea", Since: now.Add(-1 * 24 * time.Hour)}})
	}
	b.Reset()
	RenderHere(&b, d, now, true)
	if strings.Count(b.String(), "x") != 299 || !strings.Contains(b.String(), "x…\"") || !strings.Contains(b.String(), "… and 4 more") {
		t.Errorf("brief caps:\n%s", b.String())
	}
}

// `here` runs only local plugins through the door and reads everything the
// board observed for this project from observed.json; another project's
// observations stay out.
func TestBuildHereUsesThePluginDoor(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	exec.Command("git", "-C", root, "init", "-q", "-b", "main").Run()
	dir := t.TempDir()
	plugin := filepath.Join(dir, "sous-signal-fake")
	script := "#!/bin/sh\nwhile read p; do printf '{\"v\":0,\"id\":\"s:%s\",\"project\":\"%s\",\"kind\":\"me\",\"text\":\"review requested · PR #14\",\"observed\":\"2026-09-27T00:00:00Z\",\"ref\":null}\\n' \"$(basename \"$p\")\" \"$p\"; done\n"
	os.WriteFile(plugin, []byte(script), 0o755)
	// Seed observed.json the way a board run would, via the fake plugin.
	col := signal.Collect(context.Background(), signal.Registry.Discover("", nil, []string{plugin}), []string{root, "/elsewhere"}, 5*time.Second)
	if len(col.Signals) != 2 {
		t.Fatalf("seed plugin: %+v", col)
	}
	signal.Observe(s, col, []string{root, "/elsewhere"}, now)
	in := Inputs{Store: s, Cfg: &config.Config{}, Timeout: 5 * time.Second, Now: now}
	d, err := BuildHere(context.Background(), in, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Signals) != 1 || d.Signals[0].Project != root || d.Facts.Branch != "main" {
		t.Fatalf("%+v", d)
	}
	var b bytes.Buffer
	RenderHere(&b, d, now, false)
	if !strings.Contains(b.String(), "on you: 1") || !strings.Contains(b.String(), "review requested · PR #14") {
		t.Fatalf("here must count and list plugin obligations:\n%s", b.String())
	}
}

func TestMigratorsRefuseUnknownVersions(t *testing.T) {
	if _, err := CacheFile.Migrate(0, nil); err == nil {
		t.Fatal("cache")
	}
}

// What an agent hears at session start stays short: five rows of each
// kind, then where the rest are; a long note is cut where it shows it was,
// with how to read it whole.
func TestBriefHereIsShortAndSaysWhatItCut(t *testing.T) {
	now := time.Now()
	var ths []thread.View
	for i := 1; i <= 8; i++ {
		ths = append(ths, thread.View{Thread: thread.Thread{ID: i, Project: "/code/acme/api", Text: fmt.Sprintf("task %d", i), Kind: thread.Me, Since: now}})
	}
	long := strings.Repeat("word ", 40)
	ths = append(ths, thread.View{Thread: thread.Thread{ID: 9, Project: "/code/acme/api", Text: long, Kind: thread.Them, Since: now}})
	d := &HereData{Project: "/code/acme/api", Name: "api", RenderedAt: now, Threads: ths}
	var b strings.Builder
	RenderHere(&b, d, now, true)
	out := b.String()
	testutil.Contains(t, out, "  5  task 5", "… and 3 more (sous api)", "…  0m  (them)", "cut short: sous show <n>")
	if strings.Contains(out, "task 6") || strings.Contains(out, long) {
		t.Fatalf("brief lists five of each and cuts long notes:\n%s", out)
	}
	b.Reset()
	RenderHere(&b, d, now, false)
	testutil.Contains(t, b.String(), "task 8")
}
