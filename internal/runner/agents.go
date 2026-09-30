package runner

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
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
			return append(claudeArgs(), "--", m.Prompt)
		},
		resume: func(m runMeta) []string {
			return append(claudeArgs(), "--resume", m.Session, "--", m.Answer)
		},
		session: func(log []byte) string { return claudeResult(log).SessionID },
		last:    func(_ string, log []byte) string { return claudeResult(log).Result },
	},
	"codex": {
		bin: "codex",
		start: func(m runMeta) []string {
			return append([]string{"exec"}, append(codexArgs(m), "--", m.Prompt)...)
		},
		resume: func(m runMeta) []string {
			return append([]string{"exec", "resume"}, append(codexArgs(m), "--", m.Session, m.Answer)...)
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

// claudeArgs: headless, file edits accepted, and the git commands that
// commit on the run's branch allowed (a reply cannot grant a permission, so
// without them a run could never commit). Everything else follows the
// person's own Claude settings.
func claudeArgs() []string {
	return []string{"-p", "--output-format", "json", "--permission-mode", "acceptEdits",
		"--allowedTools", "Bash(git add:*)", "Bash(git commit:*)", "Bash(git status:*)", "Bash(git diff:*)", "Bash(git log:*)"}
}

// codexArgs: the workspace-write sandbox, plus git's objects, refs and logs
// and this worktree's own git folder, which committing writes to and which
// live in the project, outside the worktree. Never the repo's hooks or
// config, which would reach the person's own commits.
func codexArgs(m runMeta) []string {
	var roots []string
	for _, d := range m.GitDirs {
		roots = append(roots, strconv.Quote(d))
	}
	return []string{"--json", "-c", `sandbox_mode="workspace-write"`,
		"-c", "sandbox_workspace_write.writable_roots=[" + strings.Join(roots, ",") + "]",
		"-o", lastFile(m.Worktree)}
}

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
