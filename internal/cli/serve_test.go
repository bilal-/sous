package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"github.com/bilal-/sous/internal/board"
	"github.com/bilal-/sous/internal/store"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// sous serve prints an address with its secret, keeps the same secret from
// one start to the next, and serves the page to that address only.
func TestServePrintsAWorkingAddress(t *testing.T) {
	f := fixture(t)
	start := func() (string, func()) {
		cmd := sousCmd(f, f.Home, "serve", "--port", "0", "--page-api", "1")
		cmd.Stdin, cmd.Stderr = strings.NewReader(""), io.Discard
		out, _ := cmd.StdoutPipe()
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		line, err := bufio.NewReader(out).ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(strings.TrimPrefix(line, "sous serve · ")), func() { cmd.Process.Kill(); cmd.Wait() }
	}
	url, stop := start()
	defer stop()
	if !strings.HasPrefix(url, "http://127.0.0.1:") || !strings.Contains(url, "/?t=") {
		t.Fatalf("%q", url)
	}
	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar} // keeps the cookie the secret sets, as a browser does
	var res *http.Response
	var err error
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if res, err = browser.Get(url); err == nil {
			break
		}
	}
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("%v %v", res, err)
	}
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), "New task") {
		t.Fatalf("%.200s", body)
	}
	if r, _ := http.Get(strings.Split(url, "?")[0]); r.StatusCode != http.StatusForbidden {
		t.Fatalf("without the secret: %d", r.StatusCode)
	}
	b, _ := os.ReadFile(filepath.Join(f.SousHome, "serve.token"))
	if !strings.Contains(url, strings.TrimSpace(string(b))) {
		t.Fatal("the secret is kept in SOUS_HOME")
	}
	stop()
	again, stop2 := start()
	defer stop2()
	if strings.Split(again, "?")[1] != strings.Split(url, "?")[1] {
		t.Fatal("the secret changed between starts")
	}
	if _, _, code := f.run("serve", "--port", "nope"); code != exitUsage {
		t.Fatalf("%d", code)
	}
}

func TestServeLoadBoardBuildsTheFirstCacheOnlyAfterAnExplicitClick(t *testing.T) {
	f := fixture(t)
	marker := filepath.Join(f.Home, "tracker-called")
	f.bin("gh", "echo called >> '"+marker+"'; exit 1")
	cmd := sousCmd(f, f.Home, "serve", "--json", "--port", "0", "--page-api", "1")
	cmd.Stderr = io.Discard
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Signal(syscall.SIGTERM); cmd.Wait() }()
	var printed struct {
		URL string `json:"url"`
	}
	if err = json.NewDecoder(out).Decode(&printed); err != nil {
		t.Fatal(err)
	}
	root := strings.Split(printed.URL, "/?")[0]
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	r, err := client.Get(printed.URL)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", root+"/api/events", nil)
	events, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(events.Body).ReadString('\n')
	events.Body.Close()
	if err != nil || !strings.Contains(line, "unavailable") {
		t.Fatalf("fresh stream %q: %v", line, err)
	}
	st := &store.Store{Home: f.SousHome}
	before, err := board.ReadCache(st)
	if err != nil || before.Data != nil {
		t.Fatalf("cache on open: %+v, %v", before, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("page open queried a tracker")
	}
	post, _ := http.NewRequest("POST", root+"/api/board-load", nil)
	post.Header.Set("Origin", root)
	r, err = client.Do(post)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 202 {
		t.Fatalf("load status %d", r.StatusCode)
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		cache, err := board.ReadCache(st)
		if err != nil {
			t.Fatal(err)
		}
		if cache.Data != nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("explicit load did not publish a cache")
		case <-ticker.C:
		}
	}
}

func TestServeRejectsAnUnsupportedPageAPIWithoutStarting(t *testing.T) {
	f := fixture(t)
	_, text, code := f.run("serve", "--page-api", "2")
	if code != exitUsage || !strings.Contains(text, "update sous") {
		t.Fatalf("%d %s", code, text)
	}
	if _, err := os.Stat(filepath.Join(f.SousHome, "serve.token")); !os.IsNotExist(err) {
		t.Fatal("unsupported API changed state")
	}
}
