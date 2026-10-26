package runner

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/bilal-/sous/internal/plugin"
)

// Capabilities is what a runner says it speaks, from its optional describe
// call: the calls it answers and the status fields it reports. sous sends
// nothing a runner has not named, so a runner that predates describe is
// called exactly as before.
type Capabilities struct {
	V         int      `json:"v"`
	Calls     []string `json:"calls"`
	Fields    []string `json:"fields,omitempty"`
	Described bool     `json:"-"` // it answered describe; false means the calls every runner has
}

// baseCalls are the calls of a runner that does not describe itself: the
// required three and the two optional ones, which answer exit 2 when absent.
var baseCalls = []string{"start", "status", "stop", "reply", "clean"}

// Has: the runner answers call.
func (c Capabilities) Has(call string) bool { return slices.Contains(c.Calls, call) }

// Speaks: the runner reports status field (phase, progress, question, result).
func (c Capabilities) Speaks(field string) bool { return slices.Contains(c.Fields, field) }

// Describe asks r what it speaks. Anything but exit 0 with one readable v0
// line naming at least start, status and stop is a runner that predates
// describe; that includes a shell case with no describe arm, which exits 0
// and prints nothing.
func Describe(ctx context.Context, r Runner) Capabilities {
	ctx, cancel := context.WithTimeout(ctx, plugin.StatusTimeout)
	defer cancel()
	base := Capabilities{Calls: baseCalls}
	a, err := call(ctx, r, nil, "describe")
	if err != nil || a.Code != plugin.ExitOK {
		return base
	}
	var c Capabilities
	if json.Unmarshal([]byte(a.Out), &c) != nil || c.V != plugin.Version {
		return base
	}
	for _, need := range baseCalls[:3] {
		if !c.Has(need) {
			return base
		}
	}
	c.Described = true
	return c
}
