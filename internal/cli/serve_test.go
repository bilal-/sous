package cli

import (
	"bufio"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sous serve prints an address with its secret, keeps the same secret from
// one start to the next, and serves the page to that address only.
func TestServePrintsAWorkingAddress(t *testing.T) {
	f := fixture(t)
	start := func() (string, func()) {
		cmd := sousCmd(f, f.Home, "serve", "--port", "0")
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
