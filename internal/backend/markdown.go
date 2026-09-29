package backend

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The markdown backend: a FOLLOWUPS.md checklist in the repo. Refs point at a
// marker comment carrying the thread id, so hand edits and reordering keep
// the ref valid and filing the same thread twice finds the first marker.
const MarkdownFile = "FOLLOWUPS.md"

// Markdown is the built-in Implementation over the package functions.
type Markdown struct{}

func (Markdown) Detect(project string, _ io.Writer) bool    { return MarkdownDetect(project) }
func (Markdown) File(req Request) (string, error)           { return MarkdownFileNote(req.Project, req) }
func (Markdown) Status(project, ref string) (string, error) { return MarkdownStatus(project, ref), nil }
func (Markdown) Close(project, ref string) error            { return MarkdownClose(project, ref) }
func (Markdown) URL(string, string) (string, error)         { return "", ErrUnsupported }

func mdPath(project string) string { return filepath.Join(project, MarkdownFile) }

func MarkdownDetect(project string) bool {
	st, err := os.Stat(mdPath(project))
	return err == nil && !st.IsDir()
}

// openLocked opens (creating if asked) and takes an exclusive flock. All ops
// serialize on the file itself; concurrent agents are the normal case.
func openLocked(project string, create bool) (*os.File, error) {
	flags := os.O_RDWR
	if create {
		flags |= os.O_CREATE
	}
	f, err := os.OpenFile(mdPath(project), flags, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// refKey is the marker key a markdown ref points at: the note's uid
// ("md:FOLLOWUPS.md:0123456789ab"), or, for refs written before uids,
// "<id>:<tag>" ("md:FOLLOWUPS.md:7:abcd1234").
func refKey(ref string) (string, bool) {
	key, found := strings.CutPrefix(ref, "md:"+MarkdownFile+":")
	if !found || key == "" {
		return "", false
	}
	if id, tag, old := strings.Cut(key, ":"); old {
		if _, err := strconv.Atoi(id); err != nil || tag == "" {
			return "", false
		}
	}
	return key, true
}

func refFor(key string) string { return "md:" + MarkdownFile + ":" + key }

// findMarker returns the lines, the index of the first line holding the
// exact marker (-1 if none), and how many lines held it. Thread ids are
// per-installation, so a marker is only ours if the tag matches too.
func findMarker(r io.Reader, key string) ([]string, int, int, error) {
	var lines []string
	idx, count := -1, 0
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	m := markerComment(key)
	for sc.Scan() {
		if strings.Contains(sc.Text(), m) {
			count++
			if idx < 0 {
				idx = len(lines)
			}
		}
		lines = append(lines, sc.Text())
	}
	return lines, idx, count, sc.Err()
}

// existingRef finds a marker for this id whose line carries exactly this
// text — the crash-recovery case (created upstream, ref not recorded). A
// teammate's marker with the same id but other text is not ours.
func existingRef(r io.Reader, req Request) (string, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	if req.UID != "" {
		m := markerComment(req.UID)
		for sc.Scan() {
			if strings.Contains(sc.Text(), m) {
				return refFor(req.UID), nil
			}
		}
		return "", sc.Err()
	}
	id, text := req.ID, req.Text
	m := fmt.Sprintf("<!-- sous:%d:", id)
	want := "] " + text + " " + m
	for sc.Scan() {
		line := sc.Text()
		i := strings.Index(line, m)
		if i < 0 || !strings.Contains(line, want) {
			continue
		}
		end := strings.Index(line[i+len(m):], " -->")
		if end < 0 {
			continue
		}
		return refFor(strconv.Itoa(id) + ":" + line[i+len(m):i+len(m)+end]), nil
	}
	return "", sc.Err()
}

func MarkdownFileNote(project string, req Request) (string, error) {
	f, err := openLocked(project, true)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if ref, err := existingRef(f, req); err != nil {
		return "", err
	} else if ref != "" {
		return ref, nil // already filed (crash between create and record)
	}
	key := req.UID
	if key == "" { // a caller that sends no uid: the older id:tag marker
		sum := sha256.Sum256([]byte(req.Text + "\n" + time.Now().Format(time.RFC3339Nano)))
		key = strconv.Itoa(req.ID) + ":" + hex.EncodeToString(sum[:])[:8]
	}
	var prefix string
	st, _ := f.Stat()
	switch {
	case st.Size() == 0:
		prefix = "# Follow-ups\n\n"
	default:
		buf := make([]byte, 1)
		f.ReadAt(buf, st.Size()-1)
		if buf[0] != '\n' {
			prefix = "\n"
		}
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return "", err
	}
	if _, err := fmt.Fprintf(f, "%s- [ ] %s %s\n", prefix, req.Text, markerComment(key)); err != nil {
		return "", err
	}
	return refFor(key), nil
}

func boxState(line string) string {
	l := strings.TrimSpace(line)
	for _, b := range []string{"- ", "* "} {
		switch {
		case strings.HasPrefix(l, b+"[ ]"):
			return "open"
		case strings.HasPrefix(l, b+"[x]"), strings.HasPrefix(l, b+"[X]"):
			return "closed"
		}
	}
	return "unknown"
}

func MarkdownStatus(project, ref string) string {
	key, ok := refKey(ref)
	if !ok {
		return "unknown"
	}
	f, err := openLocked(project, false)
	if err != nil {
		return "unknown"
	}
	defer f.Close()
	lines, idx, count, err := findMarker(f, key)
	if err != nil || count != 1 {
		return "unknown"
	}
	return boxState(lines[idx])
}

// MarkdownClose flips the box on the unique marker line, rewriting the file
// in place under the lock — no rename, so a symlinked FOLLOWUPS.md and its
// mode survive and no temp file can be left behind.
func MarkdownClose(project, ref string) error {
	key, ok := refKey(ref)
	if !ok {
		return errors.New("not a markdown ref")
	}
	f, err := openLocked(project, false)
	if err != nil {
		return err
	}
	defer f.Close()
	lines, idx, count, err := findMarker(f, key)
	if err != nil {
		return err
	}
	switch {
	case count == 0:
		return fmt.Errorf("marker %s not found in %s", key, MarkdownFile)
	case count > 1:
		return fmt.Errorf("marker %s appears %d times in %s; fix the file by hand", key, count, MarkdownFile)
	}
	switch boxState(lines[idx]) {
	case "closed":
		return nil
	case "unknown":
		return fmt.Errorf("marker %s is not on a checkbox line", key)
	}
	// Tick the box by overwriting its one byte in place: nothing is
	// truncated, so a crash cannot empty the file, and every other byte
	// (line endings included) stays as the person wrote it.
	off := 0
	for _, l := range lines[:idx] {
		off += len(l) + 1
	}
	raw := make([]byte, len(lines[idx])+1)
	if _, err := f.ReadAt(raw, int64(off)); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	box := bytes.Index(raw, []byte("[ ]"))
	if box < 0 {
		return fmt.Errorf("marker %s is not on a checkbox line", key)
	}
	_, err = f.WriteAt([]byte("x"), int64(off+box+1))
	return err
}
