package runner

import (
	"path/filepath"

	"github.com/bilal-/sous/internal/harness"
)

// run is what the harness needs from run.json to start or resume.
func (m runMeta) run() harness.Run {
	return harness.Run{Prompt: m.Prompt, Session: m.Session, Answer: m.Answer, Worktree: m.Worktree, Dir: filepath.Dir(m.Worktree), GitDirs: m.GitDirs}
}
