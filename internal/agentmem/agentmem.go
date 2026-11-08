// Package agentmem reads the memory agents keep for themselves, one folder
// of Markdown files per project, as sous memory records. It never writes
// those files: the agent owns them, and sous only makes them findable from
// every project and every agent.
package agentmem

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/harness"
	"github.com/bilal-/sous/internal/memory"
	"github.com/bilal-/sous/internal/project"
)

// Off is the agent_memory setting that stops sous reading it.
const Off = "off"

// IDPrefix starts the id of every record read from an agent's memory.
const IDPrefix = "a:"

// maxFile bounds what is read of one file, like a memory's own text.
const maxFile = 32768

// index is the agent's table of contents, not a memory.
const index = "MEMORY.md"

func enabled(c *config.Config) bool { return c == nil || c.AgentMemory != Off }

// ForProject is what each agent remembers about one project.
func ForProject(home string, c *config.Config, path string) []memory.Record {
	if !enabled(c) || path == "" {
		return nil
	}
	var out []memory.Record
	for _, h := range harness.All {
		if h.Memory != nil {
			out = append(out, read(h, h.Memory.Dir(home, path), memory.ForProject(c, project.Project{Path: path}))...)
		}
	}
	return out
}

// All is what each agent remembers about every project.
func All(home string, c *config.Config) []memory.Record {
	if !enabled(c) {
		return nil
	}
	var out []memory.Record
	for _, h := range harness.All {
		if h.Memory == nil {
			continue
		}
		dirs := h.Memory.Projects(home)
		paths := make([]string, 0, len(dirs))
		for p := range dirs {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			out = append(out, read(h, dirs[p], memory.ForProject(c, project.Project{Path: p}))...)
		}
	}
	return out
}

// Get is one record by its id, from any project.
func Get(home string, c *config.Config, id string) (memory.Record, error) {
	for _, r := range All(home, c) {
		if r.ID == id {
			return r, nil
		}
	}
	return memory.Record{}, fmt.Errorf("%w %s", memory.ErrNotFound, id)
}

// Owned says whether id names a record sous reads from an agent's memory.
func Owned(id string) bool { return strings.HasPrefix(id, IDPrefix) }

func read(h harness.Harness, dir string, sc memory.Scope) []memory.Record {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []memory.Record
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || name == index || !strings.HasSuffix(name, ".md") {
			continue
		}
		path := filepath.Join(dir, name)
		r, ok := record(path, sc)
		if !ok {
			continue
		}
		r.Author, r.Source = h.Name, name
		out = append(out, r)
	}
	return out
}

func record(path string, sc memory.Scope) (memory.Record, bool) {
	f, err := os.Open(path)
	if err != nil {
		return memory.Record{}, false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return memory.Record{}, false
	}
	b := make([]byte, maxFile)
	n, _ := io.ReadFull(f, b)
	front, body := split(string(b[:n]))
	text := strings.TrimSpace(strings.TrimSpace(front["description"]) + "\n\n" + strings.TrimSpace(body))
	if text == "" {
		return memory.Record{}, false
	}
	sum := sha256.Sum256([]byte(path))
	return memory.Record{
		ID: IDPrefix + hex.EncodeToString(sum[:])[:8], Scope: sc, Kind: kind(front["type"]), Text: text,
		CreatedAt: fi.ModTime().UTC(), SharedWith: []memory.Scope{}, Path: path,
	}, true
}

// kind maps the agent's memory types onto sous's: guidance about how to
// work and who the user is are constraints; project state is a handoff;
// pointers elsewhere are lessons.
func kind(t string) memory.Kind {
	switch t {
	case "user", "feedback":
		return memory.Constraint
	case "reference":
		return memory.Lesson
	default:
		return memory.Handoff
	}
}

// split reads the simple front matter agents write (key: value lines, one
// level of nesting) and returns it with the body that follows.
func split(s string) (map[string]string, string) {
	front := map[string]string{}
	rest, ok := strings.CutPrefix(s, "---\n")
	if !ok {
		return front, s
	}
	head, body, ok := strings.Cut(rest, "\n---")
	if !ok {
		return front, s
	}
	for _, line := range strings.Split(head, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' || value[0] == '\'' && value[len(value)-1] == '\'') {
			value = strings.ReplaceAll(value[1:len(value)-1], `\"`, `"`)
		}
		if _, seen := front[key]; !seen && value != "" {
			front[key] = value
		}
	}
	return front, strings.TrimPrefix(body, "\n")
}
