package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// CheckOptions shape a conformance check. Finish, when set, is called with
// the run's ref to make it end, and the check then waits for done or
// needs_you; without it the check waits up to Wait for the run to leave
// running, and not at all when Wait is zero.
type CheckOptions struct {
	Finish func(ref string)
	Wait   time.Duration
}

// Check drives r through the contract the way sous does and returns what it
// got wrong, nothing when it conforms. It really starts a run in project,
// with a brief asking for no changes, then stops it; it leaves the run's
// files for the runner's clean, which may rightly refuse to drop work.
func Check(ctx context.Context, r Runner, project string, o CheckOptions) []string {
	var bad []string
	fail := func(format string, a ...any) { bad = append(bad, fmt.Sprintf(format, a...)) }
	caps := Describe(ctx, r)
	uid := make([]byte, 6)
	rand.Read(uid)
	req := Request{ID: 1, UID: hex.EncodeToString(uid), Project: project, Brief: "sous runner check: make no changes and finish"}
	ref, err := Start(ctx, r, req)
	if err != nil {
		return append(bad, fmt.Sprintf("start: %v", err))
	}
	if again, err := Start(ctx, r, req); err != nil || again != ref {
		fail("start again with the same uid gave %q, %v; want %q", again, err, ref)
	}
	if !strings.HasPrefix(ref, r.Name+":") {
		fail("ref %q must start with %q", ref, r.Name+":")
	}
	st, err := GetStatus(ctx, r, project, ref)
	if err != nil {
		return append(bad, fmt.Sprintf("status: %v", err))
	}
	if caps.Speaks("phase") && st.Phase == "" {
		fail("describe names phase, but status reported none")
	}
	wait := o.Wait
	if o.Finish != nil {
		o.Finish(ref)
		wait = time.Minute
	}
	for deadline := time.Now().Add(wait); st.State == Running && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if st, err = GetStatus(ctx, r, project, ref); err != nil {
			return append(bad, fmt.Sprintf("status: %v", err))
		}
	}
	if o.Finish != nil && st.State != Done && st.State != NeedsYou {
		fail("after finish: %s %q; want done or needs_you", st.State, st.Text)
	}
	for range 2 {
		if err := Stop(ctx, r, project, ref); err != nil {
			return append(bad, fmt.Sprintf("stop (stopping twice is fine): %v", err))
		}
	}
	if caps.Speaks("phase") {
		st, err := GetStatus(ctx, r, project, ref)
		switch {
		case err != nil:
			fail("status after stop: %v", err)
		case st.Phase.Working():
			fail("status after stop: phase %q; a stopped run never works again", st.Phase)
		}
	}
	return bad
}
