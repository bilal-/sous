package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSous answers the calls a page makes, recording them.
type fakeSous struct {
	mu    sync.Mutex
	calls [][]string
	stdin []string
}

func (f *fakeSous) Run(_ context.Context, argv []string, stdin string) ([]byte, int, error) {
	f.mu.Lock()
	f.calls, f.stdin = append(f.calls, argv), append(f.stdin, stdin)
	f.mu.Unlock()
	switch {
	case contains(argv, "diff"):
		return []byte(`{"kind":"diff","v":1,"id":"4","base":"b","commit":"c","page":1,"pages":1,"patch":"+x"}`), 0, nil
	case contains(argv, "--op"):
		return []byte(`{"kind":"outcome","v":1,"op":"op-0123456789abcdef","action":"run","outcome":"done","id":"9"}`), 0, nil
	}
	return []byte(`{}`), 0, nil
}

func (f *fakeSous) Stream(ctx context.Context, argv []string, line func([]byte) error) error {
	line([]byte(`{"kind":"event","v":1,"provider":"sous","type":"snapshot","revision":"r1","changes":[]}`))
	<-ctx.Done()
	return nil
}

func contains(argv []string, s string) bool {
	for _, a := range argv {
		if a == s {
			return true
		}
	}
	return false
}

func newServer(t *testing.T) (*httptest.Server, *Server, *fakeSous) {
	f := &fakeSous{}
	s := &Server{Sous: f, Exe: "/bin/sous", Token: "t0k3n0123456789abcdef"}
	ts := httptest.NewServer(s)
	s.Addr = strings.TrimPrefix(ts.URL, "http://")
	t.Cleanup(ts.Close)
	return ts, s, f
}

// Only a browser that came through the printed address gets in: the token
// sets a cookie, any other request is refused, and a request naming
// another host (DNS rebinding) or posting from another origin is refused.
func TestServeGuardsTheDoor(t *testing.T) {
	ts, s, _ := newServer(t)
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if r, _ := noRedirect.Get(ts.URL + "/"); r.StatusCode != http.StatusForbidden {
		t.Fatalf("no token: %d", r.StatusCode)
	}
	r, _ := noRedirect.Get(ts.URL + "/?t=" + s.Token)
	if r.StatusCode != http.StatusSeeOther || len(r.Cookies()) == 0 || !r.Cookies()[0].HttpOnly {
		t.Fatalf("token: %d %v", r.StatusCode, r.Cookies())
	}
	cookie := r.Cookies()[0]
	get := func(path, host string) int {
		req, _ := http.NewRequest("GET", ts.URL+path, nil)
		req.AddCookie(cookie)
		if host != "" {
			req.Host = host
		}
		res, err := noRedirect.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res.StatusCode
	}
	if c := get("/", ""); c != 200 {
		t.Fatalf("page: %d", c)
	}
	if c := get("/", "evil.example.org"); c != http.StatusForbidden {
		t.Fatalf("another host: %d", c)
	}
	post := func(origin string) int {
		req, _ := http.NewRequest("POST", ts.URL+"/api/act", strings.NewReader(`{"action":"stop","op":"op-0123456789abcdef","target":"4"}`))
		req.AddCookie(cookie)
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", "application/json")
		res, _ := noRedirect.Do(req)
		return res.StatusCode
	}
	if c := post("http://evil.example.org"); c != http.StatusForbidden {
		t.Fatalf("other origin: %d", c)
	}
	if c := post(ts.URL); c != 200 {
		t.Fatalf("same origin: %d", c)
	}
}

func authed(t *testing.T, ts *httptest.Server, s *Server) *http.Client {
	jar := &cookieJar{}
	c := &http.Client{Jar: jar}
	if _, err := c.Get(ts.URL + "/?t=" + s.Token); err != nil {
		t.Fatal(err)
	}
	return c
}

type cookieJar struct {
	mu sync.Mutex
	c  []*http.Cookie
}

func (j *cookieJar) SetCookies(_ *url.URL, cs []*http.Cookie) {
	j.mu.Lock()
	j.c = append(j.c, cs...)
	j.mu.Unlock()
}
func (j *cookieJar) Cookies(*url.URL) []*http.Cookie { j.mu.Lock(); defer j.mu.Unlock(); return j.c }

// An action becomes exactly the argv sous advertises, with the op and the
// target filled in as whole arguments and the text on standard input; an
// action sous does not list is refused.
func TestServeRunsAdvertisedActions(t *testing.T) {
	ts, s, f := newServer(t)
	c := authed(t, ts, s)
	act := func(body string) (*http.Response, string) {
		req, _ := http.NewRequest("POST", ts.URL+"/api/act", strings.NewReader(body))
		req.Header.Set("Origin", ts.URL)
		res, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		return res, string(b)
	}
	res, body := act(`{"action":"run","op":"op-0123456789abcdef","target":"/code/acme/api","input":"fix it; rm -rf /"}`)
	if res.StatusCode != 200 || !strings.Contains(body, `"outcome":"done"`) {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	got := f.calls[len(f.calls)-1]
	want := "/bin/sous integration --protocol 1 --json run --op op-0123456789abcdef -- /code/acme/api"
	if strings.Join(got, " ") != want || f.stdin[len(f.stdin)-1] != "fix it; rm -rf /" {
		t.Fatalf("%q %q", got, f.stdin[len(f.stdin)-1])
	}
	if res, _ := act(`{"action":"run","op":"op-0123456789abcdef","target":"--json"}`); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("a target that looks like a flag: %d", res.StatusCode)
	}
	if res, _ := act(`{"action":"rm","op":"op-0123456789abcdef","target":"4"}`); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("unlisted: %d", res.StatusCode)
	}
	act(`{"action":"approve","op":"op-0123456789abcdef","target":"4","commit":"abc123"}`)
	if got := strings.Join(f.calls[len(f.calls)-1], " "); !strings.Contains(got, "approve --op op-0123456789abcdef --commit abc123 -- 4") {
		t.Fatal(got)
	}
	r, err := c.Get(ts.URL + "/api/diff?id=4&page=1")
	if err != nil || r.StatusCode != 200 {
		t.Fatalf("diff: %v %v", r, err)
	}
}

// The event stream relays sous's watch lines as server-sent events.
func TestServeStreamsEvents(t *testing.T) {
	ts, s, _ := newServer(t)
	c := authed(t, ts, s)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/events", nil)
	res, err := c.Do(req)
	if err != nil || res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("%v %v", res, err)
	}
	buf := make([]byte, 4096)
	n, _ := res.Body.Read(buf)
	line := string(buf[:n])
	if !strings.HasPrefix(line, "data: {") || !strings.Contains(line, `"type":"snapshot"`) {
		t.Fatalf("%q", line)
	}
	var tokens map[string]any
	r, _ := c.Get(ts.URL + "/tokens.json")
	if json.NewDecoder(r.Body).Decode(&tokens) != nil || tokens["role"] == nil {
		t.Fatal("tokens")
	}
	_ = fmt.Sprint
}

// The page's tokens are the shared ones, byte for byte.
func TestTokensAreTheSharedOnes(t *testing.T) {
	shared, err := os.ReadFile("../../integrations/design/tokens.json")
	if err != nil {
		t.Fatal(err)
	}
	if mine, _ := web.ReadFile("web/tokens.json"); string(mine) != string(shared) {
		t.Fatal("internal/serve/web/tokens.json differs from integrations/design/tokens.json; copy it over")
	}
}
