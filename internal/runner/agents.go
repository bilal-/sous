package runner

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// cli is how one agent CLI runs headless: its program, the arguments for a
// new run and for carrying on after a reply, and how to read its session id
// and last message back from what it wrote.
type cli struct {
	bin     string
	start   func(m runMeta) []string
	resume  func(m runMeta) []string
	session func(log []byte) string
	last    func(worktree string, log []byte) string
}

// Every prompt follows "--", so text that starts with a dash is never
// read as a flag. The permissions are the person's own (spec: what the agent may do):
// Claude accepts file edits and the tools its settings allow; Codex runs in
// its workspace-write sandbox. Neither skips permission checks.
var clis = map[string]cli{
	"claude": {
		bin: "claude",
		start: func(m runMeta) []string {
			return []string{"-p", "--output-format", "json", "--permission-mode", "acceptEdits", "--", m.Prompt}
		},
		resume: func(m runMeta) []string {
			return []string{"-p", "--output-format", "json", "--permission-mode", "acceptEdits", "--resume", m.Session, "--", m.Answer}
		},
		session: func(log []byte) string { return claudeResult(log).SessionID },
		last:    func(_ string, log []byte) string { return claudeResult(log).Result },
	},
	"codex": {
		bin: "codex",
		start: func(m runMeta) []string {
			return []string{"exec", "--json", "--sandbox", "workspace-write", "-o", lastFile(m.Worktree), "--", m.Prompt}
		},
		resume: func(m runMeta) []string {
			return []string{"exec", "resume", "--json", "-c", `sandbox_mode="workspace-write"`, "-o", lastFile(m.Worktree), "--", m.Session, m.Answer}
		},
		session: func(log []byte) string {
			if m := codexThread.FindSubmatch(log); m != nil {
				return string(m[1])
			}
			return ""
		},
		last: func(worktree string, _ []byte) string { return readString(filepath.Dir(worktree), "last.txt") },
	},
}

// lastFile is where Codex writes its last message: the run folder, beside
// the worktree, so the agent never sees or commits it.
func lastFile(worktree string) string { return filepath.Join(filepath.Dir(worktree), "last.txt") }

var codexThread = regexp.MustCompile(`"(?:thread_id|session_id)"\s*:\s*"([^"]+)"`)

// claudeResult: the last JSON object Claude printed (--output-format json).
func claudeResult(log []byte) (r struct {
	Result    string `json:"result"`
	SessionID string `json:"session_id"`
}) {
	lines := strings.Split(strings.TrimSpace(string(log)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if json.Unmarshal([]byte(lines[i]), &r) == nil && r.SessionID != "" {
			return r
		}
	}
	return r
}

// BuiltinNames: the built in runners, sorted.
func BuiltinNames() []string {
	names := make([]string, 0, len(clis))
	for n := range clis {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// CLI is the program a built in runner needs, "" for any other name.
func CLI(name string) string { return clis[name].bin }
