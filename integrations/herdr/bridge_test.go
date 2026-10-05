package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/board"
	"github.com/bilal-/sous/internal/integration"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/thread"
)

type hostCall struct {
	Method string
	Params map[string]any
}

type fakeHost struct {
	mu      sync.Mutex
	Calls   []hostCall
	Session Session
	Fail    map[string]error
	Gone    chan struct{}
}

func (h *fakeHost) Call(_ context.Context, method string, params any, result any) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	b, _ := json.Marshal(params)
	var fields map[string]any
	_ = json.Unmarshal(b, &fields)
	h.Calls = append(h.Calls, hostCall{method, fields})
	if err := h.Fail[method]; err != nil {
		return err
	}
	if result == nil {
		return nil
	}
	var answer any = map[string]any{}
	switch method {
	case "session.snapshot":
		answer = map[string]any{"snapshot": h.Session}
	case "workspace.create":
		answer = map[string]any{"workspace": Workspace{ID: "w-new"}}
	case "plugin.list":
		answer = map[string]any{"plugins": []map[string]any{{"plugin_id": pluginID, "enabled": true}}}
	}
	b, _ = json.Marshal(answer)
	return json.Unmarshal(b, result)
}

func (h *fakeHost) Monitor(ctx context.Context) error {
	select {
	case <-h.Gone:
		return errors.New("herdr disconnected")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *fakeHost) calls(method string) []hostCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []hostCall
	for _, call := range h.Calls {
		if call.Method == method {
			out = append(out, call)
		}
	}
	return out
}

type fakeProvider struct {
	Snapshot integration.Snapshot
	Started  chan struct{}
	Stopped  chan struct{}
	Projects map[string]project.Project
}

func (p *fakeProvider) Describe(context.Context) (integration.Description, error) {
	return integration.Describe("test"), nil
}

func (p *fakeProvider) Read(context.Context) (integration.Snapshot, error) { return p.Snapshot, nil }
func (p *fakeProvider) Watch(ctx context.Context, emit func(integration.Event) error) error {
	if p.Started != nil {
		close(p.Started)
	}
	if err := emit(event("snapshot", p.Snapshot)); err != nil {
		return err
	}
	<-ctx.Done()
	if p.Stopped != nil {
		close(p.Stopped)
	}
	return ctx.Err()
}
func (p *fakeProvider) Project(_ context.Context, cwd string) (project.Project, error) {
	if path := p.Projects[cwd]; path.Path != "" {
		return path, nil
	}
	return project.Project{Path: cwd}, nil
}

func event(kind string, snapshot integration.Snapshot) integration.Event {
	return integration.Event{Kind: "event", V: 0, Provider: "sous", Type: kind, Snapshot: &snapshot, Revision: snapshot.Revision, Changes: []integration.Change{}}
}

func snapshot(revision, state string) integration.Snapshot {
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	return integration.Snapshot{Kind: "snapshot", V: 0, Provider: "sous", Revision: revision, Configured: true, Available: true, Complete: true, AsOf: &now,
		Items: []integration.Item{{Key: "n:retry", Section: "on_others", Actions: []string{"open", "show", "done", "snooze"},
			Item: board.Item{ID: "1", Project: "/code/acme/api", Name: "api", Kind: "them", Text: "check retry timeout", Source: "agent",
				Run: &board.RunItem{Runner: "codex", State: thread.RunState(state), Text: "which timeout?"}}}}, Problems: []string{}}
}

func testBridge(t *testing.T) (*Bridge, *fakeHost, *fakeProvider) {
	t.Helper()
	h := &fakeHost{Gone: make(chan struct{}), Session: Session{Workspaces: []Workspace{{ID: "w1"}}, Panes: []Pane{{ID: "w1:p1", Workspace: "w1", Cwd: "/code/acme/api"}}}}
	p := &fakeProvider{Snapshot: snapshot("first", "running")}
	b := &Bridge{State: &store.Store{Home: t.TempDir()}, Host: h, Provider: p, Config: defaultConfig(), Interval: 10 * time.Millisecond}
	return b, h, p
}

func TestBridgeQuietBaselineChangesRecoveryAndNotificationFailure(t *testing.T) {
	b, h, _ := testBridge(t)
	first := snapshot("first", "running")
	if err := b.Accept(context.Background(), event("snapshot", first)); err != nil {
		t.Fatal(err)
	}
	if len(h.calls("notification.show")) != 0 {
		t.Fatal("loading an existing task must be quiet")
	}
	next := snapshot("second", "needs_you")
	next.Items[0].Section, next.Items[0].Kind = "on_you", "me"
	change := event("changes", next)
	change.PreviousRevision = first.Revision
	if err := b.Accept(context.Background(), change); err != nil {
		t.Fatal(err)
	}
	if err := b.Accept(context.Background(), change); err != nil {
		t.Fatal(err)
	}
	if calls := h.calls("notification.show"); len(calls) != 1 || calls[0].Params["sound"] != "request" {
		t.Fatalf("expected one actionable state alert: %+v", calls)
	}
	if err := b.Accept(context.Background(), integration.Event{Kind: "event", V: 0, Provider: "sous", Type: "unavailable", Error: "source unreadable"}); err != nil {
		t.Fatal(err)
	}
	state, err := readState(b.State)
	if err != nil || state.Available || state.Snapshot == nil || len(state.Snapshot.Items) != 1 {
		t.Fatalf("failure discarded the last known work: %+v, %v", state, err)
	}
	metadata := h.calls("workspace.report_metadata")
	if len(metadata) == 0 || !strings.Contains(metadata[len(metadata)-1].Params["tokens"].(map[string]any)["sous_tasks"].(string), "unavailable") {
		t.Fatalf("sidebar hid uncertainty: %+v", metadata)
	}
	if err := b.Accept(context.Background(), event("snapshot", next)); err != nil {
		t.Fatal(err)
	}
	if len(h.calls("notification.show")) != 1 {
		t.Fatal("recovery must not replay all task alerts")
	}
	h.Fail = map[string]error{"notification.show": errors.New("toast unavailable")}
	done := snapshot("third", "done")
	change = event("changes", done)
	change.PreviousRevision = next.Revision
	if err := b.Accept(context.Background(), change); err != nil {
		t.Fatal(err)
	}
	state, err = readState(b.State)
	if err != nil || state.Snapshot.Revision != "third" || state.Warning() == "" {
		t.Fatalf("failed delivery must retain visible work: %+v, %v", state, err)
	}
}

