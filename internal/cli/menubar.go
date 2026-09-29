package cli

import (
	"fmt"
	"time"

	"github.com/bilal-/sous/internal/board"
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
	board.RenderMenubar(e.Stdout, c.Data, e.Exe, time.Now(), e.Cfg.RefreshWindow())
	return 0
}
