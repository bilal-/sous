package report

import (
	"bytes"
	"os"
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

// markFile is how the file is read and upgraded.
var markFile = store.V1(`{"version":1,"last_report":null}`)

// Window is where a report taken now starts: the last seven days for a
// week report, else since the last report taken, or a day back for the
// first one.
func Window(s *store.Store, now time.Time, week bool) (time.Time, error) {
	if week {
		return now.Add(-7 * 24 * time.Hour), nil
	}
	m, err := store.Load[markDoc](s, "report", markFile)
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
	_, err := store.Modify[markDoc](s, "report", markFile, func(m *markDoc) error {
		m.LastReport = &now
		return nil
	})
	return err
}

// WritePage writes the report page to home/report.html and returns its
// path. The page is a static file: no scripts, nothing loaded.
func WritePage(home string, r Report) (string, error) {
	var b bytes.Buffer
	if err := renderHTML(&b, r); err != nil {
		return "", err
	}
	p := filepath.Join(home, "report.html")
	if err := store.WriteFile(p, b.Bytes(), 0o600); err != nil {
		return "", err
	}
	return p, os.Chmod(p, 0o600) // it holds note text: private, even if it existed
}
