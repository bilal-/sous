package text

import (
	"testing"
	"time"
)

func TestEllipsizeIsRuneSafe(t *testing.T) {
	for _, c := range []struct {
		in   string
		n    int
		want string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"eleven chars", 10, "eleven ch…"},
		{"ééééé", 3, "éé…"},
	} {
		if got := Ellipsize(c.in, c.n); got != c.want {
			t.Errorf("Ellipsize(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}

func TestEllipsizeTinyBudget(t *testing.T) {
	if Ellipsize("abc", 0) != "" || Ellipsize("abc", 1) != "…" {
		t.Fatal(Ellipsize("abc", 1))
	}
}

// "as of 09:02" is ambiguous for something days old: it says the day
// unless it is from today.
func TestAsOf(t *testing.T) {
	at := time.Date(2026, 9, 24, 9, 2, 0, 0, time.Local)
	for seen, want := range map[time.Duration]string{3 * time.Hour: "09:02", 72 * time.Hour: "Thu 24 Sep 09:02"} {
		if got := AsOf(at, at.Add(seen)); got != want {
			t.Errorf("seen after %v: %q", seen, got)
		}
	}
}

func TestAgeBuckets(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	cases := map[time.Duration]string{
		-time.Hour: "now", 0: "0m", 59 * time.Minute: "59m", time.Hour: "1h", 23 * time.Hour: "23h",
		24 * time.Hour: "1d", 29 * 24 * time.Hour: "29d", 30 * 24 * time.Hour: "1mo",
		364 * 24 * time.Hour: "12mo", 365 * 24 * time.Hour: "1y",
	}
	for d, want := range cases {
		if got := Age(now, now.Add(-d)); got != want {
			t.Errorf("Age(-%v)=%q want %q", d, got, want)
		}
	}
}

func TestAgo(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	if Ago(now, now.Add(time.Minute)) != "just now" || Ago(now, now.Add(-3*time.Hour)) != "3h ago" {
		t.Fatal(Ago(now, now.Add(time.Minute)), Ago(now, now.Add(-3*time.Hour)))
	}
}

func TestOneLineCutPlural(t *testing.T) {
	if got := OneLine("  fix\n the   index \t"); got != "fix the index" {
		t.Fatal(got)
	}
	if Cut("ééé", 2) != "éé" || Cut("ab", 5) != "ab" || Cut("ab", -1) != "" {
		t.Fatal(Cut("ééé", 2))
	}
	if Plural(1, "project") != "1 project" || Plural(0, "project") != "0 projects" {
		t.Fatal(Plural(0, "project"))
	}
}