func TestBridgeDoesNotOverwriteNewerState(t *testing.T) {
	b, h, _ := testBridge(t)
	path := filepath.Join(b.State.Home, "bridge.json")
	raw := []byte(`{"version":999}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := b.Accept(context.Background(), event("snapshot", snapshot("first", "running"))); !errors.Is(err, store.ErrNewer) {
		t.Fatalf("newer state: %v", err)
	}
	if got, _ := os.ReadFile(path); !reflect.DeepEqual(got, raw) || len(h.Calls) != 0 {
		t.Fatal("newer state caused writes or host side effects")
	}
}

func TestBridgeStopsProviderWhenHerdrDisconnects(t *testing.T) {
	b, h, p := testBridge(t)
	p.Started, p.Stopped = make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- b.Run(ctx) }()
	select {
	case <-p.Started:
	case <-ctx.Done():
		t.Fatal("provider did not start")
	}
	close(h.Gone)
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal("bridge outlived herdr")
	}
	select {
	case <-p.Stopped:
	default:
		t.Fatal("provider child was left running")
	}
}

func TestOpenTaskFocusesBoundPaneOrCreatesAnArgvBackedPluginPane(t *testing.T) {
	b, h, _ := testBridge(t)
	item := snapshot("first", "running").Items[0]
	h.Session.Panes[0].Tokens = map[string]string{"sous_task": item.Key, "sous_instance": b.instance()}
	if err := b.Open(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	if len(h.calls("pane.focus")) != 1 || len(h.calls("plugin.pane.open")) != 0 {
		t.Fatal("existing linked pane must be focused, not duplicated")
	}
	h.Session.Panes[0].Tokens = nil
	if err := b.Open(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	calls := h.calls("plugin.pane.open")
	if len(calls) != 1 || calls[0].Params["entrypoint"] != "project" || calls[0].Params["workspace_id"] != "w1" {
		t.Fatalf("open did not use the registered argv entrypoint: %+v", calls)
	}
	if calls[0].Params["env"].(map[string]any)["SOUS_HERDR_TASK_KEY"] != item.Key {
		t.Fatal("opened pane lost task identity")
	}
}

func TestProjectCountsResolveWorktreesAndKeepIncompleteSourcesVisible(t *testing.T) {
	b, h, p := testBridge(t)
	h.Session.Panes[0].Cwd = "/code/acme/api-feature"
	remote := "git.example.org/acme/api"
	p.Projects = map[string]project.Project{"/code/acme/api-feature": {Path: "/code/acme/api-feature", Remote: &remote}}
	s := snapshot("first", "running")
	s.Projects = []project.Project{{Path: "/code/acme/api", Remote: &remote}}
	s.Complete, s.Problems = false, []string{"github failed"}
	if err := b.Accept(context.Background(), event("snapshot", s)); err != nil {
		t.Fatal(err)
	}
	calls := h.calls("workspace.report_metadata")
	if len(calls) != 1 || !strings.Contains(calls[0].Params["tokens"].(map[string]any)["sous_tasks"].(string), "?") || !strings.Contains(calls[0].Params["tokens"].(map[string]any)["sous_tasks"].(string), "1 on others") {
		t.Fatalf("worktree scope or source gap lost: %+v", calls)
	}
}

func TestStopDoesNotWaitForMetadataPolling(t *testing.T) {
	b, _, p := testBridge(t)
	b.Interval = time.Hour
	p.Started, p.Stopped = make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- b.Run(ctx) }()
	joined := false
	defer func() {
		cancel()
		if !joined {
			<-done
		}
	}()
	select {
	case <-p.Started:
	case <-ctx.Done():
		t.Fatal("provider did not start")
	}
	if err := store.WriteFile(filepath.Join(b.State.Home, "stop"), []byte("stop"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
		joined = true
	case <-time.After(600 * time.Millisecond):
		t.Fatal("stop waited for a metadata polling interval")
	}
}

func TestTaskLinksDoNotCrossSousInstances(t *testing.T) {
	b, h, _ := testBridge(t)
	item := snapshot("first", "running").Items[0]
	h.Session.Panes[0].Tokens = map[string]string{"sous_task": item.Key, "sous_instance": "another-instance"}
	if err := b.Open(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	if len(h.calls("pane.focus")) != 0 || len(h.calls("plugin.pane.open")) != 1 {
		t.Fatal("a task key from another instance was reused")
	}
}
