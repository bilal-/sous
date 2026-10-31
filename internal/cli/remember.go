package cli

import (
	"io"
	"time"

	"github.com/bilal-/sous/internal/memory"
)

func cmdRemember(e *Env, a argv) int {
	_, scope, code := resolveMemoryProject(e, a.value("p"))
	if code != 0 {
		return code
	}
	body := a.pos[0]
	if body == "-" {
		b, err := io.ReadAll(io.LimitReader(e.Stdin, 32769))
		if err != nil {
			return fail(e, exitFailed, "read memory: %v", err)
		}
		if len(b) > 32768 {
			return fail(e, exitUsage, "memory text must be at most 32768 bytes; nothing was saved")
		}
		body = string(b)
	}
	kind := memory.Kind(a.value("k"))
	if kind == "" {
		kind = memory.Handoff
	}
	r, already, err := memory.Add(e.store(), memory.Input{Scope: scope, Kind: kind, Text: body, Source: a.value("source"), Author: e.Source, Key: a.value("key"), Supersedes: a.value("supersedes"), Verified: a.has("verified")}, time.Now())
	if err != nil {
		return memoryErr(e, err)
	}
	did, said := didRemembered, "remembered "+r.ID
	if already {
		did, said = didAlreadyRemembered, "already remembered as "+r.ID
	}
	return e.changed(changedJSON{ID: r.ID, Did: did, Next: []string{"sous recall --show " + r.ID}}, said)
}
