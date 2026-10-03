// Package board assembles and renders the cross-project attention list.
package board

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/signal"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/text"
	"github.com/bilal-/sous/internal/thread"
)

type Data struct {
	Projects    []project.Project     `json:"projects"`
	Signals     []signal.Observed     `json:"signals"`
	Threads     []thread.View         `json:"threads"`
	Plugins     []signal.PluginStatus `json:"plugins"`
	Checked     int                   `json:"checked"`
	Unavailable int                   `json:"unavailable"`
	RenderedAt  time.Time             `json:"rendered_at"`
}

type Inputs struct {
	Store *store.Store
	Cfg   *config.Config
	Roots []string // overrides Cfg.Roots when set
	// Signals are the signal plugins to run: every one for the board,
	// the offline built ins for here (signal.Registry.All, .Offline).
	Signals []signal.Plugin
	Timeout time.Duration
	Warn    io.Writer
	Now     time.Time
	// Reconcile, when set, checks filed threads against their trackers
	// (closing what the tracker closed) before the board is built.
	Reconcile Reconciler
}

// Reconciler is satisfied by *filing.Filer without board importing filing.
// It honours ctx: under the session hook the whole build shares one deadline.
type Reconciler func(ctx context.Context, views []thread.View) []thread.View

func Build(ctx context.Context, in Inputs) (*Data, error) {
	roots := in.Roots
	if len(roots) == 0 {
		roots = in.Cfg.Roots
	}
	if len(roots) == 0 {
		return nil, errors.New(config.NoRootsHint)
	}
	ps, unavailable := project.Discover(roots, in.Cfg.Ignore, in.Warn)
	paths := make([]string, len(ps))
	known := map[string]bool{}
	knownRemote := map[string]bool{}
	for i, p := range ps {
		paths[i] = p.Path
		known[p.Path] = true
		if p.Remote != nil {
			knownRemote[*p.Remote] = true
		}
	}
	col := signal.Collect(ctx, in.Signals, paths, in.Timeout)
	obs, err := signal.Observe(in.Store, col, paths, in.Now)
	if err != nil {
		return nil, err
	}
	var sigs []signal.Observed
	for _, o := range obs {
		if known[o.Project] {
			sigs = append(sigs, o)
		}
	}
	ths, err := thread.Open(in.Store, in.Now)
	if err != nil {
		return nil, err
	}
	if in.Reconcile != nil {
		ths = in.Reconcile(ctx, ths)
	}
	// A scoped board (explicit folder) shows only that folder's threads. The
	// full board keeps every open thread: a note whose clone is gone is still
	// a commitment, shown rather than dropped.
	if len(in.Roots) > 0 {
		kept := ths[:0]
		for _, t := range ths {
			if known[t.Project] || (t.Remote != nil && knownRemote[*t.Remote]) {
				kept = append(kept, t)
			}
		}
		ths = kept
	}
	return &Data{Projects: ps, Signals: sigs, Threads: ths, Plugins: col.Plugins, Checked: len(ps), Unavailable: unavailable, RenderedAt: in.Now.UTC()}, nil
}

func (r Row) line() string {
	return fmt.Sprintf("  %-14s  %-24s  %-60s  %s", r.ID, text.Ellipsize(filepath.Base(r.Project), 24), text.Ellipsize(r.Shown, 60), r.Age) + r.upstreamNote()
}

// upstreamNote says when a filed note's tracker could not vouch for it:
// missing data is shown, never passed off as fine.
func (r Row) upstreamNote() string {
	switch {
	case r.Upstream == "unknown":
		return " (ref missing)"
	case r.Upstream == "error":
		return " (status unavailable: " + r.UpstreamErr + ")"
	case r.RunErr != "":
		return " (status unavailable: " + r.RunErr + ")"
	}
	return ""
}

// Render lays out the classified board. The headline is what the eye reads:
// it must not say zero when an obligation source failed or nothing was
// checked.
func Render(w io.Writer, d *Data) { RenderSaved(w, d, d.RenderedAt) }

