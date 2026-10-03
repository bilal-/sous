package harness

import (
	"path/filepath"
	"strings"
	"time"
)

// How long a sous hook takes: sous gives up on what it was doing after
// Guard and answers with what it has, so an agent never waits on git or a
// tracker; the agent is told to wait Timeout, with room to spare.
const (
	HookGuard   = 5 * time.Second
	HookTimeout = 2 * HookGuard
)

// The two hook roles: what sous does when an agent session starts, and
// when it ends.
const (
	RoleStart = "session-start"
	RoleEnd   = "session-end"
)

// Cmd is the hook sous puts in an agent's settings: `<exe> hook <role>
// <agent>`.
type Cmd struct{ Exe, Role, Agent string }

// String is the command line, with Exe quoted for the shell when it needs it.
func (c Cmd) String() string { return shellQuote(c.Exe) + " hook " + c.Role + " " + c.Agent }

// Ours: is command a sous hook for c's role and agent, whichever sous binary
// it names? The program must be called sous (or be c.Exe itself), and its
// arguments exactly ours. An Exe of "" matches only by name.
func (c Cmd) Ours(command string) bool {
	tail := " hook " + c.Role + " " + c.Agent
	if prog, ok := strings.CutSuffix(command, tail); ok && oldUnquotedPath(prog) {
		return true // an older, unquoted line, whose path may hold spaces
	}
	args := shellSplit(command)
	if len(args) != 4 || args[1] != "hook" || args[2] != c.Role || args[3] != c.Agent {
		return false
	}
	return filepath.Base(args[0]) == "sous" || c.Exe != "" && args[0] == c.Exe
}

// oldUnquotedPath: prog is nothing but an absolute path to a sous binary,
// as older versions wrote it without quotes (so spaces are allowed, and
// nothing that makes it a shell command: no prefix, no operators).
func oldUnquotedPath(prog string) bool {
	if !strings.HasPrefix(prog, "/") || !strings.HasSuffix(prog, "/sous") || strings.ContainsAny(prog, "'\";&|$`<>(){}=\\") {
		return false
	}
	// A space may sit inside one path ("/Users/Sam Smith/bin/sous"), where
	// the word after it continues that path. A word that starts a new path,
	// or is no path at all, means another program runs sous.
	for _, w := range strings.Split(prog, " ")[1:] {
		if strings.HasPrefix(w, "/") || !strings.Contains(w, "/") {
			return false
		}
	}
	return true
}

// shellQuote quotes s for a POSIX shell when it holds anything but plain
// path characters.
func shellQuote(s string) string {
	for _, c := range s {
		if !(c == '/' || c == '.' || c == '-' || c == '_' || c == '~' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
		}
	}
	return s
}

// shellSplit splits a command line into words the way a POSIX shell would
// for the simple cases hook commands use: spaces, '...', "..." and \.
func shellSplit(s string) []string {
	var words []string
	var cur strings.Builder
	inWord, quote := false, rune(0)
	for i := 0; i < len(s); i++ {
		c := rune(s[i])
		switch {
		case quote == '\'':
			if c == '\'' {
				quote = 0
			} else {
				cur.WriteRune(c)
			}
		case quote == '"':
			if c == '"' {
				quote = 0
			} else if c == '\\' && i+1 < len(s) {
				i++
				cur.WriteByte(s[i])
			} else {
				cur.WriteRune(c)
			}
		case c == '\'' || c == '"':
			quote, inWord = c, true
		case c == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			inWord = true
		case c == ' ' || c == '\t':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(c)
			inWord = true
		}
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words
}

// Program is the program a hook command runs, unquoted: everything before
// " hook ", so an older unquoted path with spaces in it reads whole.
func Program(command string) string {
	if i := strings.LastIndex(command, " hook "); i > 0 {
		command = command[:i]
	}
	return strings.Join(shellSplit(command), " ")
}
