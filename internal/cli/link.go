package cli

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/bilal-/sous/internal/thread"
)

// noteLinks reads --ref (a ticket that already exists) and --after (notes
// this one waits on) into links. A bad value is a usage error, said here.
func noteLinks(e *Env, a argv) (thread.Links, int) {
	var l thread.Links
	if a.has("ref") {
		if _, code := e.config(); code != 0 {
			return l, code
		}
		ref, err := e.filer().TicketRef(a.value("ref"))
		if err != nil {
			return l, threadErr(e, err)
		}
		l.Ref = ref
	}
	for _, s := range a.values("after") {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			return l, fail(e, exitUsage, "--after names a note by its number, got %q", s)
		}
		l.After = append(l.After, n)
	}
	return l, 0
}

// linkErr: a note already linked elsewhere is a conflict to resolve first.
func linkErr(e *Env, err error) int {
	if errors.Is(err, thread.ErrLinked) {
		return fail(e, exitUsage, "%v", err)
	}
	return threadErr(e, err)
}

// linkedSaid is what link and unlink say they did.
func linkedSaid(verb string, id int, l thread.Links) string {
	var what []string
	if l.Ref != "" && l.Ref != "*" {
		what = append(what, l.Ref)
	} else if l.Ref == "*" {
		what = append(what, "its ticket")
	}
	for _, n := range l.After {
		what = append(what, "waits on "+strconv.Itoa(n))
	}
	return fmt.Sprintf("%s %d · %s", verb, id, strings.Join(what, ", "))
}

// cmdLink points a note at a ticket that already exists (--ref) or at
// notes it waits on (--after). It writes nothing to any tracker.
func cmdLink(e *Env, a argv) int {
	id, code := threadID(e, a.pos[0])
	if code != 0 {
		return code
	}
	if !a.has("ref") && !a.has("after") {
		return fail(e, exitUsage, "say what to link: --ref <ticket> or --after <n>")
	}
	l, code := noteLinks(e, a)
	if code != 0 {
		return code
	}
	changed, err := thread.Link(e.store(), id, l)
	if err != nil {
		return linkErr(e, err)
	}
	return e.changed(linkedJSON(e, id, changed, didLinked, didAlreadyLinked), linkedSaid(map[bool]string{true: "linked", false: "already linked"}[changed], id, l))
}

// cmdUnlink forgets a note's ticket (--ref) or a note it waits on
// (--after). The ticket itself is left as it is.
func cmdUnlink(e *Env, a argv) int {
	id, code := threadID(e, a.pos[0])
	if code != 0 {
		return code
	}
	if !a.has("ref") && !a.has("after") {
		return fail(e, exitUsage, "say what to unlink: --ref or --after <n>")
	}
	var l thread.Links
	if a.has("ref") {
		l.Ref = "*"
	}
	for _, s := range a.values("after") {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			return fail(e, exitUsage, "--after names a note by its number, got %q", s)
		}
		l.After = append(l.After, n)
	}
	changed, err := thread.Unlink(e.store(), id, l)
	if err != nil {
		return threadErr(e, err)
	}
	return e.changed(linkedJSON(e, id, changed, didUnlinked, didAlreadyUnlinked), linkedSaid(map[bool]string{true: "unlinked", false: "already unlinked"}[changed], id, l))
}

func linkedJSON(e *Env, id int, changed bool, did, already string) changedJSON {
	c := changedJSON{ID: fmt.Sprint(id), Did: did, Next: noteNext(e, id)}
	if !changed {
		c.Did = already
	}
	if th, err := thread.Get(e.store(), id); err == nil {
		c.Ref, c.After = th.Ref, afterIDs(th.After)
	}
	return c
}

func afterIDs(after []int) []string {
	out := []string{}
	for _, n := range after {
		out = append(out, strconv.Itoa(n))
	}
	return out
}
