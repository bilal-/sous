package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/board"
	"github.com/bilal-/sous/internal/integration"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/thread"
)

func worktreeProvider(t *testing.T) (*CLIProvider, string, string) {
	t.Helper()
	home := t.TempDir()
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"gh", "glab", "claude", "codex", "agy", "opencode"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 99\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	env := []string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"), "HOME=" + home,
		"SOUS_HOME=" + filepath.Join(home, "sous"), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0",
		"SOUS_HERDR_TEST_REAL_CLI=1"}
	repo, worktree := filepath.Join(home, "code", "acme", "api"), filepath.Join(home, "worktrees", "api-feature")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git fixture failed: %s: %v", out, err)
		}
	}
	git("init", "-b", "main", repo)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# API\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("-C", repo, "add", "README.md")
	git("-C", repo, "-c", "user.name=Sam", "-c", "user.email=sam@git.example.org", "commit", "-m", "Initial API")
	git("-C", repo, "remote", "add", "origin", "https://git.example.org/acme/api.git")
	git("-C", repo, "worktree", "add", "-b", "feature/parser", worktree)
	var err error
	repo, err = filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	worktree, err = filepath.EvalSymlinks(worktree)
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return &CLIProvider{Binary: binary, Env: env}, repo, worktree
}

func TestWorktreeTaskCountsScopeAndLinksUseRepositoryIdentity(t *testing.T) {
	p, repo, worktree := worktreeProvider(t)
	ctx := context.Background()
	resolved, err := p.Project(ctx, worktree)
	if err != nil {
		t.Fatal(err)
	}
	b, h, _ := testBridge(t)
	b.Provider = p
	h.Session.Panes[0].Cwd = worktree
	s := snapshot("worktree", "running")
	s.Items[0].Project = repo
	s.Projects = []project.Project{project.Describe(repo)}
	if err := b.Accept(ctx, event("snapshot", s)); err != nil {
		t.Fatal(err)
	}
	calls := h.calls("workspace.report_metadata")
	label := calls[len(calls)-1].Params["tokens"].(map[string]any)["sous_tasks"].(string)
	if !strings.Contains(label, "1 on others") {
		t.Errorf("worktree sidebar lost the repository's task: %q", label)
	}
	state, err := readState(b.State)
	if err != nil {
		t.Fatal(err)
	}
	v := View{Scope: "space", Project: resolved.Path, Remote: resolved.Remote}
	if len(v.items(state)) != 1 {
		t.Error("this Space view lost the repository's task")
	}
	if err := b.Open(ctx, s.Items[0]); err != nil {
		t.Fatal(err)
	}
	if len(h.calls("workspace.create")) != 0 {
		t.Error("task link created a duplicate Space instead of using the worktree's Space")
	}
	panes := h.calls("plugin.pane.open")
	if len(panes) != 1 || panes[0].Params["workspace_id"] != "w1" {
		t.Fatalf("task did not open in its existing worktree Space: %+v", panes)
	}
}

func TestSidebarDoesNotReportZeroForAnUntrackedProject(t *testing.T) {
	b, h, _ := testBridge(t)
	h.Session.Panes[0].Cwd = "/code/acme/other"
	s := snapshot("tracked", "running")
	s.Projects = []project.Project{{Path: "/code/acme/api"}}
	if err := b.Accept(context.Background(), event("snapshot", s)); err != nil {
		t.Fatal(err)
	}
	calls := h.calls("workspace.report_metadata")
	label := calls[len(calls)-1].Params["tokens"].(map[string]any)["sous_tasks"].(string)
	if !strings.Contains(label, "not tracked") {
		t.Fatalf("untracked project looked empty: %q", label)
	}
}

func TestSymlinkedProjectWithoutARemoteUsesItsPhysicalIdentity(t *testing.T) {
	p, repo, _ := worktreeProvider(t)
	cmd := exec.Command("git", "-C", repo, "remote", "remove", "origin")
	cmd.Env = p.Env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git fixture failed: %s: %v", out, err)
	}
	alias := filepath.Join(t.TempDir(), "api")
	if err := os.Symlink(repo, alias); err != nil {
		t.Fatal(err)
	}
	b, h, _ := testBridge(t)
	b.Provider = p
	h.Session.Panes[0].Cwd = alias
	s := snapshot("symlink", "running")
	s.Items[0].Project = alias
	s.Projects = []project.Project{{Path: alias}}
	if err := b.Accept(context.Background(), event("snapshot", s)); err != nil {
		t.Fatal(err)
	}
	calls := h.calls("workspace.report_metadata")
	label := calls[len(calls)-1].Params["tokens"].(map[string]any)["sous_tasks"].(string)
	if !strings.Contains(label, "1 on others") {
		t.Fatalf("symlinked project's task disappeared: %q", label)
	}
}

