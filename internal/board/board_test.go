package board

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
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

// --json carries data, not layout: no ages, and a run's text is the note
// as written, with how the run is going beside it, never the board's
// "run needs you · …" line.
func TestItemHasNoDisplayText(t *testing.T) {
	now := time.Now()
	v := thread.View{Thread: thread.Thread{ID: 1, Project: "/code/acme/billing", Text: "fix the flaky test", Kind: thread.Them, Since: now.Add(-6 * 24 * time.Hour),
		Run: &thread.Run{Runner: "claude", State: "needs_you", Text: "which fixture?"}}}
	b, _ := json.Marshal(ThreadRow(v, now).Item())
	got := string(b)
	for _, want := range []string{`"text":"fix the flaky test"`, `"kind":"me"`, `"run":{"runner":"claude","state":"needs_you","text":"which fixture?"`, `"name":"billing"`} {
		if !strings.Contains(got, want) {
			t.Errorf("want %s in %s", want, got)
		}
	}
	if strings.Contains(got, "6d") || strings.Contains(got, "run needs you") {
		t.Fatalf("display text in --json: %s", got)
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

// Review: a source that stopped working is named even when every row it
// found was snoozed.
func TestLostSourceIsNamedEvenWhenItsRowsAreSnoozed(t *testing.T) {
	now := time.Now()
	stale := []signal.Observed{{Tagged: signal.Tagged{Signal: signal.Signal{ID: "s:1", Project: "/code/acme/api", Kind: signal.Me, Text: "review requested"}, Plugin: "github"}, FirstSeen: now, Stale: true, Snoozed: true}}
	var b bytes.Buffer
	Render(&b, &Data{Checked: 2, RenderedAt: now, Plugins: []signal.PluginStatus{{Name: "github", Status: signal.StatusOff}}, Signals: stale})
	if head := strings.SplitN(b.String(), "\n", 2)[0]; !strings.Contains(head, "github not set up") {
		t.Fatalf("%s", head)
	}
}

// Review: shells opened together print the board once, not once each.
func TestAmbientShowsOnceAcrossConcurrentShells(t *testing.T) {
	amb := Ambient{Home: t.TempDir()}
	var mu sync.Mutex
	shown := 0
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			amb.Run(time.Now(), time.Hour, func() bool {
				mu.Lock()
				shown++
				mu.Unlock()
				time.Sleep(20 * time.Millisecond)
				return true
			})
		}()
	}
	wg.Wait()
	if shown != 1 {
		t.Fatalf("shown %d times", shown)
	}
}

func TestRunsAreRoutedByState(t *testing.T) {
	now := time.Now()
	mk := func(id int, state thread.RunState) thread.View {
		return thread.View{Thread: thread.Thread{ID: id, Project: "/code/acme/billing", Text: "fix the flaky test", Kind: thread.Them, Since: now,
			Run: &thread.Run{Runner: "claude", State: state, Text: "which fixture?", Branch: fmt.Sprintf("sous/run-%d", id)}}}
	}
	unavailable := mk(5, "running")
	unavailable.RunErr = "claude: timed out"
	d := &Data{Checked: 1, RenderedAt: now, Threads: []thread.View{mk(1, "running"), mk(2, "needs_you"), mk(3, "done"), mk(4, "failed"), unavailable}}
	s := Classify(d)
	if len(s.Them) != 2 || len(s.Me) != 3 {
		t.Fatalf("them %d me %d", len(s.Them), len(s.Me))
	}
	for _, r := range s.Me {
		if r.Kind != "me" {
			t.Errorf("a run on you says kind %q in --json", r.Kind)
		}
	}
	var b strings.Builder
	Render(&b, d)
	for _, want := range []string{"running · fix the flaky test", `run needs you · fix the flaky test · "which fixture?"`, "run done, review it · fix the flaky test · sous/run-3", "run failed · fix the flaky test · which fixture?", "(status unavailable: claude: timed out)"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("missing %q in\n%s", want, b.String())
		}
	}
}

// Review fix: a run whose state could not be read makes the headline say ?.
func TestRunStatusUnavailableIsAGap(t *testing.T) {
	now := time.Now()
	v := thread.View{Thread: thread.Thread{ID: 1, Project: "/code/acme/billing", Text: "t", Kind: thread.Them, Since: now, Run: &thread.Run{Runner: "orchid", State: "running"}}, RunErr: "orchid: timed out"}
	s := Classify(&Data{Checked: 1, RenderedAt: now, Threads: []thread.View{v}})
	if !slices.Contains(s.Why, "run status unavailable") {
		t.Fatalf("%q", s.Why)
	}
}

// A plugin's error is cut without splitting a character.
func TestPluginFailuresIsRuneSafe(t *testing.T) {
	msg := "gh: réseau indisponible — la connexion a échoué après trois tentatives"
	if got := PluginFailures(&Data{Plugins: []signal.PluginStatus{{Name: "github", Status: "failed", Error: &msg}}}); !utf8.ValidString(got) {
		t.Fatalf("plugin failure line split a rune: %q", got)
	}
}

// "as of 09:02" is ambiguous on a board that is days old: it shows the day
// unless the board is from today.
func TestBoardSaysWhichDay(t *testing.T) {
	at := time.Date(2026, 9, 24, 9, 2, 0, 0, time.Local)
	for seen, want := range map[time.Duration]string{3 * time.Hour: "as of 09:02 ·", 72 * time.Hour: "as of Thu 24 Sep 09:02 ·"} {
		var b bytes.Buffer
		RenderSaved(&b, &Data{Checked: 1, RenderedAt: at}, at.Add(seen))
		if !strings.Contains(b.String(), want) {
			t.Errorf("seen after %v: %s", seen, b.String())
		}
	}
}

// The board's --json lists every snoozed note under snoozed, ideas too;
// here shows them in place, marked. A run's item carries its log and key.
func TestSnoozedAndRunItems(t *testing.T) {
	now := time.Now()
	later := now.Add(time.Hour)
	idea := thread.View{Thread: thread.Thread{ID: 1, Project: "/code/acme/api", Text: "an idea", Kind: thread.Idea, Since: now, SnoozedUntil: &later}, Snoozed: true}
	run := thread.View{Thread: thread.Thread{ID: 2, Project: "/code/acme/api", Text: "fix it", Kind: thread.Them, Since: now,
		Run: &thread.Run{Runner: "claude", State: thread.RunFailed, Log: "/runs/x/log", Key: "k1"}}}
	j := (&Data{Checked: 1, RenderedAt: now, Threads: []thread.View{idea, run}}).JSON()
	if len(j.Ideas) != 0 || len(j.Snoozed) != 1 || j.Snoozed[0].ID != "1" {
		t.Fatalf("ideas %v snoozed %v", j.Ideas, j.Snoozed)
	}
	if len(j.OnYou) != 1 || j.OnYou[0].Run.Log != "/runs/x/log" || j.OnYou[0].Run.Key != "k1" {
		t.Fatalf("%+v", j.OnYou)
	}
	h := (&HereData{RenderedAt: now, Threads: []thread.View{idea}}).JSON()
	if len(h.Ideas) != 1 || !h.Ideas[0].Snoozed {
		t.Fatalf("here keeps a snoozed idea in place: %+v", h)
	}
	var b strings.Builder
	RenderNote(&b, run, now, "/home/sam")
	if !strings.Contains(b.String(), "(me · ") {
		t.Fatalf("show says a run waiting on you is on you:\n%s", b.String())
	}
}
