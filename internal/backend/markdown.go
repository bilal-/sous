package backend

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bilal-/sous/internal/plugin"
	"github.com/bilal-/sous/internal/store"
)

// The markdown backend: a FOLLOWUPS.md checklist in the repo. Refs point at a
// marker comment carrying the thread id, so hand edits and reordering keep
// the ref valid and filing the same thread twice finds the first marker.
const MarkdownFile = "FOLLOWUPS.md"

// Markdown is the built-in Implementation over the package functions.
type Markdown struct{}

func (Markdown) Detect(project string) error {
	if MarkdownDetect(project) {
		return nil
	}
	return plugin.ErrNo
}
func (Markdown) File(req Request) (string, error)           { return MarkdownFileNote(req.Project, req) }
func (Markdown) Status(project, ref string) (string, error) { return MarkdownStatus(project, ref) }
func (Markdown) Close(project, ref string) error            { return MarkdownClose(project, ref) }
func (Markdown) URL(string, string) (string, error)         { return "", ErrUnsupported }

func mdPath(project string) string { return filepath.Join(project, MarkdownFile) }

func MarkdownDetect(project string) bool {
	st, err := os.Stat(mdPath(project))
	return err == nil && !st.IsDir()
}

// refKey is the marker key a markdown ref points at: the note's uid
// ("md:FOLLOWUPS.md:0123456789ab"), or, for refs written before uids,
// "<id>:<tag>" ("md:FOLLOWUPS.md:7:abcd1234").
func refKey(ref string) (string, bool) {
	key, found := strings.CutPrefix(ref, mdPrefix+MarkdownFile+":")
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

// mdPrefix starts a markdown ref, short for the backend's name.
const mdPrefix = "md:"

func refFor(key string) string { return mdPrefix + MarkdownFile + ":" + key }

// existingRef finds an item already filed for this note, so a retry after
// a crash never files it twice: by its uid marker, or by the marker a sous
// before uids wrote (same note number and text: <!-- sous:<id>:<tag> -->).
func existingRef(all []byte, req Request) string {
	if bytes.Contains(all, []byte(markerComment(req.UID))) {
		return refFor(req.UID)
	}
	if !req.Legacy {
		return ""
	}
	legacy := fmt.Sprintf("] %s <!-- sous:%d:", req.Text, req.ID)
	if at := bytes.Index(all, []byte(legacy)); at >= 0 {
		tag := all[at+len(legacy):]
		if end := bytes.Index(tag, []byte(" -->")); end > 0 {
			return refFor(strconv.Itoa(req.ID) + ":" + string(tag[:end]))
		}
	}
	return ""
}

func MarkdownFileNote(project string, req Request) (string, error) {
	if req.UID == "" {
		return "", errors.New("the request has no uid")
	}
	ref := ""
	err := store.EditFile(mdPath(project), 0o644, func(all []byte) ([]byte, error) {
		if ref = existingRef(all, req); ref != "" {
			return nil, nil // already filed (crash between create and record)
		}
		ref = refFor(req.UID)
		switch {
		case len(all) == 0:
			all = []byte("# Follow-ups\n\n")
		case all[len(all)-1] != '\n':
			all = append(all, '\n')
		}
		return fmt.Appendf(all, "- [ ] %s %s\n", req.Text, markerComment(req.UID)), nil
	})
	return ref, err
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

// MarkdownStatus reads the box on the ref's marker line: open or closed;
// unknown when the file or the marker is gone. It only reads, and a file
// that exists but cannot be read is an error, never "unknown".
func MarkdownStatus(project, ref string) (string, error) {
	key, ok := refKey(ref)
	if !ok {
		return "unknown", nil
	}
	all, err := os.ReadFile(mdPath(project))
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "unknown", nil
	case err != nil:
		return "unknown", err
	}
	line, n := markerLine(all, key)
	if n != 1 {
		return "unknown", nil
	}
	return boxState(string(line)), nil
}

// markerLine is the line holding key's marker, up to the marker, and how
// many times the marker appears.
func markerLine(all []byte, key string) ([]byte, int) {
	m := []byte(markerComment(key))
	n := bytes.Count(all, m)
	if n == 0 {
		return nil, 0
	}
	at := bytes.Index(all, m)
	return all[bytes.LastIndexByte(all[:at], '\n')+1 : at], n
}

// MarkdownClose flips the box on the unique marker line, rewriting the file
// in place under the lock — no rename, so a symlinked FOLLOWUPS.md and its
// mode survive and no temp file can be left behind.
func MarkdownClose(project, ref string) error {
	key, ok := refKey(ref)
	if !ok {
		return errors.New("not a markdown ref")
	}
	return store.EditFile(mdPath(project), 0o644, func(all []byte) ([]byte, error) {
		line, n := markerLine(all, key)
		switch {
		case n == 0:
			return nil, fmt.Errorf("marker %s not found in %s", key, MarkdownFile)
		case n > 1:
			return nil, fmt.Errorf("marker %s appears %d times in %s; fix the file by hand", key, n, MarkdownFile)
		}
		switch boxState(string(line)) {
		case "closed":
			return nil, nil
		case "unknown":
			return nil, fmt.Errorf("marker %s is not on a checkbox line", key)
		}
		// Tick the one box; every other byte (line endings too) stays.
		at := bytes.Index(all, []byte(markerComment(key))) - len(line)
		box := at + bytes.Index(line, []byte("[ ]")) + 1
		out := bytes.Clone(all)
		out[box] = 'x'
		return out, nil
	})
}
