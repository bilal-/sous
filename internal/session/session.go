// Package session keeps one pointer per project to the last agent session
// that ended there: which agent, when, and its last message. It records;
// it infers nothing.
package session

import (
	"time"

	"github.com/bilal-/sous/internal/store"
)

// LastMessageRunes caps how much of a session's last message is kept, and
// shown where space is short.
const LastMessageRunes = 300

type Session struct {
	Agent       string    `json:"agent"`
	SessionID   string    `json:"session_id"`
	Ended       time.Time `json:"ended"`
	LastMessage *string   `json:"last_message"`
}

type Doc struct {
	Version  int                `json:"version"`
	Sessions map[string]Session `json:"sessions"`
}

// sessionsFile is how the file is read and upgraded.
var sessionsFile = store.V1(`{"version":1,"sessions":{}}`)

// Record upserts the pointer for a repo root. Deterministic; infers nothing.
func Record(s *store.Store, root string, sess Session) error {
	_, err := store.Modify[Doc](s, "sessions", sessionsFile, func(d *Doc) error {
		if d.Sessions == nil {
			d.Sessions = map[string]Session{}
		}
		sess.Ended = sess.Ended.UTC()
		d.Sessions[root] = sess
		return nil
	})
	return err
}

// All reads every recorded session, keyed by repo root.
func All(s *store.Store) (map[string]Session, error) {
	d, err := store.Load[Doc](s, "sessions", sessionsFile)
	if err != nil {
		return nil, err
	}
	return d.Sessions, nil
}
