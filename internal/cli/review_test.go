package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/session"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/thread"
)

func reviewNote(t *testing.T, f *fx, p, text string, since time.Time) int {
	t.Helper()
	id, _, err := thread.Note(&store.Store{Home: f.SousHome}, project.Describe(p), thread.Me, text, thread.SourceHuman, since)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestReviewKeepsMissingAndUnavailableItems(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/api", true)
	f.brokenGH(p)
	missing := reviewNote(t, f, p, "check the missing follow-up", time.Now())
	unavailable := reviewNote(t, f, p, "check the upstream fix", time.Now())
	f.fileAs(missing, "md:FOLLOWUPS.md:000000000001")
	f.fileAs(unavailable, "github:acme/api#12")
	out, errs, code := f.run("review", "--json")
	if code != 0 || !strings.Contains(out, "ref missing") || !strings.Contains(out, "status unavailable") {
		t.Fatalf("%d %s %s", code, out, errs)
	}
	for _, id := range []int{missing, unavailable} {
		if th := threadJSON(t, f, id); th["closed"] != nil {
			t.Fatalf("uncertainty closed note %d: %+v", id, th)
		}
	}
}

func TestReviewReconcilesAndKeepsWithoutHiding(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/api", true)
	st := &store.Store{Home: f.SousHome}
	now := time.Now()
	old := reviewNote(t, f, p, "verify the timeout fix", now.Add(-8*24*time.Hour))
	reviewNote(t, f, p, "a fresh task", now)
	snoozed := reviewNote(t, f, p, "waiting until next week", now.Add(-9*24*time.Hour))
	if err := thread.Snooze(st, snoozed, 7, now); err != nil {
		t.Fatal(err)
	}
	filed := reviewNote(t, f, p, "the finished task", now.Add(-10*24*time.Hour))
	if err := os.WriteFile(filepath.Join(p, "FOLLOWUPS.md"), []byte("# Follow-ups\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, errs, code := f.run("file", fmt.Sprint(filed)); code != 0 {
		t.Fatal(code, errs)
	}
	md, _ := os.ReadFile(filepath.Join(p, "FOLLOWUPS.md"))
	if err := os.WriteFile(filepath.Join(p, "FOLLOWUPS.md"), []byte(strings.ReplaceAll(string(md), "[ ]", "[x]")), 0o644); err != nil {
		t.Fatal(err)
	}
	msg := "The timeout test passes; the slow upstream case still needs checking."
	if err := session.Record(st, p, session.Session{Agent: "codex", Ended: now, LastMessage: &msg}); err != nil {
		t.Fatal(err)
	}
	out, errs, code := f.run("review", "--json")
	var got struct {
		Items []struct {
			ID      string `json:"id"`
			Session *struct {
				LastMessage *string `json:"last_message"`
			} `json:"session"`
		} `json:"items"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &got) != nil || len(got.Items) != 1 || got.Items[0].ID != fmt.Sprint(old) || got.Items[0].Session == nil || got.Items[0].Session.LastMessage == nil || *got.Items[0].Session.LastMessage != msg {
		t.Fatalf("%d %s %s", code, out, errs)
	}
	if th := threadJSON(t, f, filed); th["closed"] == nil || th["closed_by"] != "upstream" {
		t.Fatalf("confirmed closure not reconciled: %+v", th)
	}
	for range 2 {
		out, errs, code = f.run("review", "--keep", fmt.Sprint(old), "--json")
		if code != 0 || !strings.Contains(out, `"did": "reviewed"`) {
			t.Fatalf("%d %s %s", code, out, errs)
		}
	}
	if th := threadJSON(t, f, old); th["closed"] != nil || th["snoozed_until"] != nil || th["reviewed_at"] == nil {
		t.Fatalf("keeping must leave the task visible: %+v", th)
	}
	out, errs, code = f.run("review", "--json")
	if code != 0 || !strings.Contains(out, `"items": []`) {
		t.Fatalf("%d %s %s", code, out, errs)
	}
	out, errs, code = f.run("--json")
	if code != 0 || !strings.Contains(out, "verify the timeout fix") {
		t.Fatalf("kept task disappeared: %d %s %s", code, out, errs)
	}
}

func TestReviewScopeAndReminder(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/api", true)
	q := f.mkrepo("acme/web", true)
	old := time.Now().Add(-8 * 24 * time.Hour)
	reviewNote(t, f, p, "review the API fix", old)
	reviewNote(t, f, q, "review the web fix", old)
	out, errs, code := f.run("review", "-p", "acme/api")
	if code != 0 || !strings.Contains(out, "review the API fix") || strings.Contains(out, "review the web fix") {
		t.Fatalf("%d %s %s", code, out, errs)
	}
	out, errs, code = f.run()
	if code != 0 || !strings.Contains(out, "2 notes to review") || !strings.Contains(out, "sous review") {
		t.Fatalf("%d %s %s", code, out, errs)
	}
	out, errs, code = f.runIn(p, "here", "--brief")
	if code != 0 || !strings.Contains(out, "1 note to review") || !strings.Contains(out, "sous review -p acme/api") {
		t.Fatalf("%d %s %s", code, out, errs)
	}
}

func TestHerePointsToProjectGuidesWithoutWriting(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/api", true)
	for _, name := range []string{"STATUS.md", "followups.md", "AGENTS.md"} {
		if err := os.WriteFile(filepath.Join(p, name), []byte("# Project context\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, errs, code := f.runIn(p, "here", "--brief")
	if code != 0 || !strings.Contains(out, "project docs") || !strings.Contains(out, "STATUS.md") || !strings.Contains(out, "followups.md") || !strings.Contains(out, "AGENTS.md") {
		t.Fatalf("%d %s %s", code, out, errs)
	}
	out, errs, code = f.runIn(p, "here", "--json")
	if code != 0 || !strings.Contains(out, `"documents": [`) || !strings.Contains(out, `"purpose": "status"`) {
		t.Fatalf("%d %s %s", code, out, errs)
	}
	q := f.mkrepo("acme/web", true)
	if _, errs, code = f.runIn(q, "here"); code != 0 {
		t.Fatal(code, errs)
	}
	for _, name := range []string{"STATUS.md", "FOLLOWUPS.md", "AGENTS.md"} {
		if _, err := os.Stat(filepath.Join(q, name)); !os.IsNotExist(err) {
			t.Fatalf("here created %s: %v", name, err)
		}
	}
}

func TestCachedViewsDropCompletedNotesImmediately(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/api", true)
	id := reviewNote(t, f, p, "the task completed locally", time.Now().Add(-8*24*time.Hour))
	if _, errs, code := f.run(); code != 0 {
		t.Fatal(code, errs)
	}
	if _, errs, code := f.run("done", fmt.Sprint(id)); code != 0 {
		t.Fatal(code, errs)
	}
	for _, mode := range []string{"--cached", "--menubar", "--ambient"} {
		out, errs, code := f.run(mode)
		if code != 0 || strings.Contains(out, "the task completed locally") || strings.Contains(out, "notes to review") || strings.Contains(out, "note to review") {
			t.Fatalf("%s retained a completed task: %d %s %s", mode, code, out, errs)
		}
	}
}
