package thread

import "testing"

// Every known phase reads as one of the v0 states, so a host that knows
// only states still sees the right thing; an unknown phase has none.
func TestPhaseState(t *testing.T) {
	for phase, want := range map[RunPhase]RunState{
		PhaseQueued: RunRunning, PhasePlanning: RunRunning, PhaseWorking: RunRunning,
		PhaseReviewing: RunRunning, PhaseBlocked: RunRunning, PhaseNeedsYou: RunNeedsYou,
		PhaseReady: RunDone, PhaseApproved: RunDone, PhaseFailed: RunFailed,
		PhaseStopped: RunFailed, PhaseDiscarded: RunFailed,
	} {
		if got, ok := phase.State(); !ok || got != want {
			t.Errorf("%s: %q %v; want %q", phase, got, ok, want)
		}
	}
	for _, p := range []RunPhase{"published", "dreaming", ""} {
		if _, ok := p.State(); ok {
			t.Errorf("%q is not a runner phase", p)
		}
	}
}
