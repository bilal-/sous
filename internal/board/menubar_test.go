package board

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/signal"
	"github.com/bilal-/sous/internal/thread"
)

func TestRenderMenubar(t *testing.T) {
	now := time.Date(2026, 9, 27, 9, 2, 0, 0, time.UTC)
	d := &Data{
		Threads: []thread.View{{Thread: thread.Thread{ID: 1, Project: "/ws/studio/work", Kind: thread.Me, Text: "--chase | designer", Since: now.Add(-6 * 24 * time.Hour)}}},
		Signals: []signal.Observed{
			{Tagged: signal.Tagged{Signal: signal.Signal{ID: "s:1", Project: "/ws/oss/app-next", Kind: signal.Me, Text: "review requested · PR #14"}}, FirstSeen: now.Add(-2 * 24 * time.Hour)},
			{Tagged: signal.Tagged{Signal: signal.Signal{ID: "s:2", Project: "/ws/oss/app-next", Kind: signal.Me, Text: "hidden"}}, Snoozed: true},
			{Tagged: signal.Tagged{Signal: signal.Signal{ID: "s:3", Project: "/ws/acme/chime", Kind: signal.Unfinished, Text: "6 commits unpushed"}}, FirstSeen: now.Add(-6 * 24 * time.Hour)},
		},
		Plugins: []signal.PluginStatus{{Name: "git", Status: "ok"}, {Name: "github", Status: "ok"}},
		Checked: 3, RenderedAt: now,
	}
	var b bytes.Buffer
	RenderMenubar(&b, d, "/usr/local/bin/sous", "12m", false)
	out := b.String()
	if !strings.HasPrefix(out, "⚑ 2\n---\n") {
		t.Fatalf("title:\n%s", out)
	}
	for _, want := range []string{
		"chase / designer · studio/work · 6d | bash=/usr/local/bin/sous param1=go param2=studio/work terminal=true",
		"review requested · PR #14 · oss/app-next · 2d | bash=/usr/local/bin/sous param1=go param2=oss/app-next terminal=true",
		"\nunfinished\n-- 6 commits unpushed · acme/chime · 6d",
		"3 checked · as of Sun 27 Sep 09:02 · cached 12m ago",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "hidden") || strings.Contains(out, "Refresh") || strings.Contains(out, "\n--chase") {
		t.Errorf("snoozed row, refresh action, or unsanitized dash leaked:\n%s", out)
	}
	e := "gh not installed"
	d.Plugins[1] = signal.PluginStatus{Name: "github", Status: "failed", Error: &e}
	d.Unavailable = 1
	d.Signals[0].Stale = true
	b.Reset()
	RenderMenubar(&b, d, "/s", "9h", true)
	out = b.String()
	if !strings.HasPrefix(out, "⚑ 2?\n---\n") || !strings.Contains(out, "github failed · 1 root unavailable · stale rows · cache 9h old | color=orange") || !strings.Contains(out, "2d (stale)") {
		t.Fatalf("uncertain:\n%s", out)
	}
	b.Reset()
	RenderMenubar(&b, &Data{Checked: 0, Unavailable: 2, RenderedAt: now}, "/s", "0m", false)
	if !strings.HasPrefix(b.String(), "⚑ 0?\n") || !strings.Contains(b.String(), "nothing checked") {
		t.Fatalf("nothing checked must not read as a clean zero:\n%s", b.String())
	}
	b.Reset()
	RenderMenubar(&b, &Data{Checked: 5, RenderedAt: now, Plugins: []signal.PluginStatus{{Name: "git", Status: "ok"}}}, "/s", "0m", false)
	if !strings.HasPrefix(b.String(), "⚑ 0\n") {
		t.Fatalf("clean zero:\n%s", b.String())
	}
}
