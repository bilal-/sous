package signal

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/bilal-/sous/internal/project"
)

// FollowupsShown is how many of one project's follow-ups the board lists;
// the rest are one row that says how many more there are.
const FollowupsShown = 5

// ScanFollowups is the built-in followups plugin: each open box in a
// project's FOLLOWUPS.md that sous did not file itself is a row on you.
// The file stays the source: tick the box and the row goes. It only reads
// files, so it is offline.
func ScanFollowups(paths []string, w, warn io.Writer, now time.Time) error {
	unreadable := 0
	for _, p := range paths {
		items, changed, err := project.Followups(p)
		if err != nil {
			fmt.Fprintf(warn, "sous-signal-followups: cannot read %s in %s: %v\n", project.FollowupsFile, p, err)
			unreadable++
			continue
		}
		seen := map[string]int{}
		for i, item := range items {
			if i == FollowupsShown {
				more := len(items) - FollowupsShown
				WriteLine(w, Signal{ID: ID(p, "followups:more"), Project: p, Kind: Me, Text: fmt.Sprintf("%d more follow-ups in %s", more, project.FollowupsFile), Observed: changed, State: strconv.Itoa(more)})
				break
			}
			// Keyed by the text, so reordering the file keeps each row
			// (and its snooze); the same line twice is two rows.
			sum := sha256.Sum256([]byte(item))
			key := "followups:" + hex.EncodeToString(sum[:])[:12]
			if n := seen[key]; n > 0 {
				key += ":" + strconv.Itoa(n)
			}
			seen[key]++
			WriteLine(w, Signal{ID: ID(p, key), Project: p, Kind: Me, Text: item, Observed: changed})
		}
	}
	if unreadable > 0 {
		return fmt.Errorf("%d %s unreadable", unreadable, project.FollowupsFile)
	}
	return nil
}
