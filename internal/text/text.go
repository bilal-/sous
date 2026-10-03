// Package text is how sous writes things for people to read: ages, times,
// and lines cut to fit. It decides nothing about the data.
package text

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// How much of a thing sous shows: a line of text (a note on the board's
// here view, an issue title, a session's last words), and a reason (a
// plugin's error, a line of a log).
const (
	LineRunes   = 120
	ReasonRunes = 200
)

// Age is how long ago t was, the way the board says it: 5m, 3h, 6d, 3mo, 1y.
func Age(now, t time.Time) string {
	s := now.Sub(t).Seconds()
	switch {
	case s < 0:
		return "now"
	case s < 3600:
		return fmt.Sprintf("%dm", int(s/60))
	case s < 86400:
		return fmt.Sprintf("%dh", int(s/3600))
	case s < 2592000:
		return fmt.Sprintf("%dd", int(s/86400))
	case s < 31536000:
		return fmt.Sprintf("%dmo", int(s/2592000))
	}
	return fmt.Sprintf("%dy", int(s/31536000))
}

// Ago is Age as a phrase: "3h ago", or "just now" (also for times a
// little in the future, from clock skew).
func Ago(now, t time.Time) string {
	if now.Sub(t) < time.Minute {
		return "just now"
	}
	return Age(now, t) + " ago"
}

// When is a moment in local time, with its day: "Mon 2 Jan 15:04".
func When(t time.Time) string { return t.Local().Format("Mon 2 Jan 15:04") }

// AsOf is the time of day for a moment today, and When for an older one,
// so something days old is never mistaken for this morning's.
func AsOf(t, now time.Time) string {
	lt, ln := t.Local(), now.Local()
	if lt.Year() == ln.Year() && lt.YearDay() == ln.YearDay() {
		return lt.Format("15:04")
	}
	return When(t)
}

// Ellipsize shortens s to at most n runes, ending in "…" when cut. Counting
// runes, not bytes, keeps a cut from splitting a character.
func Ellipsize(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 0 {
		return ""
	}
	return string(r[:n-1]) + "…"
}

// Fit is head, s and tail on one line of at most LineRunes, s cut to make
// room; head and tail are kept whole, and s keeps a few runes however
// long they are.
func Fit(head, s, tail string) string {
	room := LineRunes - utf8.RuneCountInString(head) - utf8.RuneCountInString(tail)
	return head + Ellipsize(s, max(room, 16)) + tail
}

// Suffix is " · s", or nothing for an empty s.
func Suffix(s string) string {
	if s == "" {
		return ""
	}
	return " · " + s
}

// OneLine collapses newlines and runs of spaces in s to single spaces.
func OneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// Cut shortens s to at most n runes, without a mark: for text stored or
// passed on, where the cut is not shown. Ellipsize is for text shown.
func Cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:max(n, 0)])
	}
	return s
}

// Plural is n and its noun: "1 project", "2 projects".
func Plural(n int, noun string) string {
	if n != 1 {
		noun += "s"
	}
	return fmt.Sprintf("%d %s", n, noun)
}
