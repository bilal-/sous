package board

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/session"
	"github.com/bilal-/sous/internal/signal"
	"github.com/bilal-/sous/internal/thread"
)

var update = flag.Bool("update", false, "rewrite golden files")

func init() { time.Local = time.UTC } // "as of HH:MM" renders in local time; pin it

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	p := filepath.Join("testdata", name+".golden")
	if *update {
		os.MkdirAll("testdata", 0o755)
		os.WriteFile(p, got, 0o644)
		return
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("missing golden %s (run: go test ./internal/board -update)", p)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("output changed; if intended run with -update.\n--- want\n%s\n--- got\n%s", want, got)
	}
}

// Layout is a product decision: any change to column widths, section order,
// headline wording or footer must be deliberate.
func TestGoldenBoard(t *testing.T) {
	now := time.Date(2026, 9, 27, 9, 2, 0, 0, time.UTC)
	e := "gh: not logged in (run gh auth login)"
	ref := "github:oss/app-next#14"
	d := &Data{
		Projects: []project.Project{{Path: "/ws/studio/billing"}, {Path: "/ws/acme/chime"}, {Path: "/ws/oss/app-next"}},
		Threads: []thread.View{
			{Thread: thread.Thread{ID: 1, Project: "/ws/studio/billing", Kind: thread.Me, Text: "need final copy for the pricing page", Since: now.Add(-6 * 24 * time.Hour)}},
			{Thread: thread.Thread{ID: 2, Project: "/ws/oss/app-next", Kind: thread.Them, Text: "PR #14 awaiting Sam", Since: now.Add(-2 * 24 * time.Hour)}},
		},
		Signals: []signal.Observed{
			{Tagged: signal.Tagged{Signal: signal.Signal{ID: "s:cccccccccccc", Project: "/ws/oss/app-next", Kind: signal.Me, Text: "review requested · PR #14 json api for browse and search endpoints", Ref: &ref}}, FirstSeen: now.Add(-2 * 24 * time.Hour), Stale: true},
			{Tagged: signal.Tagged{Signal: signal.Signal{ID: "s:aaaaaaaaaaaa", Project: "/ws/acme/chime", Kind: signal.Unfinished, Text: "6 commits unpushed · main"}}, FirstSeen: now.Add(-6 * 24 * time.Hour)},
			{Tagged: signal.Tagged{Signal: signal.Signal{ID: "s:bbbbbbbbbbbb", Project: "/ws/studio/billing", Kind: signal.Unfinished, Text: "5 stashes"}}, FirstSeen: now.Add(-90 * 24 * time.Hour)},
		},
		Plugins: []signal.PluginStatus{{Name: "git", Status: "ok"}, {Name: "github", Status: "failed", Error: &e}},
		Checked: 3, RenderedAt: now,
	}
	var b bytes.Buffer
	Render(&b, d)
	golden(t, "board", b.Bytes())

	b.Reset()
	Render(&b, &Data{Checked: 26, RenderedAt: now, Plugins: []signal.PluginStatus{{Name: "git", Status: "ok"}, {Name: "github", Status: "ok"}}})
	golden(t, "board_empty", b.Bytes())
}

func TestGoldenHere(t *testing.T) {
	now := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	msg := "tests pass, next is the widget layout for the home screen"
	ref := "github:oss/ios-app#3"
	mdRef := "md:FOLLOWUPS.md:3:a1f3b2c4"
	goneRef := "md:FOLLOWUPS.md:4:deadbeef"
	closedAt := now.Add(-2 * 24 * time.Hour)
	lc := now.Add(-6 * 7 * 24 * time.Hour)
	d := &HereData{
		Project: "/ws/oss/ios-app", Name: "ios-app", RenderedAt: now,
		Facts:   project.Facts{Branch: "feature/home-widget", HasUpstream: false, LastCommit: &lc, LastSubject: "Wire the home widget to the data service"},
		Session: &session.Session{Agent: "claude", Ended: now.Add(-24 * time.Hour), LastMessage: &msg},
		Signals: []signal.Observed{
			{Tagged: signal.Tagged{Signal: signal.Signal{ID: "s:eeeeeeeeeeee", Project: "/ws/oss/ios-app", Kind: signal.Unfinished, Text: "2 files uncommitted · feature/home-widget"}}, FirstSeen: now.Add(-1 * time.Hour)},
			{Tagged: signal.Tagged{Signal: signal.Signal{ID: "s:dddddddddddd", Project: "/ws/oss/ios-app", Kind: signal.Me, Text: "review requested · PR #3 widget", Ref: &ref}}, FirstSeen: now.Add(-4 * 24 * time.Hour)},
		},
		Plugins: []signal.PluginStatus{{Name: "git", Status: "ok"}, {Name: "github", Status: "ok"}},
		Threads: []thread.View{
			{Thread: thread.Thread{ID: 3, Kind: thread.Me, Text: "ask Sam about App Store cert", Ref: &mdRef, Since: now.Add(-1 * 24 * time.Hour)}, Upstream: "open"},
			{Thread: thread.Thread{ID: 4, Kind: thread.Me, Text: "marker was deleted", Ref: &goneRef, Since: now.Add(-2 * 24 * time.Hour)}, Upstream: "unknown"},
			{Thread: thread.Thread{ID: 1, Kind: thread.Idea, Text: "widget: home screen layout", Since: now.Add(-6 * 7 * 24 * time.Hour)}, Snoozed: true},
			{Thread: thread.Thread{ID: 2, Kind: thread.Idea, Text: "consider offline image cache", Since: now.Add(-9 * 7 * 24 * time.Hour)}},
		},
		RecentlyClosed: []thread.View{{Thread: thread.Thread{ID: 9, Kind: thread.Me, Text: "old one", Closed: &closedAt}}},
	}
	var b bytes.Buffer
	RenderHere(&b, d, now, false)
	golden(t, "here", b.Bytes())
}
