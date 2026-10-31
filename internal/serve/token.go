package serve

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"github.com/bilal-/sous/internal/store"
)

// Token is the page's secret, made once and kept in SOUS_HOME so a
// bookmarked address keeps working.
func Token(home string) (string, error) {
	p := filepath.Join(home, "serve.token")
	if b, err := os.ReadFile(p); err == nil {
		if t := strings.TrimSpace(string(b)); len(t) == 32 {
			return t, nil
		}
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	t := hex.EncodeToString(b)
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", err
	}
	return t, store.WriteFile(p, []byte(t+"\n"), 0o600)
}
