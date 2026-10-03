package session

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"

	"github.com/bilal-/sous/internal/store"
)

// WriteContext saves the resume summary handed to an agent started in
// project (by sous go, or a run), as SOUS_HERE_FILE: one file per project
// under home/here, replaced each time, so nothing piles up.
func WriteContext(home, project string, body []byte) (string, error) {
	sum := sha256.Sum256([]byte(project))
	name := filepath.Join(home, "here", hex.EncodeToString(sum[:6])+".txt")
	return name, store.WriteFile(name, body, 0o600)
}
