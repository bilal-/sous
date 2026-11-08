package signal

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func scanFollowups(t *testing.T, body string) []Signal {
	t.Helper()
	return scanFollowupsIn(t, t.TempDir(), body)
}

func scanFollowupsIn(t *testing.T, dir, body string) []Signal {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "FOLLOWUPS.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, warn bytes.Buffer
	if err := ScanFollowups([]string{dir}, &out, &warn, time.Now()); err != nil {
		t.Fatal(err, warn.String())
	}
	sigs, bad := ReadLinesLenient(&out)
	if bad != 0 {
		t.Fatalf("%d bad lines", bad)
	}
	return sigs
}

func TestFollowupsAreRowsOnYou(t *testing.T) {
	sigs := scanFollowups(t, "- [ ] ask Sam about the cert\n- [x] done already\n")
	if len(sigs) != 1 || sigs[0].Kind != Me || sigs[0].Text != "ask Sam about the cert" || sigs[0].Observed.IsZero() {
		t.Fatalf("got %+v", sigs)
	}
}

func TestFollowupsKeepTheirRowWhenTheFileIsReordered(t *testing.T) {
	dir := t.TempDir()
	a := scanFollowupsIn(t, dir, "- [ ] one\n- [ ] two\n")
	b := scanFollowupsIn(t, dir, "- [ ] two\n- [ ] one\n")
	ids := func(s []Signal) map[string]string {
		m := map[string]string{}
		for _, x := range s {
			m[x.Text] = x.ID
		}
		return m
	}
	if ia, ib := ids(a), ids(b); len(ia) != 2 || ia["one"] != ib["one"] || ia["two"] != ib["two"] {
		t.Fatalf("reordering changed the rows: %v then %v", ia, ib)
	}
	dup := scanFollowups(t, "- [ ] same\n- [ ] same\n")
	if len(dup) != 2 || dup[0].ID == dup[1].ID {
		t.Fatalf("the same line twice must be two rows: %+v", dup)
	}
}

func TestFollowupsBeyondTheFirstFewAreOneRow(t *testing.T) {
	var body strings.Builder
	for i := range FollowupsShown + 3 {
		fmt.Fprintf(&body, "- [ ] item %d\n", i)
	}
	sigs := scanFollowups(t, body.String())
	if len(sigs) != FollowupsShown+1 {
		t.Fatalf("got %d rows", len(sigs))
	}
	if last := sigs[len(sigs)-1]; last.Text != "3 more follow-ups in FOLLOWUPS.md" || last.State != "3" {
		t.Fatalf("got %+v", last)
	}
}

func TestFollowupsWithoutAFileSayNothing(t *testing.T) {
	var out, warn bytes.Buffer
	if err := ScanFollowups([]string{t.TempDir()}, &out, &warn, time.Now()); err != nil || out.Len() != 0 {
		t.Fatalf("got %q, %v", out.String(), err)
	}
}
