// Package filing is the use-case layer between threads and backends: where
// a thread's project lives now, promoting a thread into its tracker, closing
// it upstream, and reconciling filed threads with what the tracker says.
// Nothing here parses arguments or prints; cli does that.
package filing

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bilal-/sous/internal/backend"
	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/thread"
)

// Filer holds what every filing operation needs. Build one per invocation.
type Filer struct {
	Store    *store.Store
	Cfg      *config.Config
	Backends []backend.Backend
	Warn     io.Writer // detect-probe warnings; nil = discard
	// Offline limits Ask to offline backends: the resume view (and so
	// the session hook and sous go) never touches the network. Remote
	// items keep no upstream state there; the board reconciles them.
	Offline bool

	once sync.Once
	ps   []project.Project
}

func (f *Filer) warn() io.Writer {
	if f.Warn == nil {
		return io.Discard
	}
	return f.Warn
}

// discovered runs project discovery at most once per Filer.
func (f *Filer) discovered() []project.Project {
	f.once.Do(func() {
		if len(f.Cfg.Roots) > 0 {
			f.ps, _ = project.Discover(f.Cfg.Roots, f.Cfg.Ignore, io.Discard)
		}
	})
	return f.ps
}

// CurrentPath finds where a thread's project lives now. A stored path that
// still is the same repo (same remote) is trusted without discovery; a
// moved repo is found by remote.
func (f *Filer) CurrentPath(th thread.Thread) string {
	if th.Remote == nil {
		return th.Project
	}
	if _, err := os.Stat(th.Project); err == nil && project.Remote(th.Project) == *th.Remote {
		return th.Project
	}
	for _, p := range f.discovered() {
		if p.Remote != nil && *p.Remote == *th.Remote {
			return p.Path
		}
	}
	return th.Project
}

func isGitFileCheckout(path string) bool {
	st, err := os.Lstat(filepath.Join(path, ".git"))
	return err == nil && !st.IsDir()
}

// ErrWorktree: filing into a worktree/submodule needs an explicit ask.
var ErrWorktree = errors.New("worktree or submodule")

// ErrNoTracker: nothing detected and nothing declared.
var ErrNoTracker = errors.New("no tracker")

// File promotes a local thread into its project's tracker. explicit allows
// worktrees. A thread whose marker is gone (status "unknown") is re-filed
// under a new ref; an already-filed thread returns its existing ref.
func (f *Filer) File(ctx context.Context, id int, explicit bool) (string, error) {
	th, err := thread.Get(f.Store, id)
	if err != nil {
		return "", err
	}
	path := f.CurrentPath(th)
	if !explicit && isGitFileCheckout(path) {
		return "", fmt.Errorf("%w: %s", ErrWorktree, path)
	}
	override := f.Cfg.Project(project.OrgName(path)).Backend
	b, err := backend.Detect(ctx, f.Backends, path, override, f.warn())
	if err != nil {
		return "", err
	}
	if b.Name == backend.Local.Name {
		return "", fmt.Errorf("%w for %s", ErrNoTracker, project.OrgName(path))
	}
	refile := false
	if th.Ref != nil {
		if ob, err := backend.ByRef(f.Backends, *th.Ref); err == nil {
			st, serr := backend.Status(ctx, ob, path, *th.Ref)
			refile = serr == nil && st == "unknown"
		}
	}
	ref, err := thread.FileAtomically(f.Store, id, refile, func(t thread.Thread) (string, error) {
		return backend.File(ctx, b, backend.Request{ID: t.ID, UID: t.UID, Legacy: t.Legacy, Project: path, Text: t.Text, Kind: string(t.Kind)})
	})
	if err != nil {
		if errors.Is(err, thread.ErrNotFound) {
			return "", err
		}
		return "", fmt.Errorf("%s: %w", b.Name, err)
	}
	return ref, nil
}

// CloseUpstream closes a filed thread in its tracker. The backend is chosen
// by the ref, never by re-detecting. An unfiled thread is a no-op.
func (f *Filer) CloseUpstream(ctx context.Context, id int) error {
	th, err := thread.Get(f.Store, id)
	if err != nil {
		return err
	}
	if th.Ref == nil {
		return nil
	}
	b, err := backend.ByRef(f.Backends, *th.Ref)
	if err != nil {
		return err
	}
	if err := backend.Close(ctx, b, f.CurrentPath(th), *th.Ref); err != nil {
		return fmt.Errorf("%s: %w", b.Name, err)
	}
	return nil
}

// Upstream is what a filed note's tracker said about it.
type Upstream struct {
	ID         int    // the note
	State, Err string // open | closed | unknown | error, and why for error
}

// Filed are the filed notes among views, copied: Ask works on these, so
// others (runs.Dispatcher.Refresh) may change the views meanwhile.
func Filed(views []thread.View) []thread.Thread {
	var out []thread.Thread
	for _, v := range views {
		if v.Ref != nil {
			out = append(out, v.Thread)
		}
	}
	return out
}

// Ask asks each note's backend for its state, concurrently under one
// deadline, and returns the answers; it changes nothing. A backend that
// fails or is missing is answered as such, never as "ref missing" and never
// as closed.
func (f *Filer) Ask(ctx context.Context, filed []thread.Thread) []Upstream {
	out := make([]Upstream, len(filed))
	var wg sync.WaitGroup
	for i, th := range filed {
		out[i].ID = th.ID
		wg.Add(1)
		go func(u *Upstream) {
			defer wg.Done()
			b, err := backend.ByRef(f.Backends, *th.Ref)
			if err != nil {
				u.State, u.Err = "error", fmt.Sprintf("backend %s not configured", strings.SplitN(*th.Ref, ":", 2)[0])
				return
			}
			if f.Offline && !b.Offline {
				return
			}
			st, err := backend.Status(ctx, b, f.CurrentPath(th), *th.Ref)
			if err != nil {
				u.State, u.Err = "error", fmt.Sprintf("%s: %v", b.Name, err)
				return
			}
			u.State = st
		}(&out[i])
	}
	wg.Wait()
	return out
}

// Settle puts Ask's answers on views, closes here what the tracker closed
// and drops it; a close that cannot be recorded stays, saying why.
func (f *Filer) Settle(views []thread.View, answers []Upstream, now time.Time) []thread.View {
	said := map[int]Upstream{}
	for _, a := range answers {
		// An offline Ask leaves a remote answer empty: it was not checked.
		// Keep the previous status and any uncertainty in that case.
		if a.State != "" {
			said[a.ID] = a
		}
	}
	for i := range views {
		if a, ok := said[views[i].ID]; ok {
			views[i].Upstream, views[i].UpstreamErr = a.State, a.Err
		}
	}
	out := views[:0]
	for _, v := range views {
		if v.Upstream == "closed" {
			err := thread.CloseUpstreamClosed(f.Store, v.ID, now)
			if err == nil {
				continue
			}
			// Closed there but not recorded here: keep it, and say why.
			v.Upstream, v.UpstreamErr = "error", "closed upstream, but could not be closed here: "+err.Error()
		}
		out = append(out, v)
	}
	return out
}
