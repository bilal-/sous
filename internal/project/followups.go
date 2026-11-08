package project

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The project's own record of what is next: FOLLOWUPS.md, a checklist of
// unfinished actions, and STATUS.md, the handoff whose next step says where
// to pick up. sous reads them each time it looks, so a project nobody has
// opened for weeks still says what is waiting in it. Neither is copied into
// sous: the files stay the source, and ticking a box clears the row.
const (
	FollowupsFile = "FOLLOWUPS.md"
	StatusFile    = "STATUS.md"
)

// Box is a checklist line's box: "open" for "- [ ]", "closed" for "- [x]",
// "unknown" for any other line. "*" bullets count, and so does indenting.
func Box(line string) string {
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

// marker starts the comment sous writes on a line it filed. Those lines
// are notes already, followed by the markdown tracker, so they are not
// read again as follow-ups.
const marker = "<!-- sous:"

// Followups are the open boxes in root's FOLLOWUPS.md that sous did not
// file itself, in file order, and when the file last changed. No file is
// none, not an error; a file that cannot be read is an error.
func Followups(root string) ([]string, time.Time, error) {
	all, at, err := readOptional(filepath.Join(root, FollowupsFile))
	if err != nil || all == nil {
		return nil, at, err
	}
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(all))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if Box(line) != "open" || strings.Contains(line, marker) {
			continue
		}
		_, item, _ := strings.Cut(line, "[ ]")
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out, at, sc.Err()
}

// NextStep is the next step root's STATUS.md gives, and when the file last
// changed: the first line under a heading that names the next step ("##
// Next step", "## Next"), or a line that starts "Next step:" or "Next:".
// "" when the file has none.
func NextStep(root string) (string, time.Time, error) {
	all, at, err := readOptional(filepath.Join(root, StatusFile))
	if err != nil || all == nil {
		return "", at, err
	}
	under := false
	sc := bufio.NewScanner(bytes.NewReader(all))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "#") {
			h := strings.ToLower(strings.TrimSpace(strings.TrimLeft(line, "#")))
			under = h == "next" || strings.HasPrefix(h, "next step") || strings.HasPrefix(h, "next up")
			continue
		}
		if under {
			if step := listItem(line); step != "" {
				return step, at, nil
			}
			continue
		}
		plain := strings.ReplaceAll(line, "**", "")
		for _, p := range []string{"next step:", "next steps:", "next:"} {
			if len(plain) > len(p) && strings.EqualFold(plain[:len(p)], p) {
				if step := strings.TrimSpace(plain[len(p):]); step != "" {
					return step, at, nil
				}
			}
		}
	}
	return "", at, sc.Err()
}

// listItem is a line's text without its bullet, number or box.
func listItem(line string) string {
	for _, p := range []string{"- [ ] ", "* [ ] ", "- ", "* ", "+ "} {
		if s, ok := strings.CutPrefix(line, p); ok {
			return strings.TrimSpace(s)
		}
	}
	if i := strings.IndexAny(line, ".)"); i > 0 && i <= 3 && strings.Trim(line[:i], "0123456789") == "" {
		return strings.TrimSpace(line[i+1:])
	}
	return line
}

// readOptional reads a guide in the project: nil with no error when it is
// not there, and when it last changed.
func readOptional(path string) ([]byte, time.Time, error) {
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil, time.Time{}, nil
	case err != nil:
		return nil, time.Time{}, err
	case info.IsDir():
		return nil, time.Time{}, nil
	}
	all, err := os.ReadFile(path)
	if all == nil && err == nil {
		all = []byte{}
	}
	return all, info.ModTime(), err
}
