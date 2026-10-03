// Package docs carries the guides inside the sous program, so a command's
// --help says what docs/commands.md says, in the same words.
package docs

import _ "embed"

// Commands is docs/commands.md.
//
//go:embed commands.md
var Commands string
