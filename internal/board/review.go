package board

import (
	"fmt"
	"io"
	"time"

	"github.com/bilal-/sous/internal/session"
	"github.com/bilal-/sous/internal/thread"
)

// ReviewItem carries the same item as the board plus why it needs a check
// and the last session as context. That session is not proof of completion.
type ReviewItem struct {
	Item
	Reason  string       `json:"reason"`
	Session *SessionItem `json:"session"`
}

type ReviewJSON struct {
	Items     []ReviewItem `json:"items"`
	Attention []string     `json:"attention"`
	AsOf      time.Time    `json:"as_of"`
}

func Review(d *Data, sessions map[string]session.Session) ReviewJSON {
	out := ReviewJSON{Items: []ReviewItem{}, Attention: Classify(d).waiting().Attention, AsOf: d.RenderedAt}
	for _, v := range thread.Reviews(d.Threads, d.RenderedAt) {
		item := ReviewItem{Item: ThreadRow(v.View, d.RenderedAt).Item(), Reason: v.Reason}
		if sess, ok := sessions[v.Project]; ok {
			item.Session = sessionItem(sess)
		}
		out.Items = append(out.Items, item)
	}
	return out
}

func RenderReview(w io.Writer, r ReviewJSON) {
	mark := ""
	if len(r.Attention) > 0 {
		mark = "? "
	}
	fmt.Fprintf(w, "sous review · %s%d notes to check\n", mark, len(r.Items))
	cut := false
	for _, item := range r.Items {
		row := Row{ID: item.ID, Project: item.Project, Shown: item.Text, Age: item.Reason}
		fmt.Fprintln(w, row.Line())
		cut = cut || row.cut()
	}
	if cut {
		fmt.Fprintln(w, cutNote)
	}
	for _, why := range r.Attention {
		fmt.Fprintln(w, " ("+why+")")
	}
	fmt.Fprintln(w, "\n sous show <n> inspect · sous done <n> close · sous review --keep <n> keep for another week")
}

func reviewHint(w io.Writer, views []thread.View, now time.Time, command string) {
	if n := len(thread.Reviews(views, now)); n > 0 {
		noun := "notes"
		if n == 1 {
			noun = "note"
		}
		fmt.Fprintf(w, "\n %d %s to review · %s\n", n, noun, command)
	}
}
