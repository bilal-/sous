package harness

import (
	"path/filepath"
	"strings"
)

// The two hook roles: what sous does when an agent session starts, and
// when it ends.
const (
	RoleStart = "session-start"
	RoleEnd   = "session-end"
)

// Command is the hook command line for exe: `<exe> hook <role> <agent>`,
// with exe quoted for the shell when it needs it.
func Command(exe, role, agent string) string {
	return shellQuote(exe) + " hook " + role + " " + agent
}

// IsOurs: is command a sous hook for role and agent, whichever sous binary
// it names? The program must be called sous (or be exe itself), and its
// arguments exactly ours.
func IsOurs(command, role, agent string, exe ...string) bool {
	tail := " hook " + role + " " + agent
	if prog, ok := strings.CutSuffix(command, tail); ok && oldUnquotedPath(prog) {
		return true // an older, unquoted line, whose path may hold spaces
	}
	args := shellSplit(command)
	if len(args) != 4 || args[1] != "hook" || args[2] != role || args[3] != agent {
		return false
	}
	return filepath.Base(args[0]) == "sous" || len(exe) > 0 && args[0] == exe[0]
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
