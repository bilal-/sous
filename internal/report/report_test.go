package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/board"
	"github.com/bilal-/sous/internal/session"
	"github.com/bilal-/sous/internal/signal"
	"github.com/bilal-/sous/internal/thread"

	"github.com/bilal-/sous/internal/testutil"
)

func init() { time.Local = time.UTC } // "since …" renders in local time; pin it

func reportFixture() (*board.Data, []thread.View, map[string]session.Session, time.Time, time.Time) {
	now := time.Date(2026, 9, 28, 17, 0, 0, 0, time.UTC)
	since := now.Add(-3 * 24 * time.Hour)
	closedYou := now.Add(-24 * time.Hour)
	closedUp := now.Add(-2 * time.Hour)
	longAgo := now.Add(-30 * 24 * time.Hour)
	ref := "github:oss/app-next#14"
	d := &board.Data{
		Checked: 26, RenderedAt: now,
		Threads: []thread.View{
			{Thread: thread.Thread{ID: 1, Project: "/ws/studio/work", Kind: thread.Me, Text: "need pricing copy", Since: now.Add(-24 * time.Hour)}},
			{Thread: thread.Thread{ID: 2, Project: "/ws/studio/work", Kind: thread.Me, Text: "old and already known", Since: longAgo}},
			{Thread: thread.Thread{ID: 3, Project: "/ws/acme/chime", Kind: thread.Idea, Text: "notification context", Since: now.Add(-5 * time.Hour)}},
		},
		Signals: []signal.Observed{
			{Tagged: signal.Tagged{Signal: signal.Signal{ID: "s:pr", Project: "/ws/oss/app-next", Kind: signal.Me, Text: "review requested · PR #14", Ref: &ref}}, FirstSeen: now.Add(-2 * 24 * time.Hour)},
			{Tagged: signal.Tagged{Signal: signal.Signal{ID: "s:mr", Project: "/ws/oss/app-next", Kind: signal.Them, Text: "awaiting review · MR !1"}}, FirstSeen: now.Add(-10 * time.Hour)},
			{Tagged: signal.Tagged{Signal: signal.Signal{ID: "s:dirty", Project: "/ws/x/y", Kind: signal.Unfinished, Text: "3 files uncommitted"}}, FirstSeen: now.Add(-time.Hour)},
		},
		Plugins: []signal.PluginStatus{{Name: "git", Status: "ok"}, {Name: "gitlab", Status: "failed"}},
	}
	closed := []thread.View{
		{Thread: thread.Thread{ID: 7, Project: "/ws/acme/chime", Kind: thread.Me, Text: "pairing flow", Closed: &closedYou}},
		{Thread: thread.Thread{ID: 8, Project: "/ws/studio/work", Kind: thread.Me, Text: "filed and ticked", Closed: &closedUp, ClosedBy: "upstream"}},
		{Thread: thread.Thread{ID: 9, Project: "/ws/studio/work", Kind: thread.Me, Text: "closed before the window", Closed: &longAgo}},
	}
	msg := "widget layout done"
	sessions := map[string]session.Session{
		"/ws/oss/ios-app": {Agent: "claude", Ended: now.Add(-3 * time.Hour), LastMessage: &msg},
		"/ws/old/thing":   {Agent: "codex", Ended: longAgo},
	}
	return d, closed, sessions, since, now
}

func TestBuildReport(t *testing.T) {
	d, closed, sessions, since, now := reportFixture()
	r := Build(d, closed, sessions, since, now)
	if len(r.NewMe) != 2 || len(r.NewThem) != 1 || len(r.NewIdeas) != 1 {
		t.Fatalf("new: me=%d them=%d ideas=%d", len(r.NewMe), len(r.NewThem), len(r.NewIdeas))
	}
	for _, row := range r.NewMe {
		if row.Text == "old and already known" {
			t.Fatal("rows older than the window are not new")
		}
	}
	if len(r.Closed) != 2 || r.Closed[0].ClosedBy != "upstream" {
		t.Fatalf("closed in window, newest first: %+v", r.Closed)
	}
	if len(r.Worked) != 1 || r.Worked[0].Name != "ios-app" || r.Worked[0].Note != "widget layout done" {
		t.Fatalf("worked: %+v", r.Worked)
	}
	if len(r.Attention) != 1 || !strings.Contains(r.Attention[0], "gitlab failed") {
		t.Fatalf("attention: %v", r.Attention)
	}
	if r.OnYouNow != 3 || r.OnOthersNow != 1 || r.UnfinishedNow != 1 {
		t.Fatalf("totals now: %+v", r)
	}
}

func TestRenderReportTextAndHTML(t *testing.T) {
	d, closed, sessions, since, now := reportFixture()
	r := Build(d, closed, sessions, since, now)
	var b bytes.Buffer
	Render(&b, r)
	out := b.String()
	testutil.Contains(t, out, "since Fri 25 Sep 17:00", "2 new on you", "2 closed", "needs attention", "gitlab failed",
		"new on you", "need pricing copy", "review requested · PR #14", "closed", "✓ 8", "filed and ticked", "(upstream)", "worked", "ios-app")
	b.Reset()
	if err := RenderHTML(&b, r); err != nil {
		t.Fatal(err)
	}
	h := b.String()
	testutil.Contains(t, h, "<!doctype html>", "prefers-color-scheme: dark", "need pricing copy", "https://github.com/oss/app-next/issues/14", "sous done 1", "Material Design 3")
	for _, bad := range []string{"<script", "http://", "fonts.googleapis", "<link"} {
		if strings.Contains(h, bad) {
			t.Errorf("html must be self-contained with no scripts or external loads: found %q", bad)
		}
	}
	if !strings.Contains(h, "&lt;") && strings.Contains(h, "<b>injected") {
		t.Error("text must be escaped")
	}
}

func TestReportHTMLEscapesText(t *testing.T) {
	now := time.Now()
	d := &board.Data{RenderedAt: now, Checked: 1, Threads: []thread.View{{Thread: thread.Thread{ID: 1, Project: "/p", Kind: thread.Me, Text: "<script>alert(1)</script>", Since: now}}}}
	var b bytes.Buffer
	RenderHTML(&b, Build(d, nil, nil, now.Add(-time.Hour), now))
	if strings.Contains(b.String(), "<script>alert") {
		t.Fatal("note text must be HTML-escaped")
	}
}

func TestIdeasAreGroupedPerProject(t *testing.T) {
	now := time.Now()
	d := &board.Data{RenderedAt: now, Checked: 1}
	for i := 1; i <= 5; i++ {
		d.Threads = append(d.Threads, thread.View{Thread: thread.Thread{ID: i, Project: "/ws/m/sous", Kind: thread.Idea, Text: "idea", Since: now}})
	}
	d.Threads = append(d.Threads, thread.View{Thread: thread.Thread{ID: 9, Project: "/ws/m/chime", Kind: thread.Idea, Text: "one", Since: now}})
	r := Build(d, nil, nil, now.Add(-time.Hour), now)
	g := r.IdeaGroups()
	if len(g) != 2 || g[0].Name != "sous" || len(g[0].Rows) != 5 {
		t.Fatalf("%+v", g)
	}
	var b bytes.Buffer
	Render(&b, r)
	if strings.Count(b.String(), "idea") > 3 || !strings.Contains(b.String(), "sous                    5\n") {
		t.Fatalf("ideas counted, not listed:\n%s", b.String())
	}
	b.Reset()
	RenderHTML(&b, r)
	if !strings.Contains(b.String(), "<details") {
		t.Fatal("ideas collapse on the page")
	}
}
