package cli

import (
	"errors"
	"slices"
	"time"

	"github.com/bilal-/sous/internal/effort"
	"github.com/bilal-/sous/internal/memory"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/thread"
)

func memoryErr(e *Env, err error) int {
	var ambiguous memory.AmbiguousScope
	var mv memory.ValidationError
	var ev effort.ValidationError
	var tv thread.ValidationError
	if errors.As(err, &ambiguous) || errors.As(err, &mv) || errors.As(err, &ev) || errors.As(err, &tv) || errors.Is(err, memory.ErrNotFound) || errors.Is(err, effort.ErrNotFound) || errors.Is(err, thread.ErrNotFound) {
		return fail(e, exitUsage, "%v", err)
	}
	return fail(e, exitFailed, "%v", err)
}
func flagsOnly(e *Env, a argv, allowed ...string) int {
	for key := range a.set {
		if !slices.Contains(allowed, key) {
			return fail(e, exitUsage, "--%s is not used by this operation", key)
		}
	}
	return 0
}
func cmdMemory(e *Env, a argv) int {
	action, id := a.pos[0], a.pos[1]
	var err error
	did := ""
	switch action {
	case "verify":
		if code := flagsOnly(e, a, "source"); code != 0 {
			return code
		}
		err = memory.Verify(e.store(), id, a.value("source"), time.Now())
		did = didVerified
	case "forget":
		if code := flagsOnly(e, a); code != 0 {
			return code
		}
		err = memory.Forget(e.store(), id, time.Now())
		did = didForgotten
	case "share", "unshare":
		if code := flagsOnly(e, a, "with"); code != 0 {
			return code
		}
		if !a.has("with") {
			return fail(e, exitUsage, "%s needs --with <project>", action)
		}
		_, scope, code := resolveMemoryProject(e, a.value("with"))
		if code != 0 {
			return code
		}
		err = memory.Share(e.store(), id, scope, action == "share")
		did = didShared
		if action == "unshare" {
			did = didUnshared
		}
	default:
		return fail(e, exitUsage, "memory operation must be verify, forget, share or unshare")
	}
	if err != nil {
		return memoryErr(e, err)
	}
	return e.changed(changedJSON{ID: id, Did: did, Next: []string{"sous recall --show " + id}}, id+" "+did)
}

func scopeMode(term string) memory.SelectionMode {
	if term == "" || term == "." {
		return memory.CurrentDirectory
	}
	return memory.Explicit
}

func resolveMemoryProject(e *Env, term string) (project.Project, memory.Scope, int) {
	p, code := resolveProject(e, term)
	if code != 0 {
		return p, memory.Scope{}, code
	}
	if scopeMode(term) == memory.CurrentDirectory && e.cfgErr != nil {
		return p, memory.Scope{}, fail(e, exitFailed, "private context unavailable: %v", e.cfgErr)
	}
	sc, err := memory.ResolveScope(e.ctx(), e.Cfg, p, scopeMode(term))
	if err != nil {
		return p, sc, memoryErr(e, err)
	}
	return p, sc, 0
}
