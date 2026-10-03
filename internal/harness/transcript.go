package harness

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/bilal-/sous/internal/text"
)

// lastLine is the text of the last line in a JSONL transcript that said
// reads as said by the agent, capped at max runes; "" when there is none or
// the file is odd. It reads backwards from the end and stops at that line,
// so a transcript of any size costs only what comes after it.
// mark is a few bytes every such line holds, to skip the rest cheaply.
func lastLine(path string, max int, mark string, said func([]byte) string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return ""
	}
	var last string
	eachLineFromEnd(f, st.Size(), func(line []byte) bool {
		if !bytes.Contains(line, []byte(mark)) {
			return true
		}
		last = said(line)
		return last == ""
	})
	return text.Cut(text.OneLine(last), max)
}

// contentText joins the parts of a message's content that are of kind
// (Claude's "text", Codex's "output_text"); content that is a plain string
// is the text itself.
func contentText(content json.RawMessage, kind string) string {
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &parts) == nil {
		var texts []string
		for _, p := range parts {
			if p.Type == kind && p.Text != "" {
				texts = append(texts, p.Text)
			}
		}
		return strings.Join(texts, " ")
	}
	var s string
	json.Unmarshal(content, &s)
	return s
}

// parseHookJSON reads the hook input Claude Code and Codex send: snake_case
// JSON whose source is startup for a new session. Nothing at all, or
// something unreadable, is a new session with no details: the hook's own
// folder stands in for the project.
func parseHookJSON(b []byte) Input {
	var in struct {
		SessionID      string `json:"session_id"`
		CWD            string `json:"cwd"`
		Source         string `json:"source"` // startup | resume | clear | compact | fork
		TranscriptPath string `json:"transcript_path"`
		LastMessage    string `json:"last_message"` // sous's own plugins send it
	}
	if json.Unmarshal(b, &in) != nil {
		return Input{Fresh: true}
	}
	return Input{SessionID: in.SessionID, CWD: in.CWD, TranscriptPath: in.TranscriptPath, LastMessage: in.LastMessage, Fresh: in.Source == "" || in.Source == "startup"}
}

// eachLineFromEnd calls fn for each line of r, last line first, until fn
// returns false. It reads in chunks from the end.
func eachLineFromEnd(r io.ReaderAt, size int64, fn func([]byte) bool) {
	const chunk = 1 << 20
	var parts [][]byte // the unfinished line's pieces, last piece first
	flush := func(head []byte) bool {
		line := head
		if len(parts) > 0 { // join once, when the line's start is found
			n := len(head)
			for _, p := range parts {
				n += len(p)
			}
			line = make([]byte, 0, n)
			line = append(line, head...)
			for i := len(parts) - 1; i >= 0; i-- {
				line = append(line, parts[i]...)
			}
			parts = parts[:0]
		}
		return len(line) == 0 || fn(line)
	}
	for end := size; end > 0; {
		start := max(end-chunk, 0)
		buf := make([]byte, end-start)
		if _, err := r.ReadAt(buf, start); err != nil && !errors.Is(err, io.EOF) {
			return
		}
		for {
			nl := bytes.LastIndexByte(buf, '\n')
			if nl < 0 {
				break
			}
			if !flush(buf[nl+1:]) {
				return
			}
			buf = buf[:nl]
		}
		parts = append(parts, buf) // only newly read bytes are ever scanned
		end = start
	}
	flush(nil)
}

// lastJSON is the last JSON value in a run's log that decodes as a T and
// ok accepts; the zero T when there is none. Values may span lines (pretty
// printed) or share none; whatever else the log holds is passed over a line
// at a time.
func lastJSON[T any](log []byte, ok func(T) bool) (last T) {
	for rest := log; len(bytes.TrimSpace(rest)) > 0; {
		d := json.NewDecoder(bytes.NewReader(rest))
		var v T
		if d.Decode(&v) != nil {
			i := bytes.IndexByte(rest, '\n')
			if i < 0 {
				break
			}
			rest = rest[i+1:]
			continue
		}
		if ok(v) {
			last = v
		}
		rest = rest[d.InputOffset():]
	}
	return last
}

// firstMatch reads a run's session from its log: re's first group, at its
// first match.
func firstMatch(re *regexp.Regexp) func(log []byte) string {
	return func(log []byte) string {
		if m := re.FindSubmatch(log); m != nil {
			return string(m[1])
		}
		return ""
	}
}
