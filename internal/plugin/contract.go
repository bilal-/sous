package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
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

// Version is the contract version sous and its built ins speak.
const Version = 0

// Newer is the error for a message in a contract version newer than
// Version: who read it, and the version it carried.
func Newer(who string, v int) error {
	return fmt.Errorf("%s: contract v%d is newer than this one speaks (v%d)", who, v, Version)
}

// DecodeRequest reads a call's JSON request from stdin into req, and says
// whether to go on: a request that is not JSON, or fails valid, is refused
// with usage; one in a newer contract version is refused with Newer. v is
// the version the request carried.
func DecodeRequest(stdin io.Reader, req any, v func() int, valid func() bool, call, usage string, stderr io.Writer) (code int, ok bool) {
	if err := json.NewDecoder(stdin).Decode(req); err != nil || !valid() {
		return Usage(stderr, usage), false
	}
	if n := v(); n != Version {
		fmt.Fprintln(stderr, Newer(call, n))
		return ExitRefused, false
	}
	return ExitOK, true
}

// PrintCall is a built in's call made as `<call> <project> <ref>` that
// prints one answer: fn's string on success.
func PrintCall(call string, fn func(project, ref string) (string, error)) Op {
	return RefCall(call, func(project, ref string, _ io.Reader, stdout io.Writer) error {
		out, err := fn(project, ref)
		if err == nil {
			fmt.Fprintln(stdout, out)
		}
		return err
	})
}

// RefAnswer reads the answer to a call that makes something and prints its
// ref (backend file, runner start): refused on exit 2, and an error when it
// printed nothing.
func RefAnswer(name, call string, a Answer, err error) (string, error) {
	switch {
	case err != nil:
		return "", err
	case a.Code == ExitRefused:
		return "", Refused(name, call, a)
	case a.Out == "":
		return "", fmt.Errorf("%s %s: printed no ref", name, call)
	}
	return a.Out, nil
}

// Prefix is the start of every plugin program's name on axis: "sous-runner-".
func Prefix(axis string) string { return "sous-" + axis + "-" }

// Names is how a plugin's file is named, for messages: "sous-signal-,
// sous-backend-, sous-launcher- or sous-runner-".
func Names() string {
	var p []string
	for _, a := range axes {
		p = append(p, Prefix(a))
	}
	return strings.Join(p[:len(p)-1], ", ") + " or " + p[len(p)-1]
}