func TestNotesFromAnOutOfRootWorktreeMatchTheMainSpace(t *testing.T) {
	p, repo, worktree := worktreeProvider(t)
	var sousHome string
	for _, entry := range p.Env {
		if value, ok := strings.CutPrefix(entry, "SOUS_HOME="); ok {
			sousHome = value
		}
	}
	st := &store.Store{Home: sousHome}
	now := time.Now().UTC()
	if err := board.WriteCache(st, &board.Data{Projects: []project.Project{project.Describe(repo)}, Checked: 1, RenderedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := thread.Note(st, project.Describe(worktree), thread.Me, "check parser handoff", "human", now); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	s, err := (integration.Reader{Store: st, Configured: true, Now: now}).Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b, h, _ := testBridge(t)
	b.Provider = p
	h.Session.Panes[0].Cwd = repo
	if err := b.Accept(ctx, event("snapshot", s)); err != nil {
		t.Fatal(err)
	}
	calls := h.calls("workspace.report_metadata")
	label := calls[len(calls)-1].Params["tokens"].(map[string]any)["sous_tasks"].(string)
	if !strings.Contains(label, "1 on you") {
		t.Fatalf("main Space lost its worktree's private note: %q", label)
	}
	identity, err := p.Project(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	view := View{Scope: "space", Project: identity.Path, Remote: identity.Remote}
	state, err := readState(b.State)
	if err != nil || len(view.items(state)) != 1 {
		t.Fatalf("main Space view lost the worktree note: %v", err)
	}
	if err := b.Open(ctx, s.Items[0]); err != nil {
		t.Fatal(err)
	}
	if len(h.calls("workspace.create")) != 0 {
		t.Fatal("opening the worktree note created a duplicate Space")
	}
}

type unavailableProjectProvider struct{ *fakeProvider }

func (p unavailableProjectProvider) Project(context.Context, string) (project.Project, error) {
	return project.Project{}, errors.New("identity unavailable")
}

func TestMissingTaskProjectIdentityIsExplicitAndDoesNotMutateTheProviderSnapshot(t *testing.T) {
	b, _, p := testBridge(t)
	b.Provider = unavailableProjectProvider{p}
	s := snapshot("first", "running")
	if err := b.Accept(context.Background(), event("snapshot", s)); err != nil {
		t.Fatal(err)
	}
	state, err := readState(b.State)
	if err != nil || state.Snapshot.Complete || len(state.Snapshot.Items) != 1 || len(state.Snapshot.Problems) == 0 {
		t.Fatalf("missing identity was concealed or known work was lost: %v", err)
	}
	if !s.Complete || len(s.Projects) != 0 || len(s.Problems) != 0 {
		t.Fatal("host projection changed the provider's snapshot")
	}
}

func TestTaskLinksPreferTheExactProjectSpaceBeforeRemoteAliases(t *testing.T) {
	b, h, p := testBridge(t)
	remote := "git.example.org/acme/api"
	p.Projects = map[string]project.Project{
		"/code/acme/api-feature": {Path: "/code/acme/api-feature", Remote: &remote},
		"/code/acme/api":         {Path: "/code/acme/api", Remote: &remote},
	}
	h.Session.Workspaces = []Workspace{{ID: "w-feature"}, {ID: "w-main"}}
	h.Session.Panes = []Pane{
		{ID: "w-feature:p1", Workspace: "w-feature", Cwd: "/code/acme/api-feature"},
		{ID: "w-main:p1", Workspace: "w-main", Cwd: "/code/acme/api"},
	}
	s := snapshot("first", "running")
	s.Projects = []project.Project{{Path: "/code/acme/api", Remote: &remote}}
	if err := b.Accept(context.Background(), event("snapshot", s)); err != nil {
		t.Fatal(err)
	}
	if err := b.Open(context.Background(), s.Items[0]); err != nil {
		t.Fatal(err)
	}
	calls := h.calls("plugin.pane.open")
	if len(calls) != 1 || calls[0].Params["workspace_id"] != "w-main" {
		t.Fatalf("remote alias won over the exact project Space: %+v", calls)
	}
}
