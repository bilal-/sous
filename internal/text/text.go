// Package text is how sous writes things for people to read: ages, times,
// and lines cut to fit. It decides nothing about the data.
package text

import (
	"fmt"
	"time"
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

// Suffix is " · s", or nothing for an empty s.
func Suffix(s string) string {
	if s == "" {
		return ""
	}
	return " · " + s
}
