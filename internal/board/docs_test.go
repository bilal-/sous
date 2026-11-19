package board

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/memory"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/session"
	"github.com/bilal-/sous/internal/signal"
	"github.com/bilal-/sous/internal/thread"
)

// The examples people read in the README and the guide are what sous prints,
// drawn here by the real renderer. When the board's wording, order or
// columns change, this fails until the docs show the new output.

var docNow = time.Date(2026, 9, 27, 9, 2, 0, 0, time.UTC)

func docDays(n int) time.Time { return docNow.Add(-time.Duration(n) * 24 * time.Hour) }

func readDoc(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// indent is s as a Markdown code block indented by four spaces.
func indent(s string) string {
	var b strings.Builder
	for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if l != "" {
			b.WriteString("    ")
		}
		b.WriteString(l + "\n")
	}
	return b.String()
}

func TestReadmeBoardIsWhatSousPrints(t *testing.T) {
	ref := "github:oss/app-next#14"
	d := &Data{
		Projects: []project.Project{{Path: "/ws/studio/billing"}, {Path: "/ws/oss/app-next"}, {Path: "/ws/acme/mobile-app"}, {Path: "/ws/acme/chime"}, {Path: "/ws/oss/audio-app"}},
		Threads: []thread.View{
			{Thread: thread.Thread{ID: 1, Project: "/ws/studio/billing", Kind: thread.Me, Text: "send the pricing copy to Sam", Since: docDays(6)}},
			{Thread: thread.Thread{ID: 2, Project: "/ws/acme/mobile-app", Kind: thread.Them, Text: "waiting on app store review", Since: docDays(4)}},
		},
		Signals: []signal.Observed{
			{Tagged: signal.Tagged{Signal: signal.Signal{ID: "s:0b4ac59dfd33", Project: "/ws/oss/app-next", Kind: signal.Me, Text: "review requested · PR #14 json api", Ref: &ref}}, FirstSeen: docDays(2)},
			{Tagged: signal.Tagged{Signal: signal.Signal{ID: "s:fe9c4922caf6", Project: "/ws/acme/chime", Kind: signal.Unfinished, Text: "6 commits unpushed · main"}}, FirstSeen: docDays(6)},
			{Tagged: signal.Tagged{Signal: signal.Signal{ID: "s:4515743b9362", Project: "/ws/oss/audio-app", Kind: signal.Unfinished, Text: "11 files uncommitted · feat/history"}}, FirstSeen: docDays(90)},
		},
		Next: []NextStep{
			{Project: "/ws/oss/audio-app", Step: "wire waveform scrub to undo", Updated: docDays(14), Source: "session", Branch: "feat/history"},
			{Project: "/ws/acme/chime", Step: "retry a slow shard, then deploy", Updated: docDays(6), Source: project.StatusFile},
		},
		Plugins: []signal.PluginStatus{{Name: "git", Status: "ok"}, {Name: "github", Status: "ok"}},
		Checked: 26, RenderedAt: docNow,
	}
	var b bytes.Buffer
	b.WriteString(Header("~/.local/bin/sous") + "\n")
	Render(&b, d)
	if !strings.Contains(readDoc(t, "README.md"), "```\n"+b.String()+"```\n") {
		t.Errorf("README.md's board is not what sous prints. Put this in its code block:\n%s", b.String())
	}
}

