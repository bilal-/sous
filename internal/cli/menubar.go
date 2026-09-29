package cli

import (
	"fmt"
	"time"

	"github.com/bilal-/sous/internal/board"
	"github.com/bilal-/sous/internal/project"
)

// cmdMenubar renders SwiftBar output from the cache and nothing else. It
// never scans and never spawns: SwiftBar's environment is not the user's
// shell, and a refresh from there would poison the cache the shell prints.
func cmdMenubar(e *Env) int {
	c, err := board.ReadCache(e.store())
	if err != nil {
		fmt.Fprintf(e.Stdout, "⚑ –\n---\n%v\n", err)
		return 0
	}
	if c.Data == nil || c.RenderedAt == nil {
		fmt.Fprintln(e.Stdout, "⚑ –\n---\nno board yet, open a terminal")
		return 0
	}
	now := time.Now()
	board.RenderMenubar(e.Stdout, c.Data, e.Exe, project.Age(now, *c.RenderedAt), now.Sub(*c.RenderedAt) > e.Cfg.RefreshWindow())
	return 0
}
