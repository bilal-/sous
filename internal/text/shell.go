package text

import "strings"

// ShellArg keeps a contextual command usable with spaces and shell metacharacters.
func ShellArg(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_./:-", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
