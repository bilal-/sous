package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/effort"
	"github.com/bilal-/sous/internal/store"
)

func TestIntegrationWatchOversizedMetadataIsOneBoundedError(t *testing.T) {
	f := fixture(t)
	f.writeConfig("roots = []\n")
	s := &store.Store{Home: f.SousHome}
	for i := range 100 {
		if _, _, err := effort.Create(s, fmt.Sprintf("%03d %s", i, strings.Repeat("x", 180)), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	var out, errs bytes.Buffer
	e := newEnv(f.Home, strings.NewReader(""), &out, &errs)
	defer e.close()
	e.JSON = true
	e.Deadline = time.Now().Add(time.Second)
	code := dispatch(e, "integration", []string{"watch", "--protocol", "1", "--max-bytes", "16384"})
	if code != exitFailed || strings.Count(out.String(), "\n") != 1 || out.Len() > 16384 {
		t.Fatalf("watch must end with one bounded error: code %d, %d lines, %d bytes", code, strings.Count(out.String(), "\n"), out.Len())
	}
	var problem errorJSON
	if err := json.Unmarshal(out.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}
	if problem.Exit != exitFailed || !strings.Contains(problem.Error, "--max-bytes") || !strings.Contains(errs.String(), problem.Error) {
		t.Fatalf("missing limit diagnostic: %+v, stderr %q", problem, errs.String())
	}
}
