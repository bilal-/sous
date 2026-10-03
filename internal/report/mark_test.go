package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/store"
)

func TestWindowAndTake(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	now := time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)
	if since, _ := Window(s, now, false); !since.Equal(now.Add(-24 * time.Hour)) {
		t.Fatalf("first report looks back a day: %v", since)
	}
	Take(s, now, false)
	if since, _ := Window(s, now.Add(time.Hour), false); !since.Equal(now) {
		t.Fatalf("then since the last one: %v", since)
	}
	if since, _ := Window(s, now.Add(time.Hour), true); !since.Equal(now.Add(time.Hour - 7*24*time.Hour)) {
		t.Fatalf("--week: %v", since)
	}
	Take(s, now.Add(2*time.Hour), true)
	if since, _ := Window(s, now.Add(3*time.Hour), false); !since.Equal(now) {
		t.Fatal("--week never moves the mark")
	}
}

func TestWritePage(t *testing.T) {
	home := t.TempDir()
	p, err := WritePage(home, Report{})
	if err != nil || p != filepath.Join(home, "report.html") {
		t.Fatal(p, err)
	}
	if b, _ := os.ReadFile(p); !strings.Contains(string(b), "<!doctype html>") {
		t.Fatalf("%s", b)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }

// A report that could not be written says so, so it is not taken.
func TestRenderReportsWriteErrors(t *testing.T) {
	if err := Render(failingWriter{}, Report{}); err == nil {
		t.Fatal("a failed write must be an error")
	}
}

// The page holds note text; it is private, even if it existed.
func TestPageIsPrivate(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, "report.html"), nil, 0o644)
	p, _ := WritePage(home, Report{})
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Fatalf("%v", fi.Mode().Perm())
	}
}
