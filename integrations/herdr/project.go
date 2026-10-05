package main

import (
	"path/filepath"

	"github.com/bilal-/sous/internal/integration"
	"github.com/bilal-/sous/internal/project"
)

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
