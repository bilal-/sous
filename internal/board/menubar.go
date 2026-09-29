package board

import (
	"fmt"
	"io"
	"strings"

	"github.com/bilal-/sous/internal/project"
)

// sanitize keeps SwiftBar from reading a row as a separator/submenu/param.
func sanitize(s string) string {
	s = strings.ReplaceAll(s, "|", "/")
	for strings.HasPrefix(s, "-") {
		s = "‑" + s[1:] // non-breaking hyphen
	}
	return s
}

// RenderMenubar emits SwiftBar's line format from cached data. The title is
// the on-you count; a trailing ? means some of the picture is missing and the
// first dropdown line says which part.
func RenderMenubar(w io.Writer, d *Data, exe, cacheAge string, cacheStale bool) {
	s := Classify(d)
	why := s.Why
	if cacheStale {
		why = append(why, "cache "+cacheAge+" old")
	}
	mark := ""
	if len(why) > 0 {
		mark = "?"
	}
	fmt.Fprintf(w, "⚑ %d%s\n---\n", len(s.Me), mark)
	if len(why) > 0 {
		fmt.Fprintf(w, "%s | color=orange\n", strings.Join(why, " · "))
	}
	for _, r := range s.Me {
		fmt.Fprintf(w, "%s · %s · %s | bash=%s param1=go param2=%s terminal=true\n", sanitize(r.Text+r.upstreamNote()), project.OrgName(r.Project), r.Age, exe, project.OrgName(r.Project))
	}
	if len(s.Unfinished) > 0 {
		fmt.Fprintln(w, "---")
		fmt.Fprintln(w, "unfinished")
		for _, r := range s.Unfinished {
			fmt.Fprintf(w, "-- %s · %s · %s\n", sanitize(r.Text), project.OrgName(r.Project), r.Age)
		}
	}
	fmt.Fprintln(w, "---")
	fmt.Fprintf(w, "%d checked · as of %s · cached %s ago\n", d.Checked, asOf(d.RenderedAt), cacheAge)
}