// RenderSaved draws a saved board as seen at now, so a board from another
// day says which day.
func RenderSaved(w io.Writer, d *Data, now time.Time) {
	s := Classify(d)
	switch {
	case d.Checked == 0:
		fmt.Fprintln(w, "sous · nothing checked · no projects in your project folders")
	case s.Total() == 0 && len(s.Why) == 0:
		fmt.Fprintln(w, "sous · 0 on you · nothing waiting")
	case len(s.Why) > 0:
		fmt.Fprintf(w, "sous · ? on you (%s) · %d found · %d on others · %d unfinished\n", strings.Join(s.Why, ", "), len(s.Me), len(s.Them), len(s.Unfinished))
	default:
		fmt.Fprintf(w, "sous · %d on you · %d on others · %d unfinished\n", len(s.Me), len(s.Them), len(s.Unfinished))
	}
	section := func(title string, rows []Row) {
		if len(rows) == 0 {
			return
		}
		fmt.Fprintf(w, "\n  %s\n", title)
		for _, r := range rows {
			fmt.Fprintln(w, r.line())
		}
	}
	section("on you", s.Me)
	section("on others", s.Them)
	section("unfinished", s.Unfinished)

	fmt.Fprintf(w, "\n  %d checked · %d unavailable%s · as of %s · sous snooze <id> to hide a row\n",
		d.Checked, d.Unavailable, PluginFailures(d), text.AsOf(d.RenderedAt, now))
}

// Cache: the last rendered board, so the zsh surface can print in ~5 ms and
// say how old what it printed is.
type CacheDoc struct {
	Version    int        `json:"version"`
	RenderedAt *time.Time `json:"rendered_at"`
	Board      *string    `json:"board"`
	Data       *Data      `json:"data,omitempty"` // additive: --menubar renders from it
}

type CacheMigrator struct{}

func (CacheMigrator) Empty() []byte { return []byte(`{"version":1,"rendered_at":null,"board":null}`) }
func (CacheMigrator) Current() int  { return 1 }
func (CacheMigrator) Migrate(from int, raw []byte) ([]byte, error) {
	return nil, fmt.Errorf("no migration from v%d", from)
}

// ReadCache is the last board written (empty before the first).
func ReadCache(s *store.Store) (*CacheDoc, error) {
	return store.Load[CacheDoc](s, "cache", CacheMigrator{})
}

func WriteCache(s *store.Store, d *Data) error {
	var b strings.Builder
	Render(&b, d)
	text := b.String()
	_, err := store.Modify[CacheDoc](s, "cache", CacheMigrator{}, func(c *CacheDoc) error {
		c.RenderedAt = &d.RenderedAt
		c.Board = &text
		c.Data = d
		return nil
	})
	return err
}

// Ambient is the at-most-once-per-window rule for new shells.
type Ambient struct{ Home string }

func (a Ambient) stamp() string { return filepath.Join(a.Home, ".ambient-stamp") }

// Run calls show when the window since the last showing has passed. When
// show reports nothing was shown (no board yet), the window does not
// restart, so the next shell tries again.
//
// Shells opened together take turns, so the board prints once.
func (a Ambient) Run(now time.Time, window time.Duration, show func() bool) {
	(&store.Store{Home: a.Home}).Locked("ambient", func() error {
		if st, err := os.Stat(a.stamp()); err == nil && now.Sub(st.ModTime()) < window {
			return nil
		}
		if show() {
			store.WriteFile(a.stamp(), nil, 0o644)
		}
		return nil
	})
}

// Summary is the saved board in one line, for an agent starting a session
// outside any project: how much waits, where, as of when, and how to see
// it now.
func Summary(d *Data, now time.Time) string {
	s := Classify(d)
	line := fmt.Sprintf("%d on you · %d on others · %d unfinished across %s", len(s.Me), len(s.Them), len(s.Unfinished), text.Plural(d.Checked, "project"))
	if len(s.Why) > 0 {
		line += " (" + strings.Join(s.Why, ", ") + ")"
	}
	return line + " · as of " + text.Ago(now, d.RenderedAt) + " · sous for the board"
}
