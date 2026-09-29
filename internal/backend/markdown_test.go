package backend

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestMarkdownLifecycle(t *testing.T) {
	p := t.TempDir()
	if MarkdownDetect(p) {
		t.Fatal("no FOLLOWUPS.md → no detect")
	}
	os.WriteFile(filepath.Join(p, MarkdownFile), []byte("# Follow-ups\n\n- [ ] existing item"), 0o644) // no trailing newline
	if !MarkdownDetect(p) {
		t.Fatal("detect")
	}
	ref, err := MarkdownFileNote(p, Request{ID: 7, Project: p, Text: "fix notification context", Kind: "idea"})
	if err != nil || !strings.HasPrefix(ref, "md:FOLLOWUPS.md:7:") || len(ref) != len("md:FOLLOWUPS.md:7:")+8 {
		t.Fatal(ref, err)
	}
	b, _ := os.ReadFile(filepath.Join(p, MarkdownFile))
	id := strings.TrimPrefix(ref, "md:FOLLOWUPS.md:")
	want := "# Follow-ups\n\n- [ ] existing item\n- [ ] fix notification context <!-- sous:" + id + " -->\n"
	if string(b) != want {
		t.Fatalf("append shape (trailing newline repaired):\n%q\nwant\n%q", b, want)
	}
	// Crash recovery: same id and same text → the existing marker is reused.
	ref2, err := MarkdownFileNote(p, Request{ID: 7, Project: p, Text: "fix notification context", Kind: "me"})
	if err != nil || ref2 != ref {
		t.Fatalf("idempotent: %s vs %s (%v)", ref2, ref, err)
	}
	b, _ = os.ReadFile(filepath.Join(p, MarkdownFile))
	if strings.Count(string(b), "<!-- sous:") != 1 {
		t.Fatalf("duplicate marker:\n%s", b)
	}
	if st := MarkdownStatus(p, ref); st != "open" {
		t.Fatal(st)
	}
	os.WriteFile(filepath.Join(p, MarkdownFile), []byte("- [ ] fix notification context (edited) <!-- sous:"+id+" -->\n- [ ] existing item\n"), 0o644)
	if st := MarkdownStatus(p, ref); st != "open" {
		t.Fatal("edited line must still be open")
	}
	if err := MarkdownClose(p, ref); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(p, MarkdownFile))
	if string(b) != "- [x] fix notification context (edited) <!-- sous:"+id+" -->\n- [ ] existing item\n" {
		t.Fatalf("close must flip exactly the marker line's box:\n%s", b)
	}
	if st := MarkdownStatus(p, ref); st != "closed" {
		t.Fatal(st)
	}
	if err := MarkdownClose(p, ref); err != nil {
		t.Fatal("closing a closed item is fine")
	}
	os.WriteFile(filepath.Join(p, MarkdownFile), []byte("- [X] thing <!-- sous:"+id+" -->\n"), 0o644)
	if st := MarkdownStatus(p, ref); st != "closed" {
		t.Fatal("[X] is closed")
	}
	os.WriteFile(filepath.Join(p, MarkdownFile), []byte("some prose <!-- sous:"+id+" -->\n"), 0o644)
	if st := MarkdownStatus(p, ref); st != "unknown" {
		t.Fatal(st)
	}
	if err := MarkdownClose(p, ref); err == nil {
		t.Fatal("cannot close a non-checkbox line")
	}
	os.WriteFile(filepath.Join(p, MarkdownFile), []byte("- [ ] a <!-- sous:"+id+" -->\n- [ ] b <!-- sous:"+id+" -->\n"), 0o644)
	if st := MarkdownStatus(p, ref); st != "unknown" {
		t.Fatal("duplicate markers are ambiguous")
	}
	if err := MarkdownClose(p, ref); err == nil {
		t.Fatal("refuse to close an ambiguous marker")
	}
	os.WriteFile(filepath.Join(p, MarkdownFile), []byte("- [ ] something else\n"), 0o644)
	if st := MarkdownStatus(p, ref); st != "unknown" {
		t.Fatal(st)
	}
	if err := MarkdownClose(p, ref); err == nil {
		t.Fatal("close of a missing marker must error")
	}
	if st := MarkdownStatus(p, "md:OTHER.md:1:abc"); st != "unknown" {
		t.Fatal("foreign ref")
	}
}

func TestMarkdownCreatesFileAndKeepsSymlinkAndMode(t *testing.T) {
	p := t.TempDir()
	ref, err := MarkdownFileNote(p, Request{ID: 1, Project: p, Text: "first", Kind: "me"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(p, MarkdownFile))
	if !strings.HasPrefix(string(b), "# Follow-ups\n\n- [ ] first") || MarkdownStatus(p, ref) != "open" {
		t.Fatalf("%s", b)
	}
	q := t.TempDir()
	real := filepath.Join(t.TempDir(), "notes.md")
	os.WriteFile(real, []byte("# N\n"), 0o664)
	os.Chmod(real, 0o600) // explicit: WriteFile's mode is subject to umask
	os.Symlink(real, filepath.Join(q, MarkdownFile))
	ref, _ = MarkdownFileNote(q, Request{ID: 2, Project: q, Text: "via link", Kind: "idea"})
	if err := MarkdownClose(q, ref); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(filepath.Join(q, MarkdownFile)); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("close replaced the symlink with a file")
	}
	rb, _ := os.ReadFile(real)
	if !strings.Contains(string(rb), "- [x] via link") {
		t.Fatalf("real file not updated:\n%s", rb)
	}
	if fi, _ := os.Stat(real); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode changed to %o", fi.Mode().Perm())
	}
	if entries, _ := os.ReadDir(q); len(entries) != 1 {
		t.Fatalf("temp files left: %v", entries)
	}
}

