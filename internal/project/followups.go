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
	return readFollowups(root, true)
}

// FollowupsAll returns every open box in root's FOLLOWUPS.md (case-insensitive),
// stripping sous markers so recall and summaries stay readable.
func FollowupsAll(root string) ([]string, time.Time, error) {
	return readFollowups(root, false)
}

func readFollowups(root string, skipSous bool) ([]string, time.Time, error) {
	name := FollowupsFile
	if !skipSous {
		name = findCaseFile(root, FollowupsFile)
	}
	all, at, err := readOptional(filepath.Join(root, name))
	if err != nil || all == nil {
		return nil, at, err
	}
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(all))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if Box(line) != "open" {
			continue
		}
		if skipSous && strings.Contains(line, marker) {
			continue
		}
		_, item, _ := strings.Cut(line, "[ ]")
		if !skipSous {
			if before, _, hasMarker := strings.Cut(item, marker); hasMarker {
				item = before
			}
		}
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out, at, sc.Err()
}

// StatusDoc holds what a project's STATUS.md says, beyond a single Next step.
type StatusDoc struct {
	Exists   bool      `json:"exists"`
	Path     string    `json:"path,omitempty"`
	Updated  time.Time `json:"updated,omitempty"`
	NextStep string    `json:"next_step,omitempty"`
	Summary  string    `json:"summary,omitempty"`
	Body     string    `json:"-"`
}

// ReadStatus reads root's STATUS.md (case-insensitive), returning its next
// step, a concise plain-text summary of the rest, and its full body (capped at
// 32 KiB) for search.
func ReadStatus(root string) (StatusDoc, error) {
	name := findCaseFile(root, StatusFile)
	all, at, err := readOptional(filepath.Join(root, name))
	if err != nil || all == nil {
		return StatusDoc{}, err
	}
	const maxBody = 32768
	if len(all) > maxBody {
		all = all[:maxBody]
	}
	body := string(all)
	return StatusDoc{
		Exists:   true,
		Path:     name,
		Updated:  at.UTC(),
		NextStep: parseNextStep(all),
		Summary:  summarizeStatus(body),
		Body:     body,
	}, nil
}

// NextStep is the next step root's STATUS.md gives, and when the file last
// changed: the first line under a heading that names the next step ("##
// Next step", "## Next"), or a line that starts "Next step:" or "Next:".
// "" when the file has none.
func NextStep(root string) (string, time.Time, error) {
	all, at, err := readOptional(filepath.Join(root, findCaseFile(root, StatusFile)))
	if err != nil || all == nil {
		return "", at, err
	}
	return parseNextStep(all), at, nil
}

func isNextHeading(h string) bool {
	h = strings.ToLower(strings.TrimSpace(h))
	return h == "next" || strings.HasPrefix(h, "next step") || strings.HasPrefix(h, "next up")
}

func isInlineNextLine(line string) bool {
	plain := strings.TrimSpace(strings.ReplaceAll(listItem(line), "**", ""))
	for _, p := range []string{"next step:", "next steps:", "next:"} {
		if len(plain) >= len(p) && strings.EqualFold(plain[:len(p)], p) {
			return true
		}
	}
	return false
}

func parseNextStep(all []byte) string {
	under := false
	sc := bufio.NewScanner(bytes.NewReader(all))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "#") {
			h := strings.TrimSpace(strings.TrimLeft(line, "#"))
			under = isNextHeading(h)
			continue
		}
		if under {
			if step := listItem(line); step != "" {
				return step
			}
			continue
		}
		plain := strings.ReplaceAll(line, "**", "")
		for _, p := range []string{"next step:", "next steps:", "next:"} {
			if len(plain) > len(p) && strings.EqualFold(plain[:len(p)], p) {
				if step := strings.TrimSpace(plain[len(p):]); step != "" {
					return step
				}
			}
		}
	}
	return ""
}

func summarizeStatus(body string) string {
	var parts []string
	total := 0
	skip := false
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			h := strings.TrimSpace(strings.TrimLeft(line, "#"))
			skip = isNextHeading(h)
			if skip || h == "" || strings.HasPrefix(line, "# ") || !strings.HasPrefix(line, "##") {
				continue
			}
			line = h + ":"
		} else if skip || isInlineNextLine(line) {
			continue
		}
		parts = append(parts, line)
		total += len([]rune(line))
		if total >= 400 || len(parts) >= 8 {
			break
		}
	}
	return strings.Join(parts, " ")
}

func findCaseFile(root, target string) string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return target
	}
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(e.Name(), target) {
			return e.Name()
		}
	}
	return target
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
