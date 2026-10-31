package project

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAliasesUseDiscoveryPathsWithoutGit(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	physical := filepath.Join(base, "checkout")
	root := filepath.Join(base, "workspace")
	org := filepath.Join(base, "organization")
	for _, dir := range []string{filepath.Join(physical, ".git"), root, org} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for link, target := range map[string]string{
		filepath.Join(org, "api"):          physical,
		filepath.Join(root, "acme"):        org,
		filepath.Join(base, "linked-root"): root,
	} {
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	// Discovery of aliases is a filesystem operation even if git is absent.
	t.Setenv("PATH", t.TempDir())
	want := []Alias{{Path: filepath.Join(root, "acme", "api"), Selector: "acme/api"}}
	for _, roots := range [][]string{{root}, {filepath.Join(base, "linked-root")}, {root, root}} {
		got, err := AliasesForPath(context.Background(), roots, nil, physical)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("%v: %v %v", roots, got, err)
		}
	}
	got, err := AliasesForPath(context.Background(), []string{root}, []string{"acme/*"}, physical)
	if err != nil || len(got) != 0 {
		t.Fatalf("ignored alias: %v %v", got, err)
	}
}

func TestAliasesRefuseIncompleteInventory(t *testing.T) {
	root := t.TempDir()
	if _, err := AliasesForPath(context.Background(), []string{filepath.Join(root, "missing")}, nil, root); err == nil {
		t.Fatal("missing root looked like no aliases")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := AliasesForPath(ctx, []string{root}, nil, root); err != context.Canceled {
		t.Fatalf("canceled discovery: %v", err)
	}
}

func TestAliasesDoNotSkipBrokenEntries(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "api")); err != nil {
		t.Fatal(err)
	}
	if _, err := AliasesForPath(context.Background(), []string{root}, nil, root); err == nil {
		t.Fatal("broken alias looked like complete inventory")
	}
}
