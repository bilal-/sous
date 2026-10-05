package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/bilal-/sous/internal/integration"
	"github.com/bilal-/sous/internal/project"
)

// Add identities for task paths outside the provider's cached project list.
// This is the host's private projection; the provider snapshot stays intact.
func (b *Bridge) projectSnapshot(ctx context.Context, snapshot integration.Snapshot) integration.Snapshot {
	snapshot.Projects = append([]project.Project{}, snapshot.Projects...)
	snapshot.Problems = append([]string{}, snapshot.Problems...)
	known := map[string]bool{}
	for _, p := range snapshot.Projects {
		known[filepath.Clean(p.Path)] = true
	}
	for _, item := range snapshot.Items {
		path := filepath.Clean(item.Project)
		if item.Project == "" || known[path] {
			continue
		}
		known[path] = true
		identity, err := b.Provider.Project(ctx, item.Project)
		if err != nil {
			snapshot.Complete = false
			snapshot.Problems = append(snapshot.Problems, fmt.Sprintf("Project identity unavailable for %s: %v", item.Name, err))
			continue
		}
		identity.Path = item.Project
		snapshot.Projects = append(snapshot.Projects, identity)
	}
	return snapshot
}

func sameProjectPath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	a, aerr := filepath.EvalSymlinks(a)
	b, berr := filepath.EvalSymlinks(b)
	return aerr == nil && berr == nil && filepath.Clean(a) == filepath.Clean(b)
}

// Match a project's path or remote, as sous does for notes in linked worktrees.
func projectMatches(snapshot *integration.Snapshot, scope project.Project, path string) bool {
	if sameProjectPath(scope.Path, path) {
		return true
	}
	if snapshot == nil || scope.Remote == nil || *scope.Remote == "" {
		return false
	}
	for _, known := range snapshot.Projects {
		if sameProjectPath(known.Path, path) && known.Remote != nil && *known.Remote == *scope.Remote {
			return true
		}
	}
	return false
}

func projectTracked(snapshot *integration.Snapshot, scope project.Project) bool {
	if snapshot == nil {
		return false
	}
	for _, known := range snapshot.Projects {
		if projectMatches(snapshot, scope, known.Path) {
			return true
		}
	}
	for _, item := range snapshot.Items {
		if projectMatches(snapshot, scope, item.Project) {
			return true
		}
	}
	return false
}
