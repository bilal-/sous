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
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", err
	}
	var token string
	err := store.EditFile(p, 0o600, func(old []byte) ([]byte, error) {
		if token = strings.TrimSpace(string(old)); len(token) == 32 {
			return nil, nil
		}
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		token = hex.EncodeToString(b)
		return []byte(token + "\n"), nil
	})
	return token, err
}
