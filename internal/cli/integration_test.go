package cli

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/board"
	"github.com/bilal-/sous/internal/integration"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/signal"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/thread"
)

func TestIntegrationWatchProcessStreamsAndStopsWithItsHost(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/api", true)
	f.runIn(p, "note", "-k", "me", "check retry timeout")
	if _, errs, code := f.run("--refresh"); code != 0 {
		t.Fatalf("initial board: %d %q", code, errs)
	}
	cmd := sousCmd(f, f.Home, "integration", "watch", "--json")
	cmd.Stdin, cmd.Stderr = strings.NewReader(""), io.Discard
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- cmd.Wait() }()
	joined := false
	t.Cleanup(func() {
		if !joined {
			cmd.Process.Kill()
			<-finished
		}
	})
	reader := bufio.NewReader(pipe)
	read := func() integration.Event {
		t.Helper()
		got := make(chan struct {
			line []byte
			err  error
		}, 1)
		go func() {
			line, err := reader.ReadBytes('\n')
			got <- struct {
				line []byte
				err  error
			}{line, err}
		}()
		select {
		case value := <-got:
			var event integration.Event
			if value.err != nil || json.Unmarshal(value.line, &event) != nil || event.Kind != "event" {
				t.Fatalf("watch must emit one JSON value per line: %q %v", value.line, value.err)
			}
			return event
		case <-time.After(5 * time.Second):
			t.Fatal("watch stopped delivering state")
			return integration.Event{}
		}
	}
	baseline := read()
	if baseline.Type != "snapshot" || baseline.Snapshot == nil || len(baseline.Snapshot.Items) != 1 || len(baseline.Changes) != 0 {
		t.Fatalf("startup must be a baseline, not new-task notifications: %+v", baseline)
	}
	f.run("edit", "1", "check updated timeout")
	changed := read()
	if changed.Type != "changes" || changed.PreviousRevision != baseline.Revision || len(changed.Changes) != 1 || changed.Changes[0].Type != "item.updated" || changed.Changes[0].Item.Text != "check updated timeout" {
		t.Fatalf("local edit did not reach the host: %+v", changed)
	}
	f.writeConfig("roots = []\n")
	configured := read()
	if configured.Snapshot == nil || configured.Snapshot.Configured || len(configured.Snapshot.Items) != 1 {
		t.Fatalf("watch must notice config edits and keep known work: %+v", configured)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		joined = true
		if err != nil {
			t.Fatalf("watch did not stop cleanly: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watch outlived cancellation")
	}
}

func TestIntegrationDiscoveryDoesNotNeedConfiguration(t *testing.T) {
	f := fixture(t)
	f.writeConfig("not valid TOML = [")
	out, errs, code := f.run("integration", "--json")
	var got struct {
		V          int    `json:"v"`
		Provider   string `json:"provider"`
		Protocol   string `json:"protocol"`
		Operations map[string]struct {
			Argv    []string `json:"argv"`
			Offline bool     `json:"offline"`
			Format  string   `json:"format"`
		} `json:"operations"`
	}
	if code != 0 || errs != "" || json.Unmarshal([]byte(out), &got) != nil {
		t.Fatalf("discovery: %d %q %q", code, out, errs)
	}
	if got.V != 0 || got.Provider != "sous" || got.Protocol != "sous.integration" {
		t.Fatalf("wrong provider identity: %+v", got)
	}
	for _, op := range []string{"snapshot", "watch", "context"} {
		if !got.Operations[op].Offline || len(got.Operations[op].Argv) == 0 {
			t.Errorf("%s must be discoverable and offline: %+v", op, got.Operations[op])
		}
	}
	if got.Operations["watch"].Format != "ndjson" {
		t.Fatalf("watch format: %+v", got.Operations["watch"])
	}
	if _, err := os.Stat(filepath.Join(f.SousHome, "cache.json")); !os.IsNotExist(err) {
		t.Fatal("discovery must not create a board")
	}
}

// A native host must distinguish an unavailable snapshot from an empty list,
// and asking for one must never start a network refresh behind its back.
func TestIntegrationSnapshotMissingOrNewerCache(t *testing.T) {
	f := fixture(t)
	called := filepath.Join(f.Home, "tracker-called")
	f.bin("gh", `touch "`+called+`"; exit 99`)
	out, errs, code := f.run("integration", "snapshot", "--json")
	var got errorJSON
	if code != exitNotReady || json.Unmarshal([]byte(out), &got) != nil || !strings.Contains(got.Error, "--refresh") || errs == "" {
		t.Fatalf("missing cache must be unavailable: %d %q %q", code, out, errs)
	}
	cache := filepath.Join(f.SousHome, "cache.json")
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Fatal("snapshot must not bootstrap a refresh")
	}
	newer := []byte(`{"version":999,"rendered_at":null,"board":null}`)
	if err := os.WriteFile(cache, newer, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, code := f.run("integration", "snapshot", "--json"); code != exitFailed {
		t.Fatalf("newer cache: %d", code)
	}
	if b, _ := os.ReadFile(cache); string(b) != string(newer) {
		t.Fatal("a newer state file was changed")
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatal("snapshot called a tracker")
	}
}

