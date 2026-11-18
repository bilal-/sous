// Package session keeps per-project and per-branch agent session context:
// which agent, branch, user prompt, modified files, when it ended, and its
// last message, plus a bounded rolling history. It records; it infers nothing.
package session

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/bilal-/sous/internal/store"
)

// LastMessageRunes caps how much of a session's last message or prompt is
// kept, and shown where space is short.
const LastMessageRunes = 300

// MaxHistory bounds how many session history entries sous keeps across projects.
const MaxHistory = 200

const maxStarted = 100

type Session struct {
	Agent       string    `json:"agent"`
	SessionID   string    `json:"session_id"`
	Ended       time.Time `json:"ended"`
	LastMessage *string   `json:"last_message"`
	Branch      string    `json:"branch,omitempty"`
	Prompt      *string   `json:"prompt,omitempty"`
	Files       []string  `json:"files,omitempty"`
	Commit      string    `json:"commit,omitempty"`
}

// Entry is one session record in the rolling history, carrying its project path.
type Entry struct {
	Project string `json:"project"`
	Session
}

type Doc struct {
	Version  int                           `json:"version"`
	Sessions map[string]Session            `json:"sessions"`
	Branches map[string]map[string]Session `json:"branches,omitempty"`
	History  []Entry                       `json:"history,omitempty"`
	Started  map[string]time.Time          `json:"started,omitempty"`
}

type sessionsMigrator struct{}

func (sessionsMigrator) Empty() []byte {
	return []byte(`{"version":2,"sessions":{},"branches":{},"history":[]}`)
}
func (sessionsMigrator) Current() int { return 2 }
func (sessionsMigrator) Migrate(from int, raw []byte) ([]byte, error) {
	if from != 1 {
		return nil, fmt.Errorf("cannot migrate sessions from version %d", from)
	}
	var old struct {
		Version  int                `json:"version"`
		Sessions map[string]Session `json:"sessions"`
	}
	if err := json.Unmarshal(raw, &old); err != nil {
		return nil, err
	}
	doc := Doc{
		Version:  2,
		Sessions: old.Sessions,
		Branches: map[string]map[string]Session{},
		History:  []Entry{},
	}
	if doc.Sessions == nil {
		doc.Sessions = map[string]Session{}
	}
	for root, sess := range doc.Sessions {
		doc.History = append(doc.History, Entry{Project: root, Session: sess})
	}
	sortHistory(doc.History)
	return json.Marshal(doc)
}

var sessionsFile store.Migrator = sessionsMigrator{}

func sortHistory(h []Entry) {
	sort.SliceStable(h, func(i, j int) bool {
		if !h[i].Ended.Equal(h[j].Ended) {
			return h[i].Ended.After(h[j].Ended)
		}
		if h[i].Project != h[j].Project {
			return h[i].Project < h[j].Project
		}
		return h[i].SessionID < h[j].SessionID
	})
}

func mergeSameSession(prev, next Session) Session {
	if prev.SessionID == "" || prev.SessionID == "unknown" || prev.SessionID != next.SessionID {
		return next
	}
	if next.Prompt == nil && prev.Prompt != nil {
		next.Prompt = prev.Prompt
	}
	if next.LastMessage == nil && prev.LastMessage != nil {
		next.LastMessage = prev.LastMessage
	}
	if len(next.Files) == 0 && len(prev.Files) > 0 {
		next.Files = prev.Files
	}
	if next.Commit == "" {
		next.Commit = prev.Commit
	}
	if next.Branch == "" {
		next.Branch = prev.Branch
	}
	return next
}

// Record upserts the pointer for a repo root, its branch map, and the rolling
// history. Deterministic; infers nothing.
func Record(s *store.Store, root string, sess Session) error {
	_, err := store.Modify[Doc](s, "sessions", sessionsFile, func(d *Doc) error {
		if d.Sessions == nil {
			d.Sessions = map[string]Session{}
		}
		if d.Branches == nil {
			d.Branches = map[string]map[string]Session{}
		}
		sess.Ended = sess.Ended.UTC()
		if prev, ok := d.Sessions[root]; ok {
			sess = mergeSameSession(prev, sess)
		}
		d.Sessions[root] = sess
		if sess.Branch != "" {
			if d.Branches[root] == nil {
				d.Branches[root] = map[string]Session{}
			}
			d.Branches[root][sess.Branch] = sess
		}
		updated := false
		if sess.SessionID != "" && sess.SessionID != "unknown" {
			for i := range d.History {
				if d.History[i].Project == root && d.History[i].SessionID == sess.SessionID && d.History[i].Branch == sess.Branch {
					d.History[i].Session = mergeSameSession(d.History[i].Session, sess)
					updated = true
					break
				}
			}
		}
		if !updated {
			d.History = append(d.History, Entry{Project: root, Session: sess})
		}
		sortHistory(d.History)
		if len(d.History) > MaxHistory {
			d.History = d.History[:MaxHistory]
		}
		return nil
	})
	return err
}

// MarkStarted records that a session key received startup context, returning
// true on the first call for that key. Used by harnesses whose injection hook
// runs on every user prompt rather than once per session.
func MarkStarted(s *store.Store, key string, now time.Time) bool {
	if key == "" {
		return true
	}
	fresh := false
	_, err := store.Modify[Doc](s, "sessions", sessionsFile, func(d *Doc) error {
		if d.Started == nil {
			d.Started = map[string]time.Time{}
		}
		if _, seen := d.Started[key]; seen {
			return nil
		}
		fresh = true
		d.Started[key] = now.UTC()
		if len(d.Started) > maxStarted {
			var oldestKey string
			var oldestAt time.Time
			for k, at := range d.Started {
				if oldestKey == "" || at.Before(oldestAt) {
					oldestKey, oldestAt = k, at
				}
			}
			if oldestKey != "" {
				delete(d.Started, oldestKey)
			}
		}
		return nil
	})
	if err != nil {
		return true
	}
	return fresh
}

// All reads every recorded latest session, keyed by repo root.
func All(s *store.Store) (map[string]Session, error) {
	d, err := store.Load[Doc](s, "sessions", sessionsFile)
	if err != nil {
		return nil, err
	}
	return d.Sessions, nil
}

// History returns the rolling session history across all projects, newest first.
func History(s *store.Store) ([]Entry, error) {
	d, err := store.Load[Doc](s, "sessions", sessionsFile)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, len(d.History))
	copy(out, d.History)
	return out, nil
}

// ForProject returns the rolling session history for one repo root, newest first.
func ForProject(s *store.Store, root string) ([]Entry, error) {
	d, err := store.Load[Doc](s, "sessions", sessionsFile)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, e := range d.History {
		if e.Project == root {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		if sess, ok := d.Sessions[root]; ok {
			out = append(out, Entry{Project: root, Session: sess})
		}
	}
	return out, nil
}
