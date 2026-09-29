package board

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/signal"
	"github.com/bilal-/sous/internal/thread"
)

func TestRender(t *testing.T) {
	now := time.Date(2026, 9, 27, 9, 2, 0, 0, time.UTC)
	errmsg := "gh not installed"
	d := &Data{
		Projects: []project.Project{{Path: "/ws/a/billing"}, {Path: "/ws/b/chime"}, {Path: "/ws/c/app-next"}, {Path: "/ws/d/quiet"}},
		Threads: []thread.View{
			{Thread: thread.Thread{ID: 1, Project: "/ws/a/billing", Kind: thread.Me, Text: "need final copy for the pricing page", Since: now.Add(-6 * 24 * time.Hour)}},
			{Thread: thread.Thread{ID: 2, Project: "/ws/c/app-next", Kind: thread.Them, Text: "waiting on Sam", Since: now.Add(-2 * 24 * time.Hour)}},
		},
		Signals: []signal.Observed{
			{Tagged: signal.Tagged{Signal: signal.Signal{ID: "s:aaaaaaaaaaaa", Project: "/ws/b/chime", Kind: signal.Unfinished, Text: "6 commits unpushed · main"}}, FirstSeen: now.Add(-6 * 24 * time.Hour)},
			{Tagged: signal.Tagged{Signal: signal.Signal{ID: "s:bbbbbbbbbbbb", Project: "/ws/b/chime", Kind: signal.Unfinished, Text: "hidden"}}, Snoozed: true},
			{Tagged: signal.Tagged{Signal: signal.Signal{ID: "s:cccccccccccc", Project: "/ws/c/app-next", Kind: signal.Me, Text: "review requested · PR #14 json api"}}, FirstSeen: now.Add(-2 * 24 * time.Hour), Stale: true},
		},
		Plugins:     []signal.PluginStatus{{Name: "git", Status: "ok"}, {Name: "github", Status: "failed", Error: &errmsg}},
		Checked:     4,
		Unavailable: 1,
		RenderedAt:  now,
	}
	var b bytes.Buffer
	Render(&b, d)
	out := b.String()
	for _, want := range []string{
		"sous · ? on you (github failed, 1 root unavailable, stale rows) · 2 found · 1 on others · 1 unfinished",
		"  on you", "  on others", "  unfinished",
		"need final copy for the pricing page", "6d",
		"review requested · PR #14 json api", "2d (stale)",
		"waiting on Sam",
		"6 commits unpushed · main",
		"  1  ", "s:aaaaaaaaaaaa",
		"4 checked · 1 unavailable · github: failed (gh not installed) · as of ",
		"sous snooze <id> to hide a row",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "hidden") || strings.Contains(out, "quiet") {
		t.Errorf("snoozed signal or quiet project leaked:\n%s", out)
	}

	empty := &Data{Checked: 3, RenderedAt: now, Plugins: []signal.PluginStatus{{Name: "git", Status: "ok"}}}
	b.Reset()
	Render(&b, empty)
	if !strings.Contains(b.String(), "sous · 0 on you · nothing waiting") || !strings.Contains(b.String(), "3 checked") {
		t.Errorf("empty state:\n%s", b.String())
	}
}

// Review 2 I5/I4: the headline must not claim zero when an obligation source
// failed or nothing was checked; (stale) must survive truncation.
func TestRenderHonestHeadlineAndStale(t *testing.T) {
	now := time.Date(2026, 9, 27, 9, 2, 0, 0, time.UTC)
	e := "gh not installed"
	d := &Data{Checked: 4, RenderedAt: now, Plugins: []signal.PluginStatus{{Name: "git", Status: "ok"}, {Name: "github", Status: "failed", Error: &e}}}
	var b bytes.Buffer
	Render(&b, d)
	if !strings.Contains(b.String(), "sous · ? on you") || !strings.Contains(b.String(), "github failed") {
		t.Errorf("headline must not say 0 when github failed:\n%s", b.String())
	}
	b.Reset()
	Render(&b, &Data{Checked: 0, Unavailable: 1, RenderedAt: now})
	if !strings.Contains(b.String(), "nothing checked") {
		t.Errorf("0 checked must not read as nothing waiting:\n%s", b.String())
	}
	long := strings.Repeat("review requested · PR #14 a very long dependabot title ", 3)
	d = &Data{Checked: 1, RenderedAt: now, Plugins: []signal.PluginStatus{{Name: "github", Status: "failed", Error: &e}},
		Signals: []signal.Observed{
			{Tagged: signal.Tagged{Signal: signal.Signal{ID: "s:1", Project: "/p", Kind: signal.Me, Text: long}}, FirstSeen: now.Add(-2 * 24 * time.Hour), Stale: true},
			{Tagged: signal.Tagged{Signal: signal.Signal{ID: "s:2", Project: "/p", Kind: signal.Unfinished, Text: "3 stashes"}}, FirstSeen: now.Add(-1 * 24 * time.Hour), Stale: true},
		}}
	b.Reset()
	Render(&b, d)
	if strings.Count(b.String(), "(stale)") != 2 {
		t.Errorf("both stale rows must be marked:\n%s", b.String())
	}
}

