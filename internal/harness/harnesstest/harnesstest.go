// Package harnesstest fakes the built in agents for tests.
package harnesstest

import (
	"strconv"
	"strings"
)

// Session is the session every fake agent reports.
const Session = "s"

// Says is the end of a fake agent's script: it reports msg as each built
// in agent ends a headless run, all at once (Claude Code's result, Codex's
// thread and its -o file, Antigravity's conversation, opencode's events),
// so every built in runner finds Session and msg. msg holds no single
// quote.
func Says(msg string) string {
	if strings.Contains(msg, "'") {
		panic("harnesstest.Says: a single quote in " + msg)
	}
	q := strconv.Quote(msg)
	return `out=/dev/null; prev=; for x in "$@"; do [ "$prev" = "-o" ] && out="$x"; prev="$x"; done
echo '{"result":` + q + `,"session_id":"` + Session + `"}'
echo '{"thread_id":"` + Session + `"}'
echo '` + msg + `' > "$out"
echo '{"conversation_id":"` + Session + `","response":` + q + `}'
echo '{"type":"text","sessionID":"` + Session + `","part":{"type":"text","text":` + q + `}}'`
}
