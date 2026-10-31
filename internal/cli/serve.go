package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	ossignal "os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/bilal-/sous/internal/serve"
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
	token, err := serve.Token(e.Home)
	if err != nil {
		return fail(e, exitFailed, "the page's secret: %v", err)
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return fail(e, exitFailed, "listening on port %d: %v (another sous serve? try --port 0 for any free port)", port, err)
	}
	addr := ln.Addr().String()
	url := "http://" + addr + "/?t=" + token
	ctx, stop := ossignal.NotifyContext(e.ctx(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv := &http.Server{Handler: &serve.Server{Sous: serve.Exec{}, Exe: e.Exe, Token: token, Addr: addr},
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	if e.JSON {
		e.writeJSON(map[string]string{"url": url})
	} else {
		fmt.Fprintf(e.Stdout, "sous serve · %s\nOpen it in your browser; Ctrl-C stops it.\n", url)
	}
	if a.has("open") {
		openBrowser(url)
	}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := srv.Shutdown(sctx); err != nil {
			srv.Close()
		}
	}()
	err = srv.Serve(ln)
	stop()
	<-shutdownDone
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fail(e, exitFailed, "%v", err)
	}
	return 0
}

// openBrowser opens url in the person's browser, quietly failing.
var openBrowser = func(url string) {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	exec.Command(name, url).Start()
}
