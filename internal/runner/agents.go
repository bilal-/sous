package runner

import (
	"path/filepath"

	"github.com/bilal-/sous/internal/harness"
)

// BuiltinNames: the built in runners, one per harness that can run
// headless, sorted.
func BuiltinNames() []string {
	var names []string
	for _, n := range harness.Names() {
		if h, _ := harness.Find(n); h.Headless != nil {
			names = append(names, n)
		}
	}
	return names
}

// CLI is the program a built in runner needs, "" for any other name.
func CLI(name string) string {
	if h, ok := harness.Find(name); ok && h.Headless != nil {
		return h.Bin
	}
	return ""
}

// run is what the harness needs from run.json to start or resume.
func (m runMeta) run() harness.Run {
	return harness.Run{Prompt: m.Prompt, Session: m.Session, Answer: m.Answer, Worktree: m.Worktree, Dir: filepath.Dir(m.Worktree), GitDirs: m.GitDirs}
}
