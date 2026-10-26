package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	ossignal "os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bilal-/sous/internal/serve"
	"github.com/bilal-/sous/internal/store"
)

// defaultServePort is where sous serve listens unless told otherwise, so
// its address stays the same from one day to the next.
const defaultServePort = 7428

// cmdServe: sous serve [--port <n>] [--open]. The board as a web page on
// this computer, until interrupted.
func cmdServe(e *Env, a argv) int {
	port := defaultServePort
	if p := a.value("port"); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 65535 {
			return fail(e, exitUsage, "--port takes a port number, not %q", p)
		}
		port = n
	}
	token, err := serveToken(e.Home)
	if err != nil {
		return fail(e, exitFailed, "the page's secret: %v", err)
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return fail(e, exitFailed, "listening on port %d: %v (another sous serve? try --port 0 for any free port)", port, err)
	}
	addr := ln.Addr().String()
	url := "http://" + addr + "/?t=" + token
	srv := &http.Server{Handler: &serve.Server{Sous: serve.Exec{}, Exe: e.Exe, Token: token, Addr: addr}, ReadHeaderTimeout: 10 * time.Second}
	if e.JSON {
		e.writeJSON(map[string]string{"url": url})
	} else {
		fmt.Fprintf(e.Stdout, "sous serve · %s\nOpen it in your browser; Ctrl-C stops it.\n", url)
	}
	if a.has("open") {
		openBrowser(url)
	}
	ctx, stop := ossignal.NotifyContext(e.ctx(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fail(e, exitFailed, "%v", err)
	}
	return 0
}

// serveToken is the page's secret, made once and kept in SOUS_HOME so a
// bookmarked address keeps working.
func serveToken(home string) (string, error) {
	p := filepath.Join(home, "serve.token")
	if b, err := os.ReadFile(p); err == nil {
		if t := strings.TrimSpace(string(b)); len(t) == 32 {
			return t, nil
		}
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	t := hex.EncodeToString(b)
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", err
	}
	return t, store.WriteFile(p, []byte(t+"\n"), 0o600)
}

// openBrowser opens url in the person's browser, quietly failing.
var openBrowser = func(url string) {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	exec.Command(name, url).Start()
}
