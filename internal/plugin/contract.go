package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// The exit codes every kind of plugin speaks, for every call.
const (
	ExitOK = 0
	// ExitFailed: the call failed, the reason on standard error. A silent
	// exit 1 is a plain "no" (ErrNo).
	ExitFailed = 1
	// ExitRefused: the request was not understood, or the call is an
	// optional one this plugin does not have.
	ExitRefused = 2
	// ExitNotSetUp: what the plugin needs is missing or logged out.
	ExitNotSetUp = 3
)

var (
	// ErrUnsupported: an optional call this plugin does not have.
	ErrUnsupported = errors.New("not supported")
	// ErrNotSetUp: the plugin cannot work on this machine yet; the reason
	// says what is missing.
	ErrNotSetUp = errors.New("not set up")
)

// Timeout bounds a call; StatusTimeout bounds a status call, which sous
// makes for many items at once while a person waits.
const (
	Timeout       = 15 * time.Second
	StatusTimeout = 3 * time.Second
)

// Exit is how a built in ends a call: nil is ExitOK, ErrUnsupported is
// ExitRefused, ErrNotSetUp is ExitNotSetUp, anything else ExitFailed. The
// reason goes to stderr, except for a silent ErrNo.
func Exit(err error, stderr io.Writer) int {
	switch {
	case err == nil:
		return ExitOK
	case errors.Is(err, ErrUnsupported):
		return ExitRefused
	case errors.Is(err, ErrNo):
		return ExitFailed
	}
	fmt.Fprintln(stderr, err)
	if errors.Is(err, ErrNotSetUp) {
		return ExitNotSetUp
	}
	return ExitFailed
}

// Request encodes a JSON request for a plugin's standard input, with no
// HTML escaping: notes are sent as the person wrote them.
func Request(v any) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		panic(fmt.Sprintf("plugin request %T: %v", v, err)) // only plain structs are sent
	}
	return b.Bytes()
}

// Usage refuses a call made with the wrong arguments.
func Usage(stderr io.Writer, usage string) int {
	fmt.Fprintln(stderr, "usage: "+usage)
	return ExitRefused
}

// RefCall is a built in's call made as `<call> <project> <ref>`; fn's
// error becomes the exit code (Exit).
func RefCall(call string, fn func(project, ref string, stdin io.Reader, stdout io.Writer) error) Op {
	return func(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
		if len(args) != 2 {
			return Usage(stderr, call+" <project> <ref>")
		}
		return Exit(fn(args[0], args[1], stdin, stdout), stderr)
	}
}