func TestEllipsizeIsRuneSafe(t *testing.T) {
	for _, c := range []struct {
		in   string
		n    int
		want string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"eleven chars", 10, "eleven ch…"},
		{"ééééé", 3, "éé…"},
	} {
		if got := Ellipsize(c.in, c.n); got != c.want {
			t.Errorf("Ellipsize(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
	msg := "gh: réseau indisponible — la connexion a échoué après trois tentatives"
	if got := PluginFailures(&Data{Plugins: []signal.PluginStatus{{Name: "github", Status: "failed", Error: &msg}}}); !utf8.ValidString(got) {
		t.Fatalf("plugin failure line split a rune: %q", got)
	}
}

// Review F7: missing data never looks like zero. Any reason in Why — not
// only a failed plugin — puts a "?" in the headline.
func TestHeadlineCarriesEveryWhy(t *testing.T) {
	now := time.Date(2026, 9, 27, 9, 2, 0, 0, time.UTC)
	for name, d := range map[string]*Data{
		"root unavailable": {Checked: 3, Unavailable: 1, RenderedAt: now},
		"stale rows":       {Checked: 3, RenderedAt: now, Signals: []signal.Observed{{Signal: signal.Signal{ID: "s:1", Project: "/code/acme/chime", Kind: signal.Me, Text: "review requested"}, FirstSeen: now, Stale: true}}},
	} {
		var b bytes.Buffer
		Render(&b, d)
		head := strings.SplitN(b.String(), "\n", 2)[0]
		if !strings.Contains(head, "?") || !strings.Contains(head, name) {
			t.Errorf("%s: headline %q", name, head)
		}
	}
}

// Review F11: --json carries data, not layout. Age is text for the eye;
// since and stale are what a program reads.
func TestRowJSONHasNoDisplayText(t *testing.T) {
	b, _ := json.Marshal(Row{ID: "1", Age: "6d (stale)", Stale: true})
	if strings.Contains(string(b), "6d") || !strings.Contains(string(b), `"stale":true`) {
		t.Fatalf("%s", b)
	}
}

func TestEllipsizeTinyBudget(t *testing.T) {
	if Ellipsize("abc", 0) != "" || Ellipsize("abc", 1) != "…" {
		t.Fatal(Ellipsize("abc", 1))
	}
}

// "as of 09:02" is ambiguous on a board that is days old: show the date.
func TestAsOfShowsTheDateWhenNotToday(t *testing.T) {
	old := time.Now().Add(-72 * time.Hour)
	var b bytes.Buffer
	Render(&b, &Data{Checked: 1, RenderedAt: old})
	if !strings.Contains(b.String(), "as of "+old.Local().Format("Mon 2 Jan 15:04")) {
		t.Fatalf("%s", b.String())
	}
}

// A source that was never set up is not a gap. One that found things
// before and now is not set up is: its old rows are stale, and it is named.
func TestNotSetUpIsOnlyAGapWhenItHadData(t *testing.T) {
	now := time.Now()
	off := []signal.PluginStatus{{Name: "github", Status: "off"}}
	var b bytes.Buffer
	Render(&b, &Data{Checked: 2, RenderedAt: now, Plugins: off})
	if head := strings.SplitN(b.String(), "\n", 2)[0]; head != "sous · 0 on you · nothing waiting" || strings.Contains(b.String(), "github") {
		t.Fatalf("never set up must be quiet:\n%s", b.String())
	}
	stale := []signal.Observed{{Tagged: signal.Tagged{Signal: signal.Signal{ID: "s:1", Project: "/code/acme/api", Kind: signal.Me, Text: "review requested"}, Plugin: "github"}, FirstSeen: now, Stale: true}}
	b.Reset()
	Render(&b, &Data{Checked: 2, RenderedAt: now, Plugins: off, Signals: stale})
	if head := strings.SplitN(b.String(), "\n", 2)[0]; !strings.Contains(head, "? on you (github not set up, stale rows)") {
		t.Fatalf("%s", head)
	}
}
