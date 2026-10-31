package memory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/project"
)

func TestCurrentDirectoryPreservesPrivateScopeBoundaries(t *testing.T) {
	base := t.TempDir()
	physical := filepath.Join(base, "checkout")
	if err := os.MkdirAll(filepath.Join(physical, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	roots := []string{filepath.Join(base, "work"), filepath.Join(base, "personal")}
	for _, root := range roots {
		if err := os.MkdirAll(filepath.Join(root, "acme"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(physical, filepath.Join(root, "acme", "api")); err != nil {
			t.Fatal(err)
		}
	}
	remote := "git.example.org/acme/api"
	p := project.Project{Path: physical, Remote: &remote}
	for _, order := range [][]string{roots, {roots[1], roots[0]}} {
		cfg := &config.Config{Roots: order}
		var ambiguous AmbiguousScope
		if _, err := ResolveScope(context.Background(), cfg, p, CurrentDirectory); !errors.As(err, &ambiguous) {
			t.Fatalf("ambiguous private scopes: %v", err)
		}
		got, err := ResolveScope(context.Background(), cfg, p, Explicit)
		if err != nil || got != ForProject(cfg, p) {
			t.Fatalf("explicit scope changed: %#v %v", got, err)
		}
	}
	// A configured shared workspace and remote already denote one scope.
	cfg := &config.Config{Roots: roots, Projects: map[string]map[string]string{
		"acme/api": {"workspace": "shared"}, project.OrgName(physical): {"workspace": "shared"},
	}}
	got, err := ResolveScope(context.Background(), cfg, p, CurrentDirectory)
	if err != nil || got != ForProject(cfg, p) {
		t.Fatalf("equivalent aliases changed scope: %#v %v", got, err)
	}
	p.Remote = nil
	var ambiguous AmbiguousScope
	if _, err := ResolveScope(context.Background(), cfg, p, CurrentDirectory); !errors.As(err, &ambiguous) {
		t.Fatalf("remote-less paths were silently merged: %v", err)
	}
}
