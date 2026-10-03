package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReportCLI(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("a/r", true)
	f.runIn(p, "note", "-k", "me", "fresh note")
	out, _, code := f.run("report")
	if code != 0 || !strings.Contains(out, "fresh note") || !strings.Contains(out, "new on you") {
		t.Fatalf("%d\n%s", code, out)
	}
	// Default window is "since the last report": a second run has nothing new.
	out, _, _ = f.run("report")
	if strings.Contains(out, "fresh note") {
		t.Fatalf("second report must start where the first ended:\n%s", out)
	}
	if out, _, _ = f.run("report", "--week"); !strings.Contains(out, "fresh note") {
		t.Fatalf("--week looks back 7 days regardless:\n%s", out)
	}
	if out, _, code = f.run("--json", "report", "--week"); code != 0 || !strings.Contains(out, `"new_on_you"`) || !strings.Contains(out, `"text": "fresh note"`) {
		t.Fatalf("--json: %d %s", code, out)
	}
	// --open writes the page and opens it (fake `open` records its argument).
	opened := filepath.Join(f.Home, "opened")
	f.bin("open", "echo \"$1\" > "+opened+"")
	if _, errs, code := f.run("report", "--week", "--open"); code != 0 {
		t.Fatalf("%d %s", code, errs)
	}
	page := filepath.Join(f.SousHome, "report.html")
	b, err := os.ReadFile(page)
	if err != nil || !strings.Contains(string(b), "fresh note") {
		t.Fatalf("report.html: %v", err)
	}
	if got, _ := os.ReadFile(opened); strings.TrimSpace(string(got)) != page {
		t.Fatalf("open called with %q", got)
	}
	if _, _, code := f.run("report", "--bogus"); code != 2 {
		t.Fatal("unknown flag exits 2")
	}
}

// A report that could not be shown has not been taken; its window
// stays for the next one.
func TestFailedReportKeepsItsWindow(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/chime", true)
	f.runIn(p, "note", "-k", "me", "fresh note")
	os.MkdirAll(filepath.Join(f.SousHome, "report.html"), 0o755) // the page cannot be written
	if _, _, code := f.run("report", "--open"); code != 1 {
		t.Fatalf("code %d", code)
	}
	if out, _, _ := f.run("report"); !strings.Contains(out, "fresh note") {
		t.Fatalf("the failed report consumed the window:\n%s", out)
	}
}
