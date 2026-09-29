package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type doc struct {
	Version int      `json:"version"`
	NextID  int      `json:"next_id"`
	Items   []string `json:"items"`
}

type docMig struct{}

func (docMig) Empty() []byte { return []byte(`{"version":1,"next_id":1,"items":[]}`) }
func (docMig) Current() int  { return 1 }
func (docMig) Migrate(from int, raw []byte) ([]byte, error) {
	if from != 0 {
		return nil, errors.New("no such migration")
	}
	var v0 struct {
		Items []string `json:"items"`
	}
	if err := json.Unmarshal(raw, &v0); err != nil {
		return nil, err
	}
	return json.Marshal(doc{Version: 1, NextID: len(v0.Items) + 1, Items: v0.Items})
}

func TestReadMissingGivesEmptyWithoutWriting(t *testing.T) {
	s := &Store{Home: t.TempDir()}
	d, err := Load[doc](s, "threads", docMig{})
	if err != nil || d.Version != 1 || d.NextID != 1 {
		t.Fatalf("empty: %+v %v", d, err)
	}
	if _, err := os.Stat(filepath.Join(s.Home, "threads.json")); !os.IsNotExist(err) {
		t.Fatal("read must not create the file")
	}
}

func TestModifyIsAtomicAndLeavesNoTemp(t *testing.T) {
	s := &Store{Home: t.TempDir()}
	d, err := Modify[doc](s, "threads", docMig{}, func(d *doc) error { d.Items = append(d.Items, "a"); d.NextID++; return nil })
	if err != nil || d.NextID != 2 {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(s.Home)
	for _, e := range entries {
		if e.Name() != "threads.json" && e.Name() != "threads.lock" {
			t.Fatalf("unexpected file left: %s", e.Name())
		}
	}
}

func TestFailedModifyLeavesFileUntouched(t *testing.T) {
	s := &Store{Home: t.TempDir()}
	Modify[doc](s, "threads", docMig{}, func(d *doc) error { d.Items = []string{"x"}; return nil })
	before, _ := os.ReadFile(filepath.Join(s.Home, "threads.json"))
	_, err := Modify[doc](s, "threads", docMig{}, func(d *doc) error { return errors.New("nope") })
	if err == nil {
		t.Fatal("expected error")
	}
	after, _ := os.ReadFile(filepath.Join(s.Home, "threads.json"))
	if string(before) != string(after) {
		t.Fatal("file changed on failed modify")
	}
}

func TestNewerVersionRefusesAndDoesNotWrite(t *testing.T) {
	s := &Store{Home: t.TempDir()}
	p := filepath.Join(s.Home, "threads.json")
	os.WriteFile(p, []byte(`{"version":2,"items":[]}`), 0o644)
	_, err := Load[doc](s, "threads", docMig{})
	if !errors.Is(err, ErrNewer) {
		t.Fatalf("want ErrNewer, got %v", err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != `{"version":2,"items":[]}` {
		t.Fatal("file was modified")
	}
}

func TestV0MigratesAndPersists(t *testing.T) {
	s := &Store{Home: t.TempDir()}
	p := filepath.Join(s.Home, "threads.json")
	os.WriteFile(p, []byte(`{"items":["a","b"]}`), 0o644)
	d, err := Load[doc](s, "threads", docMig{})
	if err != nil || d.Version != 1 || d.NextID != 3 {
		t.Fatalf("migrated: %+v %v", d, err)
	}
	var on doc
	b, _ := os.ReadFile(p)
	json.Unmarshal(b, &on)
	if on.Version != 1 {
		t.Fatal("migration not persisted")
	}
}

func TestConcurrentModifiesAllLand(t *testing.T) {
	s := &Store{Home: t.TempDir()}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := Modify[doc](s, "threads", docMig{}, func(d *doc) error {
				time.Sleep(2 * time.Millisecond)
				d.Items = append(d.Items, "x")
				d.NextID++
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	d, _ := Load[doc](s, "threads", docMig{})
	if len(d.Items) != 20 || d.NextID != 21 {
		t.Fatalf("lost updates: %d items, next_id %d", len(d.Items), d.NextID)
	}
}

func TestWriteRefusesInvalidAndUnwritable(t *testing.T) {
	s := &Store{Home: t.TempDir()}
	_, err := s.Update("threads", docMig{}, func([]byte) ([]byte, error) { return []byte("{not json"), nil })
	if err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("%v", err)
	}
	if _, err := os.Stat(filepath.Join(s.Home, "threads.json")); !os.IsNotExist(err) {
		t.Fatal("nothing must be written")
	}
	ro := &Store{Home: filepath.Join(t.TempDir(), "ro")}
	os.MkdirAll(ro.Home, 0o500)
	defer os.Chmod(ro.Home, 0o700)
	if _, err := Modify[doc](ro, "threads", docMig{}, func(*doc) error { return nil }); err == nil {
		t.Fatal("unwritable home must error")
	}
}

func TestMalformedFileShapes(t *testing.T) {
	for _, body := range []string{`{"version":"1"}`, `[]`, ``, `{"version":1}x`} {
		s := &Store{Home: t.TempDir()}
		os.WriteFile(filepath.Join(s.Home, "threads.json"), []byte(body), 0o644)
		if _, err := Load[doc](s, "threads", docMig{}); err == nil {
			t.Errorf("%q must not load silently", body)
		}
	}
}

// Each document has its own schema version. A store must not refuse every
// other file because one document's version moved.
type v2Mig struct{}

func (v2Mig) Empty() []byte { return []byte(`{"version":2,"items":[]}`) }
func (v2Mig) Current() int  { return 2 }
func (v2Mig) Migrate(from int, raw []byte) ([]byte, error) {
	if from == 1 {
		return []byte(`{"version":2,"next_id":1,"items":[]}`), nil
	}
	return docMig{}.Migrate(from, raw)
}

func TestPerDocumentVersions(t *testing.T) {
	s := &Store{Home: t.TempDir()}
	os.WriteFile(filepath.Join(s.Home, "a.json"), []byte(`{"version":1,"next_id":1,"items":[]}`), 0o644)
	os.WriteFile(filepath.Join(s.Home, "b.json"), []byte(`{"version":2,"items":[]}`), 0o644)
	if _, err := Load[doc](s, "a", docMig{}); err != nil {
		t.Fatalf("v1 document with a v1 migrator: %v", err)
	}
	if _, err := Load[doc](s, "b", v2Mig{}); err != nil {
		t.Fatalf("v2 document with a v2 migrator: %v", err)
	}
	if _, err := Load[doc](s, "a", v2Mig{}); err != nil {
		t.Fatalf("v1 document read by a v2 migrator must migrate: %v", err)
	}
	if _, err := Load[doc](s, "b", docMig{}); !errors.Is(err, ErrNewer) {
		t.Fatalf("v2 document read by a v1 migrator is newer: %v", err)
	}
}

// lockCheckMig records whether the store's lock was held when Migrate ran,
// by trying to take it without waiting from a second file handle (flock
// locks are per open file, so this conflicts exactly as another process
// would).
type lockCheckMig struct {
	home     string
	unlocked *bool
}

func (lockCheckMig) Empty() []byte { return []byte(`{"version":2,"items":[]}`) }
func (lockCheckMig) Current() int  { return 2 }
func (m lockCheckMig) Migrate(from int, raw []byte) ([]byte, error) {
	f, err := os.OpenFile(filepath.Join(m.home, "doc.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err == nil {
		defer f.Close()
		if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil {
			*m.unlocked = true
			syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		}
	}
	return []byte(`{"version":2,"items":[]}`), nil
}

// Review: an old file is upgraded only under the lock, from a fresh read,
// so another process's write in between is never overwritten.
func TestReadMigratesOnlyUnderTheLock(t *testing.T) {
	s := &Store{Home: t.TempDir()}
	os.WriteFile(filepath.Join(s.Home, "doc.json"), []byte(`{"version":1,"items":[]}`), 0o644)
	unlocked := false
	if _, err := s.Read("doc", lockCheckMig{home: s.Home, unlocked: &unlocked}); err != nil {
		t.Fatal(err)
	}
	if unlocked {
		t.Fatal("Migrate ran without the lock held")
	}
}

// A symlinked file (a dotfiles repo's .zshrc or config.toml) is written
// through: the link stays a link and the target gets the new content.
func TestWriteFileFollowsSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles", "zshrc")
	os.MkdirAll(filepath.Dir(target), 0o755)
	os.WriteFile(target, []byte("old\n"), 0o644)
	link := filepath.Join(dir, ".zshrc")
	os.Symlink(target, link)
	if err := WriteFile(link, []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the link was replaced by a file")
	}
	if b, _ := os.ReadFile(target); string(b) != "new\n" {
		t.Fatalf("%q", b)
	}
}

// Review: replacing an existing file keeps its permissions (a private
// 0600 .zshrc stays private); perm only applies to a new file. A dangling
// symlink is followed: the target is created and the link stays.
func TestWriteFileKeepsModeAndFollowsDanglingLinks(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".zshrc")
	os.WriteFile(p, []byte("old\n"), 0o600)
	WriteFile(p, []byte("new\n"), 0o644)
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	target := filepath.Join(dir, "dotfiles", "bashrc")
	link := filepath.Join(dir, ".bashrc")
	os.Symlink(target, link)
	if err := WriteFile(link, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("a dangling link was replaced by a file")
	}
	if b, _ := os.ReadFile(target); string(b) != "x\n" {
		t.Fatalf("%q", b)
	}
}

// EditFile is read-modify-write under a lock: concurrent edits of one file
// all survive.
func TestEditFileSerializesEdits(t *testing.T) {
	p := filepath.Join(t.TempDir(), "rc")
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			EditFile(p, 0o644, func(old []byte) ([]byte, error) {
				return append(old, []byte(fmt.Sprintf("line %d\n", i))...), nil
			})
		}()
	}
	wg.Wait()
	b, _ := os.ReadFile(p)
	if n := strings.Count(string(b), "line "); n != 20 {
		t.Fatalf("%d of 20 edits survived:\n%s", n, b)
	}
}
