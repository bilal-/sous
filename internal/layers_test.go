package internal_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const module = "github.com/bilal-/sous/"

// tiers is the order AGENTS.md gives: code depends only downward, or
// across its own tier.
var tiers = [][]string{
	{"cli"},
	{"doctor"},
	{"report", "install", "integration"},
	{"board", "filing", "runs", "hook", "launcher"},
	{"signal", "backend", "runner"},
	{"tracker", "plugin", "harness", "thread", "session", "project"},
	{"store", "config", "text", "docs"},
}

// never are the imports AGENTS.md rules out even within the order.
var never = map[string][]string{
	"board":  {"filing", "backend"}, // it is handed a function instead
	"signal": {"backend"},           // both use tracker
}

// envReads are the calls that read settings from the environment, which
// only cli makes.
var envReads = []string{"Getenv", "LookupEnv", "UserHomeDir", "Environ"}

// The layering AGENTS.md describes is the layering the code has: a new
// package must take a place in it, and an import that points up fails.
func TestLayers(t *testing.T) {
	tier := map[string]int{}
	for i, names := range tiers {
		for _, n := range names {
			tier[n] = i
		}
	}
	for pkg, imports := range packages(t) {
		from, ok := tier[pkg]
		if !ok {
			t.Errorf("%s has no place in AGENTS.md's order", pkg)
			continue
		}
		for _, imp := range imports {
			if to, ok := tier[imp]; ok && to < from {
				t.Errorf("%s imports %s, which is above it", pkg, imp)
			}
			for _, no := range never[pkg] {
				if imp == no {
					t.Errorf("%s imports %s, which AGENTS.md rules out", pkg, imp)
				}
			}
		}
	}
}

// Only cli reads settings from the environment. Handing a child process
// the environment it was given (tracker's fallback, the runner's agent) is
// the one other use, and says so where it happens.
func TestOnlyCLIReadsTheEnvironment(t *testing.T) {
	allowed := map[string]bool{"tracker/tracker.go": true, "runner/watch.go": true}
	for _, file := range sources(t) {
		rel, _ := filepath.Rel(".", file)
		if strings.HasPrefix(rel, "cli/") || allowed[rel] {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if x, ok := sel.X.(*ast.Ident); ok && x.Name == "os" {
					for _, r := range envReads {
						if sel.Sel.Name == r {
							t.Errorf("%s reads the environment (os.%s); take it from cli", rel, r)
						}
					}
				}
			}
			return true
		})
	}
}

// packages maps each package under internal (and docs) to the sous
// packages it imports, by their last name. Test helpers and tests are left
// out: they may reach anywhere.
func packages(t *testing.T) map[string][]string {
	out := map[string][]string{}
	for _, file := range append(sources(t), "../docs/docs.go") {
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		pkg := filepath.Base(filepath.Dir(file))
		if out[pkg] == nil {
			out[pkg] = []string{} // a package that imports nothing still has a place
		}
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if strings.HasPrefix(path, module) {
				out[pkg] = append(out[pkg], filepath.Base(path))
			}
		}
	}
	return out
}

// sources are the non-test Go files of every package under internal,
// helpers for tests aside.
func sources(t *testing.T) []string {
	var out []string
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (strings.HasSuffix(d.Name(), "test") || d.Name() == "testutil") {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
