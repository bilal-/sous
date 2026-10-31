// Package serve is sous's board as a web page on this computer: a host of
// the native integration protocol, version 1, like the herdr plugin or
// Shellbell. It runs the same `sous integration` calls any host runs, so
// the page and the phone draw the same JSON. It listens on this machine
// only, and lets in only a browser that came through the address it
// printed.
package serve

import (
	"bufio"
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/integration"
	"github.com/bilal-/sous/internal/plugin"
)

//go:embed web
var web embed.FS

// Sous runs sous's integration calls: one answer, or a stream of lines.
type Sous interface {
	Run(ctx context.Context, argv []string, stdin string) (out []byte, code int, err error)
	Stream(ctx context.Context, argv []string, line func([]byte) error) error
}

// Server is the page and its API. Addr is where it listens (host:port),
// for the Host and Origin checks; Token is the secret in the address it
// printed.
type Server struct {
	Sous  Sous
	Exe   string
	Token string
	Addr  string
}

const cookieName = "sous"

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if !s.hostOK(r.Host) {
		http.Error(w, "sous serves this machine only", http.StatusForbidden)
		return
	}
	if r.URL.Path == "/" && r.URL.Query().Has("t") {
		if !s.secret(r.URL.Query().Get("t")) {
			http.Error(w, "that address is out of date; use the one sous serve printed", http.StatusForbidden)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: s.Token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if c, err := r.Cookie(cookieName); err != nil || !s.secret(c.Value) {
		http.Error(w, "open the address sous serve printed", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet && r.Header.Get("Origin") != "http://"+r.Host {
		http.Error(w, "refused: not from this page", http.StatusForbidden)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/":
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'")
		s.file(w, "web/index.html", "text/html; charset=utf-8")
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/") && static[r.URL.Path] != "":
		s.file(w, "web"+r.URL.Path, static[r.URL.Path])
	case r.Method == http.MethodGet && r.URL.Path == "/api/describe":
		s.answer(w, r, s.argv("--json"), "")
	case r.Method == http.MethodGet && r.URL.Path == "/api/events":
		s.events(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/api/diff":
		id, page := r.URL.Query().Get("id"), r.URL.Query().Get("page")
		if !number(id) || !number(page) {
			http.Error(w, "id and page are numbers", http.StatusBadRequest)
			return
		}
		s.answer(w, r, s.argv("--json", "diff", "--page", page, "--", id), "")
	case r.Method == http.MethodGet && r.URL.Path == "/api/show":
		id := r.URL.Query().Get("id")
		if !number(id) {
			http.Error(w, "id is a number", http.StatusBadRequest)
			return
		}
		s.answer(w, r, []string{s.Exe, "show", "--json", "--", id}, "")
	case r.Method == http.MethodPost && r.URL.Path == "/api/act":
		s.act(w, r)
	default:
		http.NotFound(w, r)
	}
}

// static is the page's other files and their types.
var static = map[string]string{
	"/app.js":      "text/javascript; charset=utf-8",
	"/app.css":     "text/css; charset=utf-8",
	"/tokens.json": "application/json",
	"/favicon.svg": "image/svg+xml",
}

func (s *Server) file(w http.ResponseWriter, name, typ string) {
	b, err := web.ReadFile(name)
	if err != nil {
		http.NotFound(w, nil)
		return
	}
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(b)
}

// hostOK: the request names this server, by the address it listens on or
// as localhost. Anything else is another site resolving to this machine.
func (s *Server) hostOK(host string) bool {
	_, port, err := net.SplitHostPort(s.Addr)
	if err != nil {
		return false
	}
	return host == s.Addr || host == "localhost:"+port || host == "127.0.0.1:"+port
}

func (s *Server) secret(v string) bool {
	return len(v) == len(s.Token) && subtle.ConstantTimeCompare([]byte(v), []byte(s.Token)) == 1
}

// argv is a version 1 integration call.
func (s *Server) argv(args ...string) []string {
	return append([]string{s.Exe, "integration", "--protocol", "1"}, args...)
}

// answer runs one call and sends what it printed, which is JSON whether it
// succeeded or not.
func (s *Server) answer(w http.ResponseWriter, r *http.Request, argv []string, stdin string) {
	out, _, err := s.Sous.Run(r.Context(), argv, stdin)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
}

// events relays sous's watch as server-sent events, one per line, with a
// comment now and then so idle connections stay open.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ctx, cancel := context.WithCancel(r.Context())
	lines := make(chan []byte, 16)
	finished := make(chan struct{})
	defer func() {
		cancel()
		<-finished // The watcher has reaped its child before this request ends.
	}()
	go func() {
		defer close(finished)
		defer close(lines)
		s.Sous.Stream(ctx, s.argv("--json", "watch"), func(b []byte) error {
			select {
			case lines <- append([]byte{}, b...):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	keep := time.NewTicker(15 * time.Second)
	defer keep.Stop()
	for {
		select {
		case b, ok := <-lines:
			if !ok {
				io.WriteString(w, "event: end\ndata: {}\n\n")
				fl.Flush()
				return
			}
			io.WriteString(w, "data: ")
			w.Write(b)
			io.WriteString(w, "\n\n")
			fl.Flush()
		case <-keep.C:
			io.WriteString(w, ": keep\n\n")
			fl.Flush()
		case <-ctx.Done():
			return
		}
	}
}

// actRequest is what the page asks for: an action sous lists, its op, its
// target (a note's number, or a project for run) and its parameters.
type actRequest struct {
	Action   string `json:"action"`
	Op       string `json:"op"`
	Target   string `json:"target"`
	Question string `json:"question,omitempty"`
	Commit   string `json:"commit,omitempty"`
	Input    string `json:"input,omitempty"`
}

// actable are the actions the page may run: those taking an op, and the
// private writes that need no terminal.
func actable(a integration.ActionV1) bool {
	return !a.Terminal && a.Effect != "read" && (a.Op || a.ID == "snooze" || a.ID == "done")
}

// act fills the advertised argv of the action asked for, each value a
// whole argument, and runs it.
func (s *Server) act(w http.ResponseWriter, r *http.Request) {
	var req actRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil || req.Target == "" || strings.HasPrefix(req.Target, "-") {
		http.Error(w, "an action names its target", http.StatusBadRequest)
		return
	}
	d := integration.DescribeV1("")
	i := slices.IndexFunc(d.Actions, func(a integration.ActionV1) bool { return a.ID == req.Action && actable(a) })
	if i < 0 {
		http.Error(w, "sous has no such action for this page", http.StatusBadRequest)
		return
	}
	a := d.Actions[i]
	values := map[string]string{"{op}": req.Op, "{id}": req.Target, "{project}": req.Target, "{question}": req.Question, "{commit}": req.Commit}
	argv := make([]string, 0, len(a.Argv))
	for j, arg := range a.Argv {
		switch v, ok := values[arg]; {
		case j == 0:
			argv = append(argv, s.Exe)
		case ok && v == "":
			http.Error(w, a.ID+" needs "+strings.Trim(arg, "{}"), http.StatusBadRequest)
			return
		case ok:
			argv = append(argv, v)
		default:
			argv = append(argv, arg)
		}
	}
	stdin := ""
	if a.Input == "stdin" {
		stdin = req.Input
	}
	s.answer(w, r, argv, stdin)
}

func number(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n > 0
}

// Exec runs sous as a program, each call in its own process group.
type Exec struct{}

func (Exec) Run(ctx context.Context, argv []string, stdin string) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := plugin.GroupCommand(ctx, argv[0], argv[1:]...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.Output()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return out, 0, nil
	case len(out) > 0 && errors.As(err, &ee):
		return out, ee.ExitCode(), nil // sous said why, in JSON
	}
	return nil, -1, err
}

func (Exec) Stream(ctx context.Context, argv []string, line func([]byte) error) error {
	cmd := plugin.GroupCommand(ctx, argv[0], argv[1:]...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(nil, 16<<20)
	for sc.Scan() {
		if err := line(sc.Bytes()); err != nil {
			break
		}
	}
	cmd.Process.Kill()
	return cmd.Wait()
}