func TestMarkdownConcurrentAppendsAllSurvive(t *testing.T) {
	p := t.TempDir()
	var wg sync.WaitGroup
	for i := 1; i <= 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := MarkdownFileNote(p, Request{ID: i, Project: p, Text: "n", Kind: "idea"}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	b, _ := os.ReadFile(filepath.Join(p, MarkdownFile))
	if n := strings.Count(string(b), "<!-- sous:"); n != 20 {
		t.Fatalf("lost appends: %d of 20\n%s", n, b)
	}
}

// Review C2: thread ids are per-installation; a teammate's marker with the
// same id must not be mistaken for ours. Dedupe on id+text; status/close
// match the full id:tag.
func TestMarkdownDoesNotCollideOnForeignId(t *testing.T) {
	p := t.TempDir()
	os.WriteFile(filepath.Join(p, MarkdownFile), []byte("# F\n- [ ] teammate's item <!-- sous:3:bbbbbbbb -->\n"), 0o644)
	ref, err := MarkdownFileNote(p, Request{ID: 3, Project: p, Text: "my note", Kind: "me"})
	if err != nil || ref == "md:FOLLOWUPS.md:3:bbbbbbbb" {
		t.Fatalf("must not adopt a foreign marker: %s %v", ref, err)
	}
	b, _ := os.ReadFile(filepath.Join(p, MarkdownFile))
	if !strings.Contains(string(b), "- [ ] my note <!-- sous:3:") || strings.Count(string(b), "<!-- sous:3:") != 2 {
		t.Fatalf("my note must be written:\n%s", b)
	}
	if st := MarkdownStatus(p, "md:FOLLOWUPS.md:3:bbbbbbbb"); st != "open" {
		t.Fatal("teammate's marker still resolves by its own tag")
	}
	if st := MarkdownStatus(p, ref); st != "open" {
		t.Fatal(st)
	}
	if err := MarkdownClose(p, ref); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(p, MarkdownFile))
	if !strings.Contains(string(b), "- [ ] teammate's item") || !strings.Contains(string(b), "- [x] my note") {
		t.Fatalf("close must touch only my line:\n%s", b)
	}
	// Crash recovery: same id AND same text → the existing marker is reused.
	ref2, _ := MarkdownFileNote(p, Request{ID: 3, Project: p, Text: "my note", Kind: "me"})
	if ref2 != ref {
		t.Fatalf("id+text dedupe: %s vs %s", ref2, ref)
	}
}

// Closing a box changes one byte in place: nothing is truncated (a crash
// can't leave an empty file) and the rest of the file, CRLF endings and
// all, is untouched.
func TestMarkdownCloseChangesOnlyTheBox(t *testing.T) {
	p := t.TempDir()
	orig := "# Follow-ups\r\n\r\n- [ ] keep me\r\n- [ ] close me <!-- sous:7:abcd1234 -->\r\nno newline at end"
	os.WriteFile(filepath.Join(p, MarkdownFile), []byte(orig), 0o644)
	if err := MarkdownClose(p, "md:FOLLOWUPS.md:7:abcd1234"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(p, MarkdownFile))
	want := strings.Replace(orig, "- [ ] close me", "- [x] close me", 1)
	if string(b) != want {
		t.Fatalf("got %q\nwant %q", b, want)
	}
}

// New filings mark items with the note's stable uid; markers written by
// earlier versions (id:tag) keep working for status and close.
func TestMarkdownUsesStableUIDAndKeepsOldMarkers(t *testing.T) {
	p := t.TempDir()
	os.WriteFile(filepath.Join(p, MarkdownFile), []byte("# Follow-ups\n- [ ] old one <!-- sous:7:abcd1234 -->\n"), 0o644)
	ref, err := MarkdownFileNote(p, Request{ID: 7, UID: "0123456789ab", Project: p, Text: "new one", Kind: "me"})
	if err != nil || ref != "md:FOLLOWUPS.md:0123456789ab" {
		t.Fatal(ref, err)
	}
	b, _ := os.ReadFile(filepath.Join(p, MarkdownFile))
	if !strings.Contains(string(b), "- [ ] new one <!-- sous:0123456789ab -->") {
		t.Fatalf("%s", b)
	}
	// Same uid again (crash recovery): same ref, no second line, even if
	// the text was edited meanwhile.
	if again, _ := MarkdownFileNote(p, Request{ID: 7, UID: "0123456789ab", Project: p, Text: "new one, edited", Kind: "me"}); again != ref {
		t.Fatal(again)
	}
	if st := MarkdownStatus(p, "md:FOLLOWUPS.md:7:abcd1234"); st != "open" {
		t.Fatal("old marker still readable:", st)
	}
	if err := MarkdownClose(p, ref); err != nil || MarkdownStatus(p, ref) != "closed" || MarkdownStatus(p, "md:FOLLOWUPS.md:7:abcd1234") != "open" {
		t.Fatal("closing by uid touches only that item", err)
	}
}
