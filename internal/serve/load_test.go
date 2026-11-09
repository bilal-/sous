package serve

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBoardLoadIsExplicitAuthenticatedSingleFlightAndOwned(t *testing.T) {
	ts, s, _ := newServer(t)
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	s.LoadBoard = func(ctx context.Context) error {
		calls.Add(1)
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		s.Close(ctx)
	})
	client := authed(t, ts, s)
	res, err := client.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if calls.Load() != 0 {
		t.Fatal("opening the page built the cache")
	}
	request := func(origin string, auth bool) int {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/board-load", nil)
		req.Header.Set("Origin", origin)
		c := client
		if !auth {
			c = http.DefaultClient
		}
		r, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		return r.StatusCode
	}
	if request(ts.URL, false) != 403 || request("http://other.example", true) != 403 {
		t.Fatal("load admitted without authentication/origin")
	}
	if calls.Load() != 0 {
		t.Fatal("refused request started work")
	}
	if request(ts.URL, true) != 202 {
		t.Fatal("explicit load was not accepted")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("load did not start")
	}
	if request(ts.URL, true) != 202 || calls.Load() != 1 {
		t.Fatal("concurrent clicks started another build")
	}
	close(release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.WaitLoad(ctx); err != nil {
		t.Fatal(err)
	}
	r, err := client.Get(ts.URL + "/api/board-load")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var state struct {
		Loading bool
		Ready   bool
		Error   string
	}
	json.NewDecoder(r.Body).Decode(&state)
	if state.Loading || !state.Ready || state.Error != "" {
		t.Fatalf("load result: %+v", state)
	}
}

func TestBoardLoadCancellationAndFailureRemainVisible(t *testing.T) {
	ts, s, _ := newServer(t)
	started, stopped := make(chan struct{}), make(chan struct{})
	s.LoadBoard = func(ctx context.Context) error { close(started); <-ctx.Done(); close(stopped); return ctx.Err() }
	c := authed(t, ts, s)
	req, _ := http.NewRequest("POST", ts.URL+"/api/board-load", nil)
	req.Header.Set("Origin", ts.URL)
	r, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("not started")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	default:
		t.Fatal("work outlived server close")
	}
	r, err = c.Get(ts.URL + "/api/board-load")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var state struct {
		Loading bool
		Error   string
	}
	json.NewDecoder(r.Body).Decode(&state)
	if state.Loading || state.Error == "" {
		t.Fatalf("lost cancellation: %+v", state)
	}
}

func TestBoardLoadAcceptsEmptyChunkedProxyRequests(t *testing.T) {
	ts, s, _ := newServer(t)
	var calls atomic.Int32
	s.LoadBoard = func(context.Context) error { calls.Add(1); return nil }
	c := authed(t, ts, s)
	req, _ := http.NewRequest("POST", ts.URL+"/api/board-load", io.NopCloser(strings.NewReader("")))
	req.ContentLength = -1
	req.TransferEncoding = []string{"chunked"}
	req.Header.Set("Origin", ts.URL)
	r, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 202 {
		t.Fatalf("empty chunked POST refused: %d", r.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err = s.WaitLoad(ctx); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("load not called once")
	}
	req, _ = http.NewRequest("POST", ts.URL+"/api/board-load", strings.NewReader("x"))
	req.Header.Set("Origin", ts.URL)
	r, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 400 || calls.Load() != 1 {
		t.Fatal("nonempty request started another build")
	}
	if err = s.Close(ctx); err != nil {
		t.Fatal(err)
	}
}
