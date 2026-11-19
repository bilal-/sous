package board

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/memory"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/session"
	"github.com/bilal-/sous/internal/signal"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/thread"

	"github.com/bilal-/sous/internal/testutil"
	"github.com/bilal-/sous/internal/text"
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
	if !strings.Contains(b.String(), "x…\"") || !strings.Contains(b.String(), "… and 4 more") {
		t.Errorf("brief caps:\n%s", b.String())
	}
	for _, l := range strings.Split(b.String(), "\n") {
		if utf8.RuneCountInString(l) > text.LineRunes {
			t.Errorf("a brief line is at most %d runes: %q", text.LineRunes, l)
		}
	}
}

// `here` runs only local plugins through the door and reads everything the
// board observed for this project from observed.json; another project's
// observations stay out.
func TestBuildHereUsesThePluginDoor(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	root := testutil.Repo(t, t.TempDir(), false, "")
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
	if _, err := cacheFile.Migrate(0, nil); err == nil {
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
	testutil.Contains(t, out, "  5  task 5", "… and 3 more (sous acme/api)", "…  0m  (them)", "cut short: sous show <n>")
	if strings.Contains(out, "task 6") || strings.Contains(out, long) {
		t.Fatalf("brief lists five of each and cuts long notes:\n%s", out)
	}
	b.Reset()
	RenderHere(&b, d, now, false)
	testutil.Contains(t, b.String(), "task 8")
}

func TestNextStepsFallbackAndHereContext(t *testing.T) {
	strPtr := func(s string) *string { return &s }
	s := &store.Store{Home: t.TempDir()}
	ws := t.TempDir()
	if real, err := filepath.EvalSymlinks(ws); err == nil {
		ws = real
	}
	repoSess := testutil.Repo(t, filepath.Join(ws, "repo-sess"), false, "")
	repoHandoff := testutil.Repo(t, filepath.Join(ws, "repo-handoff"), false, "")
	repoStatus := testutil.Repo(t, filepath.Join(ws, "repo-status"), false, "")
	t0 := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)

	// repoSess has no STATUS.md, only two branch sessions in history.
	if err := session.Record(s, repoSess, session.Session{
		Agent: "codex", SessionID: "cx-1", Ended: t0,
		Branch: "feat/old", LastMessage: strPtr("built parser"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.Record(s, repoSess, session.Session{
		Agent: "claude", SessionID: "cl-1", Ended: t0.Add(2 * time.Hour),
		Branch: "feat/auth", Prompt: strPtr("fix token refresh"), Files: []string{"auth.go", "auth_test.go"},
	}); err != nil {
		t.Fatal(err)
	}

	// repoHandoff has an older session and a newer private handoff memory.
	if err := session.Record(s, repoHandoff, session.Session{
		Agent: "claude", SessionID: "cl-2", Ended: t0,
		Branch: "main", LastMessage: strPtr("older session summary"),
	}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Roots: []string{ws}}
	if _, _, err := memory.Add(s, memory.Input{
		Scope: memory.ForProject(cfg, project.Project{Path: repoHandoff}),
		Kind:  memory.Handoff,
		Text:  "wait for Sam to merge schema change",
	}, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	// repoStatus has STATUS.md written before a newer session.
	statusPath := filepath.Join(repoStatus, project.StatusFile)
	if err := os.WriteFile(statusPath, []byte("# Status\n\nWorking on billing rollout.\n\n## Next step\n- deploy canary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(statusPath, t0, t0); err != nil {
		t.Fatal(err)
	}
	if err := session.Record(s, repoStatus, session.Session{
		Agent: "gemini", SessionID: "gm-1", Ended: t0.Add(3 * time.Hour),
		Branch: "main", LastMessage: strPtr("ran load test"),
	}); err != nil {
		t.Fatal(err)
	}

	ps := []project.Project{{Path: repoSess}, {Path: repoHandoff}, {Path: repoStatus}}
	steps := nextSteps(ps, s, cfg, nil)
	if len(steps) != 3 {
		t.Fatalf("got %d next steps: %+v", len(steps), steps)
	}
	byProj := map[string]NextStep{}
	for _, st := range steps {
		byProj[st.Project] = st
	}
	if byProj[repoSess].Source != "session" || byProj[repoSess].Branch != "feat/auth" || byProj[repoSess].Step != "fix token refresh" {
		t.Fatalf("session fallback: %+v", byProj[repoSess])
	}
	if byProj[repoHandoff].Source != "handoff" || byProj[repoHandoff].Step != "wait for Sam to merge schema change" {
		t.Fatalf("handoff fallback: %+v", byProj[repoHandoff])
	}
	if byProj[repoStatus].Source != project.StatusFile || byProj[repoStatus].Step != "deploy canary" {
		t.Fatalf("STATUS.md must take preference: %+v", byProj[repoStatus])
	}

	var boardBuf bytes.Buffer
	renderNext(&boardBuf, steps, t0.Add(4*time.Hour))
	testutil.Contains(t, boardBuf.String(), "next up · from STATUS.md and recent work", "[session · feat/auth] fix token refresh", "[handoff] wait for Sam", "deploy canary")

	in := Inputs{Store: s, Cfg: cfg, Now: t0.Add(4 * time.Hour), Timeout: 2 * time.Second}
	hereSess, err := BuildHere(context.Background(), in, repoSess)
	if err != nil {
		t.Fatal(err)
	}
	var hb bytes.Buffer
	RenderHere(&hb, hereSess, t0.Add(4*time.Hour), false)
	testutil.Contains(t, hb.String(),
		`last session · claude · 2026-10-01 · feat/auth · working on: "fix token refresh"`,
		"files · auth.go, auth_test.go",
		`recent · codex · feat/old · 2026-10-01 · "built parser"`,
		"next step · fix token refresh (session, feat/auth, updated",
	)
	hb.Reset()
	RenderHere(&hb, hereSess, t0.Add(4*time.Hour), true)
	for _, l := range strings.Split(hb.String(), "\n") {
		if utf8.RuneCountInString(l) > text.LineRunes {
			t.Fatalf("brief line exceeds %d runes: %q", text.LineRunes, l)
		}
	}

	hereStatus, err := BuildHere(context.Background(), in, repoStatus)
	if err != nil {
		t.Fatal(err)
	}
	hb.Reset()
	RenderHere(&hb, hereStatus, t0.Add(4*time.Hour), false)
	testutil.Contains(t, hb.String(),
		"next step · deploy canary (STATUS.md, updated 4h ago)",
		"status · Working on billing rollout.",
		"note · session activity is newer than STATUS.md",
	)
}

func TestBuildHereRestoresBranchSessionsEvictedPastMaxHistory(t *testing.T) {
	strPtr := func(s string) *string { return &s }
	s := &store.Store{Home: t.TempDir()}
	ws := t.TempDir()
	if real, err := filepath.EvalSymlinks(ws); err == nil {
		ws = real
	}
	repo := testutil.Repo(t, filepath.Join(ws, "acme-api"), true, "")
	t0 := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)

	// Record sessions across five branches in repo, oldest on feat/billing.
	branches := []struct {
		branch, id, msg string
		offset          time.Duration
	}{
		{"feat/billing", "s-bill", "wired Sam's invoice webhook", 0},
		{"feat/search", "s-srch", "indexed prefix tokens", time.Minute},
		{"feat/export", "s-exp", "streamed CSV rows", 2 * time.Minute},
		{"feat/webhooks", "s-wh", "added HMAC verification", 3 * time.Minute},
		{"main", "s-main", "cut staging tag", 4 * time.Minute},
	}
	for _, b := range branches {
		if err := session.Record(s, repo, session.Session{
			Agent: "claude", SessionID: b.id, Branch: b.branch,
			LastMessage: strPtr(b.msg), Ended: t0.Add(b.offset),
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Evict every repo entry from the 200-entry global rolling history ring.
	for i := 0; i < session.MaxHistory+10; i++ {
		if err := session.Record(s, "/ws/acme/other", session.Session{
			Agent:       "codex",
			SessionID:   fmt.Sprintf("other-%d", i),
			Branch:      "main",
			LastMessage: strPtr("other project work"),
			Ended:       t0.Add(time.Hour + time.Duration(i)*time.Second),
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Sanity-check that global rolling history no longer holds any repo branch entries.
	globalHist, err := session.History(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range globalHist {
		if e.Project == repo {
			t.Fatalf("expected repo entries evicted from global history, found %+v", e)
		}
	}

	// Switch repo back to the oldest branch (feat/billing) and build here.
	testutil.Git(t, repo, "checkout", "-q", "-b", "feat/billing")
	in := Inputs{Store: s, Cfg: &config.Config{Roots: []string{ws}}, Now: t0.Add(5 * time.Hour), Timeout: 2 * time.Second}
	d, err := BuildHere(context.Background(), in, repo)
	if err != nil {
		t.Fatal(err)
	}
	if d.Session == nil || d.Session.SessionID != "s-main" {
		t.Fatalf("expected latest project session s-main, got %+v", d.Session)
	}
	if len(d.RecentSessions) != 3 {
		t.Fatalf("expected 3 recent branch sessions restored from d.Branches, got %+v", d.RecentSessions)
	}
	if d.RecentSessions[0].Branch != "feat/billing" || d.RecentSessions[0].SessionID != "s-bill" {
		t.Fatalf("expected checked-out branch feat/billing prioritized first in RecentSessions, got %+v", d.RecentSessions)
	}

	var hb bytes.Buffer
	RenderHere(&hb, d, t0.Add(5*time.Hour), false)
	testutil.Contains(t, hb.String(),
		`last session · claude · 2026-10-01 · main · ended: "cut staging tag"`,
		`recent · claude · feat/billing · 2026-10-01 · "wired Sam's invoice webhook"`,
		`recent · claude · feat/webhooks · 2026-10-01 · "added HMAC verification"`,
		`recent · claude · feat/export · 2026-10-01 · "streamed CSV rows"`,
	)

	// When d.Sessions has no entry for root, d.Branches[root][branch] restores d.Session directly.
	sBranchOnly := &store.Store{Home: t.TempDir()}
	branchJSON := fmt.Sprintf(`{"version":2,"sessions":{},"branches":{%q:{"feat/billing":{"agent":"claude","session_id":"s-bill","ended":"2026-10-01T10:00:00Z","last_message":"wired Sam's invoice webhook","branch":"feat/billing"}}},"history":[]}`, repo)
	if err := os.WriteFile(filepath.Join(sBranchOnly.Home, "sessions.json"), []byte(branchJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	dBranchOnly, err := BuildHere(context.Background(), Inputs{Store: sBranchOnly, Cfg: &config.Config{Roots: []string{ws}}, Now: t0.Add(5 * time.Hour), Timeout: 2 * time.Second}, repo)
	if err != nil {
		t.Fatal(err)
	}
	if dBranchOnly.Session == nil || dBranchOnly.Session.SessionID != "s-bill" {
		t.Fatalf("expected d.Session restored from d.Branches when sessions[root] is absent, got %+v", dBranchOnly.Session)
	}
}
