package board

import (
	"context"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/effort"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/testutil"
	"github.com/bilal-/sous/internal/thread"
)

func effortRefreshFixture(t *testing.T) (Inputs, []project.Project, []thread.View) {
	t.Helper()
	s := &store.Store{Home: t.TempDir()}
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Roots: []string{root}}
	ps := []project.Project{}
	ids := []int{}
	for _, name := range []string{"api", "web"} {
		p := project.Project{Path: testutil.Repo(t, filepath.Join(root, name), true, "")}
		id, _, err := thread.Note(s, p, thread.Me, "Review "+name, "human", now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := thread.FileAtomically(s, id, false, func(thread.Thread) (string, error) { return "github:acme/" + name + "#12", nil }); err != nil {
			t.Fatal(err)
		}
		ps, ids = append(ps, p), append(ids, id)
	}
	e, _, err := effort.Create(s, "Launch", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := effort.Link(s, cfg, e.ID, effort.Changes{Notes: ids}, false); err != nil {
		t.Fatal(err)
	}
	views, err := thread.Open(s, now)
	if err != nil {
		t.Fatal(err)
	}
	return Inputs{Store: s, Cfg: cfg, Now: now, Warn: io.Discard}, ps, views
}

func TestBuildEffortsUseThisRefreshBeforeScoping(t *testing.T) {
	for _, tc := range []struct {
		name, cached, current string
		unavailable           int
	}{
		{"first read", "", "open", 0},
		{"recovered", "error", "open", 0},
		{"new failure", "open", "error", 2},
		{"missing refs", "open", "unknown", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, ps, views := effortRefreshFixture(t)
			if tc.cached != "" {
				for i := range views {
					views[i].Upstream = tc.cached
				}
				if err := WriteCache(in.Store, &Data{Threads: views, RenderedAt: in.Now.Add(-time.Hour)}); err != nil {
					t.Fatal(err)
				}
			}
			in.Roots = []string{ps[0].Path}
			in.Reconcile = func(_ context.Context, vs []thread.View) []thread.View {
				for i := range vs {
					vs[i].Upstream = tc.current
					vs[i].UpstreamErr = "current answer"
				}
				return vs
			}
			d, err := Build(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			if len(d.Threads) != 1 || d.Threads[0].Upstream != tc.current {
				t.Fatalf("scoped threads: %+v", d.Threads)
			}
			if len(d.Efforts) != 1 || d.Efforts[0].Open != 2 || d.Efforts[0].Unavailable != tc.unavailable {
				t.Fatalf("efforts: %+v", d.Efforts)
			}
		})
	}
}

func TestHereEffortsCombineLocalAnswersWithCachedRemoteAnswers(t *testing.T) {
	for _, answer := range []string{"", "open", "error"} {
		t.Run("local_"+answer, func(t *testing.T) {
			in, ps, views := effortRefreshFixture(t)
			for i := range views {
				views[i].Upstream = "open"
			}
			cachedAt := in.Now.Add(-time.Hour)
			if err := WriteCache(in.Store, &Data{Threads: views, RenderedAt: cachedAt}); err != nil {
				t.Fatal(err)
			}
			in.Reconcile = func(_ context.Context, vs []thread.View) []thread.View {
				if len(vs) != 1 {
					t.Fatalf("here checked outside project: %+v", vs)
				}
				vs[0].Upstream, vs[0].UpstreamErr = answer, "local answer"
				return vs
			}
			d, err := BuildHere(context.Background(), in, ps[0].Path)
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if answer == "error" {
				want = 1
			}
			if len(d.Context.Efforts) != 1 || d.Context.Efforts[0].Open != 2 || d.Context.Efforts[0].Unavailable != want {
				t.Fatalf("efforts: %+v", d.Context.Efforts)
			}
			cache, err := ReadCache(in.Store)
			if err != nil || cache.RenderedAt == nil || !cache.RenderedAt.Equal(cachedAt) {
				t.Fatalf("offline projection changed cache age: %+v %v", cache, err)
			}
		})
	}
}

func TestBuildEffortsSeeUpstreamCloseAfterCachedFailure(t *testing.T) {
	in, _, views := effortRefreshFixture(t)
	for i := range views {
		views[i].Upstream, views[i].UpstreamErr = "error", "old failure"
	}
	if err := WriteCache(in.Store, &Data{Threads: views, RenderedAt: in.Now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	in.Reconcile = func(_ context.Context, vs []thread.View) []thread.View {
		for _, v := range vs {
			if err := thread.CloseUpstreamClosed(in.Store, v.ID, in.Now); err != nil {
				t.Fatal(err)
			}
		}
		return []thread.View{}
	}
	d, err := Build(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Threads) != 0 || len(d.Efforts) != 1 || d.Efforts[0].Completed != 2 || d.Efforts[0].Open != 0 || d.Efforts[0].Unavailable != 0 {
		t.Fatalf("after close: %+v", d)
	}
}

func TestEffortCurrentObservationsRespectIdentityAndCacheAge(t *testing.T) {
	for _, mismatch := range []string{"none", "uid", "id", "ref", "missing ref", "unobserved"} {
		t.Run(mismatch, func(t *testing.T) {
			in, _, views := effortRefreshFixture(t)
			for i := range views {
				views[i].Upstream = "error"
				views[i].UpstreamErr = "cached failure"
			}
			cachedAt := in.Now.Add(-time.Hour)
			if err := WriteCache(in.Store, &Data{Threads: views, RenderedAt: cachedAt}); err != nil {
				t.Fatal(err)
			}
			current := views[0]
			current.Upstream, current.UpstreamErr = "open", ""
			switch mismatch {
			case "uid":
				current.UID = "other-note"
			case "id":
				current.ID = 999
			case "ref":
				ref := "github:acme/api#99"
				current.Ref = &ref
			case "missing ref":
				current.Ref = nil
			case "unobserved":
				current.Upstream = ""
			}
			groups, err := effort.All(in.Store)
			if err != nil {
				t.Fatal(err)
			}
			got := describeEfforts(in.Store, groups, in.Now, []thread.View{current})[0]
			want := 2
			if mismatch == "none" {
				want = 1
			}
			if got.Unavailable != want || got.AsOf == nil || !got.AsOf.Equal(cachedAt) {
				t.Fatalf("projection: %+v", got)
			}
			for i, item := range got.Items {
				if mismatch == "none" && i == 0 {
					continue
				}
				if item.Upstream == nil || item.Upstream.Error != "cached failure" {
					t.Fatalf("lost cached failure on item %d: %+v", i, item)
				}
			}
		})
	}
}