func TestReadmeHereIsWhatSousPrints(t *testing.T) {
	lastCommit := docDays(1)
	endedMain := docDays(1)
	endedBranch := docDays(3)
	verifiedAt := docDays(10)
	msgMain := "isolated the flaky shard test; staging canary is ready"
	msgBranch := "added queue latency histogram"
	d := &HereData{
		Project: "/ws/acme/chime", Name: "chime", RenderedAt: docNow,
		Facts: project.Facts{
			Branch:      "main",
			LastCommit:  &lastCommit,
			LastSubject: "Add retry backoff around webhook delivery",
		},
		Session: &session.Session{
			Agent:       "claude",
			SessionID:   "cl-1",
			Ended:       endedMain,
			Branch:      "main",
			LastMessage: &msgMain,
		},
		RecentSessions: []session.Session{{
			Agent:       "codex",
			SessionID:   "cx-1",
			Ended:       endedBranch,
			Branch:      "feat/metrics",
			LastMessage: &msgBranch,
		}},
		Next: &NextStep{
			Project: "/ws/acme/chime",
			Step:    "retry a slow shard, then deploy",
			Updated: docDays(6),
			Source:  project.StatusFile,
		},
		Status: &project.StatusDoc{
			Exists:   true,
			Path:     project.StatusFile,
			Updated:  docDays(6),
			NextStep: "retry a slow shard, then deploy",
			Summary:  "Webhook queue cutover is live at 50%. Waiting on Priya's sign-off for 100%.",
		},
		Signals: []signal.Observed{
			{Tagged: signal.Tagged{Signal: signal.Signal{ID: "s:fe9c4922caf6", Project: "/ws/acme/chime", Kind: signal.Unfinished, Text: "6 commits unpushed · main"}}, FirstSeen: docDays(6)},
		},
		Threads: []thread.View{
			{Thread: thread.Thread{ID: 4, Project: "/ws/acme/chime", Kind: thread.Me, Text: "confirm alert threshold with Priya", Since: docDays(2)}},
		},
		Context: Context{
			Available:    true,
			Scope:        memory.Scope{Project: "/ws/acme/chime"},
			historyFlag:  "-p",
			historyValue: "/ws/acme/chime",
			historyLabel: "previous decisions and handoffs",
			Memories: []memory.Record{{
				ID:         "m:1",
				Scope:      memory.Scope{Project: "/ws/acme/chime"},
				Kind:       memory.Decision,
				Text:       "Keep webhook delivery idempotent on event ID",
				Source:     "docs/webhooks.md",
				Author:     "claude",
				VerifiedAt: &verifiedAt,
			}},
		},
	}
	var b bytes.Buffer
	RenderHere(&b, d, docNow, false)
	if !strings.Contains(readDoc(t, "README.md"), "```\n"+b.String()+"```\n") {
		t.Errorf("README.md's sous here block is not what sous prints. Put this in its code block:\n%s", b.String())
	}
}

func TestGuideRunExamplesAreWhatSousPrints(t *testing.T) {
	guide := readDoc(t, "docs/guide.md")
	run := func(state thread.RunState, text string) thread.View {
		return thread.View{Thread: thread.Thread{ID: 7, Project: "/ws/acme/billing", Kind: thread.Them, Text: "fix the flaky test", Since: docNow.Add(-4 * time.Minute),
			Run: &thread.Run{Runner: "claude", Ref: "claude:7", State: state, Text: text}}}
	}

	var b bytes.Buffer
	b.WriteString(Header("~/.local/bin/sous") + "\n")
	Render(&b, &Data{
		Projects: []project.Project{{Path: "/ws/acme/billing"}},
		Threads:  []thread.View{run(thread.RunRunning, "")},
		Plugins:  []signal.PluginStatus{{Name: "git", Status: "ok"}},
		Checked:  26, RenderedAt: docNow,
	})
	if !strings.Contains(guide, indent(b.String())) {
		t.Errorf("docs/guide.md's running board is not what sous prints. Indent this by four spaces:\n%s", b.String())
	}

	asks := run(thread.RunNeedsYou, "which fixture should win?")
	asks.Run.Question = &thread.RunQuestion{ID: "q", Token: "t", Text: "which fixture should win?"}
	waiting := RunsWaiting([]thread.View{asks})
	if !strings.Contains(guide, indent(waiting)) {
		t.Errorf("docs/guide.md's session start is not what sous prints. Indent this by four spaces:\n%s", waiting)
	}
}

// The command guide's run groups quote rows from the board_runs golden.
func TestCommandGuideRunRowsAreWhatSousPrints(t *testing.T) {
	guide := readDoc(t, "docs/commands.md")
	g, err := os.ReadFile(filepath.Join("testdata", "board_runs.golden"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"91", "92", "88"} {
		var row string
		for _, l := range strings.Split(string(g), "\n") {
			if strings.HasPrefix(l, "  "+id+" ") {
				row = l
			}
		}
		if row == "" || !strings.Contains(guide, row+"\n") {
			t.Errorf("docs/commands.md should show run %s as sous prints it:\n%s", id, row)
		}
	}
}
