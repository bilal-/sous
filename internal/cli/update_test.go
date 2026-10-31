package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bilal-/sous/internal/update"
)

func TestUpdateCommandCheckRetryAndErrors(t *testing.T) {
	f := fixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v0.24.0"}`)
	}))
	defer srv.Close()
	exe := filepath.Join(f.Home, "bin", "sous-release")
	os.WriteFile(exe, []byte("installed release"), 0755)
	u := update.New(exe, "0.24.0")
	u.API, u.HTTP = srv.URL, srv.Client()
	call := func(jsonOutput bool, args ...string) (string, string, int) {
		t.Helper()
		var out, errs bytes.Buffer
		e := newEnv(f.Home, strings.NewReader(""), &out, &errs)
		defer e.close()
		e.Updater, e.JSON = u, jsonOutput
		code := dispatch(e, "update", args)
		return out.String(), errs.String(), code
	}
	for range 2 {
		out, errs, code := call(true)
		var result changedJSON
		if code != 0 || errs != "" || json.Unmarshal([]byte(out), &result) != nil || result.Did != "already_current" || len(result.Next) == 0 {
			t.Fatalf("%d %s %s", code, out, errs)
		}
	}
	out, errs, code := call(true, "--check")
	if code != 0 || errs != "" || !strings.Contains(out, `"update_available": false`) || !strings.Contains(out, `"next"`) || strings.Contains(out, `"did"`) {
		t.Fatalf("%d %s %s", code, out, errs)
	}
	u.Version = "v0.23.0"
	out, _, code = call(false, "--check")
	if code != 0 || !strings.Contains(out, "v0.24.0 available") || !strings.Contains(out, "sous update") {
		t.Fatalf("%d %s", code, out)
	}
	u.Version = "dev"
	out, errs, code = call(true)
	if code != 1 || !strings.Contains(out, `"error"`) || !strings.Contains(errs, "source build") {
		t.Fatalf("%d %s %s", code, out, errs)
	}
	out, _, code = call(false, "--check")
	if code != 0 || !strings.Contains(out, "source build") || strings.Contains(out, "up to date") {
		t.Fatalf("%d %s", code, out)
	}
	out, _, code = call(true, "--check")
	if code != 0 || !strings.Contains(out, `"update_available": null`) {
		t.Fatalf("an unversioned build cannot be compared: %d %s", code, out)
	}
	srv.Close()
	out, errs, code = call(true, "--check")
	if code != 1 || !strings.Contains(out, `"error"`) || errs == "" {
		t.Fatalf("%d %s %s", code, out, errs)
	}
}