func TestIntegrationSnapshotKeepsUncertaintyAndCurrentNotes(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/api", true)
	f.runIn(p, "note", "-k", "me", "check retry fix")
	f.fileAs(1, "github:acme/api#12")
	now := time.Now().UTC().Add(-time.Hour)
	st := &store.Store{Home: f.SousHome}
	views, err := thread.Open(st, now)
	if err != nil {
		t.Fatal(err)
	}
	views[0].Upstream, views[0].UpstreamErr = "error", "tracker unavailable"
	d := &board.Data{Projects: []project.Project{{Path: p, Name: "api", Org: "acme"}}, Threads: views, Checked: 1, RenderedAt: now,
		Plugins: []signal.PluginStatus{{Name: "github", Status: signal.StatusFailed}},
		Signals: []signal.Observed{{Tagged: signal.Tagged{Signal: signal.Signal{ID: "s:000000000001", Project: p, Kind: signal.Me, Text: "review requested"}, Plugin: "github"}, FirstSeen: now, Stale: true}}}
	if err := board.WriteCache(st, d); err != nil {
		t.Fatal(err)
	}
	f.run("edit", "1", "check updated retry fix")
	called := filepath.Join(f.Home, "tracker-called")
	f.bin("gh", `touch "`+called+`"; exit 99`)
	out, errs, code := f.run("integration", "snapshot", "--json")
	var got struct {
		V        int       `json:"v"`
		Revision string    `json:"revision"`
		AsOf     time.Time `json:"as_of"`
		Complete bool      `json:"complete"`
		Problems []string  `json:"problems"`
		Items    []struct {
			board.Item
			Section string   `json:"section"`
			Actions []string `json:"actions"`
		} `json:"items"`
	}
	if code != 0 || errs != "" || json.Unmarshal([]byte(out), &got) != nil {
		t.Fatalf("snapshot: %d %q %q", code, out, errs)
	}
	if got.V != 0 || got.Revision == "" || !got.AsOf.Equal(now) || got.Complete || len(got.Problems) == 0 || len(got.Items) != 2 {
		t.Fatalf("snapshot lost uncertainty: %+v", got)
	}
	for _, item := range got.Items {
		if item.Section != "on_you" {
			t.Errorf("wrong section: %+v", item)
		}
		if item.ID == "1" && (item.Text != "check updated retry fix" || item.Upstream == nil || item.Upstream.Error != "tracker unavailable" || !slices.Contains(item.Actions, "done")) {
			t.Errorf("note changed incorrectly: %+v", item)
		}
		if signal.IsID(item.ID) && (!item.Stale || slices.Contains(item.Actions, "done") || slices.Contains(item.Actions, "show")) {
			t.Errorf("signal must stay stale and cannot be closed as a note: %+v", item)
		}
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatal("snapshot asked the remote tracker about a filed note")
	}
	f.run("done", "1")
	out, _, _ = f.run("integration", "snapshot", "--json")
	if json.Unmarshal([]byte(out), &got) != nil || len(got.Items) != 1 || got.Items[0].ID != "s:000000000001" {
		t.Fatalf("a closure must appear without refreshing the board: %s", out)
	}
}

func TestIntegrationCaptureActionTreatsTextAsData(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/a pi", true)
	out, errs, code := f.run("integration", "--json")
	var got struct {
		Actions []struct {
			ID   string   `json:"id"`
			Argv []string `json:"argv"`
			Cwd  string   `json:"cwd"`
		} `json:"actions"`
	}
	if code != 0 || errs != "" || json.Unmarshal([]byte(out), &got) != nil {
		t.Fatalf("discovery: %d %s %s", code, errs, out)
	}
	for _, a := range got.Actions {
		if a.ID != "note" {
			continue
		}
		text := "--help; $(touch should-not-exist)\nfollow up on the parser"
		args := append([]string{}, a.Argv[1:]...)
		for i, arg := range args {
			switch arg {
			case "{project}":
				args[i] = p
			case "{text}":
				args[i] = text
			}
		}
		if a.Cwd != "{project}" {
			t.Fatal("capture must execute in the selected project's directory")
		}
		out, errs, code = f.runIn(p, args...)
		if code != 0 {
			t.Fatalf("advertised capture failed: %d %q %q", code, out, errs)
		}
		note, err := thread.Get(&store.Store{Home: f.SousHome}, 1)
		if err != nil || note.Text != strings.Join(strings.Fields(text), " ") || note.Project != p {
			t.Fatalf("capture changed text or project: %+v %v", note, err)
		}
		return
	}
	t.Fatal("discovery did not advertise note capture")
}

func TestIntegrationRefusesUnknownCall(t *testing.T) {
	f := fixture(t)
	if _, errs, code := f.run("integration", "unknown"); code != exitUsage || !strings.Contains(errs, "usage:") {
		t.Fatalf("unknown integration call: %d %q", code, errs)
	}
}
