package report

import (
	"bytes"
	"fmt"
	"path/filepath"
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

// Window is where a report taken now starts: the last seven days for a
// week report, else since the last report taken, or a day back for the
// first one.
func Window(s *store.Store, now time.Time, week bool) (time.Time, error) {
	if week {
		return now.Add(-7 * 24 * time.Hour), nil
	}
	m, err := store.Load[markDoc](s, "report", markMigrator{})
	if err != nil {
		return time.Time{}, err
	}
	if m.LastReport != nil {
		return *m.LastReport, nil
	}
	return now.Add(-24 * time.Hour), nil
}

// Take records that a report was seen at now, so the next one starts
// there. A week report looks back without moving the mark.
func Take(s *store.Store, now time.Time, week bool) error {
	if week {
		return nil
	}
	_, err := store.Modify[markDoc](s, "report", markMigrator{}, func(m *markDoc) error {
		m.Version, m.LastReport = 1, &now
		return nil
	})
	return err
}

// WritePage writes the report page to home/report.html and returns its
// path. The page is a static file: no scripts, nothing loaded.
func WritePage(home string, r Report) (string, error) {
	var b bytes.Buffer
	if err := RenderHTML(&b, r); err != nil {
		return "", err
	}
	p := filepath.Join(home, "report.html")
	return p, store.WriteFile(p, b.Bytes(), 0o644)
}
