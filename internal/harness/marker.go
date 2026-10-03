package harness

import "strings"

// How an agent on a run says how it ended: the last line of its last
// message is "SOUS: done <summary>" or "SOUS: needs you <question>". The
// preamble a run starts with asks for it (EndWith), the watcher reads it
// (ReadMarker), and a harness may write one for an agent that could not
// (NeedsYou).
const (
	marker       = "SOUS:"
	markDone     = "done"
	markNeedsYou = "needs you"
)

// EndWith is what a run's preamble asks the agent to end with.
var EndWith = "End your last message with one line:\n" +
	marker + " " + markDone + " <one line summary of what you did>\nor\n" +
	marker + " " + markNeedsYou + " <one question for the person>\n"

// hasMarker: msg already says how the run ended.
func hasMarker(msg string) bool { return strings.Contains(msg, marker) }

// needsYou is the line saying the run needs the person, asking question.
func needsYou(question string) string { return marker + " " + markNeedsYou + " " + question }

// ReadMarker is how msg says the run ended, from its last marker line:
// done or needs you (needsYou), and the summary or question; ok is false
// when there is none.
func ReadMarker(msg string) (needsYou bool, said string, ok bool) {
	lines := strings.Split(msg, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		rest, found := strings.CutPrefix(strings.TrimSpace(lines[i]), marker)
		if !found {
			continue
		}
		rest = strings.TrimSpace(rest)
		if t, found := strings.CutPrefix(rest, markNeedsYou); found {
			return true, strings.TrimSpace(t), true
		}
		if t, found := strings.CutPrefix(rest, markDone); found {
			return false, strings.TrimSpace(t), true
		}
	}
	return false, "", false
}
