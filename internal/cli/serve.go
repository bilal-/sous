package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	ossignal "os/signal"
	"runtime"
	"slices"
	"strconv"
	"syscall"
	"time"

	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/serve"
	"github.com/bilal-/sous/internal/terminal"
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
	page := &serve.Server{Sous: serve.Exec{}, Exe: e.Exe, Token: token, Addr: addr}
	page.Terminal, page.Why, page.Open = serveTerminal(e)
	srv := &http.Server{Handler: page,
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

// serveTerminal is where sous serve opens a project's tab: the terminal
// setting, or the terminal sous serve was started in. Open refuses a path
// that is not a project sous finds in its folders.
func serveTerminal(e *Env) (title, why string, open func(context.Context, string, bool) error) {
	// Quietly: the page still serves when settings cannot be read, and
	// says why it has no terminal.
	if e.cfgErr != nil || e.Cfg == nil {
		return "", "sous config could not be read", nil
	}
	cfg := e.Cfg
	k, ok, why := terminal.Detect(cfg.Terminal, runtime.GOOS, os.Getenv)
	if !ok {
		return "", why, nil
	}
	return k.Title, "", func(ctx context.Context, path string, agent bool) error {
		ps, _ := project.Discover(cfg.Roots, cfg.Ignore, io.Discard)
		i := slices.IndexFunc(ps, func(p project.Project) bool { return p.Path == path })
		if i < 0 {
			return fmt.Errorf("%s is not a project sous found in your project folders", path)
		}
		t := terminal.Tab{Dir: path, Label: ps[i].Name}
		if agent {
			t.Type = "sous go ."
		}
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		return k.Open(ctx, terminal.Exec, os.Getenv, t)
	}
}
