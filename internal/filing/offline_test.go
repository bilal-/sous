package filing

import (
	"testing"
	"time"

	"github.com/bilal-/sous/internal/thread"
)

func TestSettleKeepsKnownStatusWhenOfflineAnswerWasSkipped(t *testing.T) {
	ref := "github:acme/api#12"
	views := []thread.View{{Thread: thread.Thread{ID: 1, Ref: &ref}, Upstream: "error", UpstreamErr: "tracker unavailable"}}
	got := (&Filer{}).Settle(views, []Upstream{{ID: 1}}, time.Now())
	if len(got) != 1 || got[0].Upstream != "error" || got[0].UpstreamErr != "tracker unavailable" {
		t.Fatalf("a skipped remote check erased known uncertainty: %+v", got)
	}
}
