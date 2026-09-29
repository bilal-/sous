package report

import (
	"fmt"
	"time"

	"github.com/bilal-/sous/internal/store"
)

// markDoc remembers when the last report was taken, so the default window
// is "since you last looked at a report".
type markDoc struct {
	Version    int        `json:"version"`
	LastReport *time.Time `json:"last_report"`
}

type markMigrator struct{}

func (markMigrator) Empty() []byte { return []byte(`{"version":1,"last_report":null}`) }
func (markMigrator) Current() int  { return 1 }
func (markMigrator) Migrate(from int, _ []byte) ([]byte, error) {
	return nil, fmt.Errorf("no migration from v%d", from)
}

// LastTaken is when the last report was taken; ok is false before the first.
func LastTaken(s *store.Store) (t time.Time, ok bool, err error) {
	m, err := store.Load[markDoc](s, "report", markMigrator{})
	if err != nil || m.LastReport == nil {
		return time.Time{}, false, err
	}
	return *m.LastReport, true, nil
}

// MarkTaken records a report taken at now.
func MarkTaken(s *store.Store, now time.Time) error {
	_, err := store.Modify[markDoc](s, "report", markMigrator{}, func(m *markDoc) error {
		m.Version, m.LastReport = 1, &now
		return nil
	})
	return err
}
