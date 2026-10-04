package thread

import (
	"sort"
	"time"

	"github.com/bilal-/sous/internal/store"
)

const ReviewInterval = 7 * 24 * time.Hour

// Review is a note worth checking, with a reason rather than a guess that
// its work is finished. A review never closes, snoozes or files a note.
type Review struct {
	View
	Reason string
}

// Reviews selects old notes and unresolved tracker/run outcomes. Keeping a
// note acknowledges it for a week; the task remains on the ordinary board.
func Reviews(views []View, now time.Time) []Review {
	out := []Review{}
	for _, v := range views {
		if v.Closed != nil || v.Snoozed || v.Run != nil && v.Run.State.Working() {
			continue
		}
		if v.ReviewedAt != nil && now.Sub(*v.ReviewedAt) < ReviewInterval {
			continue
		}
		reason := ""
		switch {
		case v.Upstream == "unknown":
			reason = "ref missing; check what happened"
		case v.Upstream == "error" || v.RunErr != "":
			reason = "status unavailable; completion is unknown"
		case v.Run != nil && v.Run.State == RunDone:
			reason = "run finished; verify the work"
		case v.Run != nil && v.Run.State == RunFailed:
			reason = "run failed; decide what remains"
		case now.Sub(reviewedSince(v.Thread)) >= ReviewInterval:
			reason = "not reviewed for a week"
		}
		if reason != "" {
			out = append(out, Review{View: v, Reason: reason})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := reviewedSince(out[i].Thread), reviewedSince(out[j].Thread)
		if a.Equal(b) {
			return out[i].ID < out[j].ID
		}
		return a.Before(b)
	})
	return out
}

func reviewedSince(t Thread) time.Time {
	if t.ReviewedAt != nil {
		return *t.ReviewedAt
	}
	return t.Since
}

// Keep records that the person or their agent checked this note and it
// still matters. It does not change its age, kind, visibility or tracker.
func Keep(s *store.Store, id int, now time.Time) error {
	at := now.UTC()
	return touch(s, id, func(t *Thread) { t.ReviewedAt = &at })
}
