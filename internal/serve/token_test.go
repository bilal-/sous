package serve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTokenConcurrentCreationKeepsEveryAddressValid(t *testing.T) {
	home := filepath.Join(t.TempDir(), "private")
	start := make(chan struct{})
	type result struct {
		token string
		err   error
	}
	results := make(chan result, 64)
	for range 64 {
		go func() { <-start; token, err := Token(home); results <- result{token, err} }()
	}
	close(start)
	tokens := map[string]bool{}
	for range 64 {
		r := <-results
		if r.err != nil {
			t.Fatal(r.err)
		}
		tokens[r.token] = true
	}
	if len(tokens) != 1 {
		t.Fatalf("simultaneous starts received %d different secrets", len(tokens))
	}
	raw, err := os.ReadFile(filepath.Join(home, "serve.token"))
	if err != nil {
		t.Fatal(err)
	}
	if !tokens[strings.TrimSpace(string(raw))] {
		t.Fatal("returned secret differs from persisted secret")
	}
	for path, mode := range map[string]os.FileMode{home: 0700, filepath.Join(home, "serve.token"): 0600} {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != mode {
			t.Fatalf("%s permissions %o, want %o", path, st.Mode().Perm(), mode)
		}
	}
}

func TestTokenKeepsExistingBookmarksAndRepairsIncompleteSecret(t *testing.T) {
	for _, existing := range []string{strings.Repeat("a", 32), "  " + strings.Repeat("x", 32) + "\n", "incomplete"} {
		t.Run(existing[:3], func(t *testing.T) {
			home := t.TempDir()
			path := filepath.Join(home, "serve.token")
			if err := os.WriteFile(path, []byte(existing), 0600); err != nil {
				t.Fatal(err)
			}
			then := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			if err := os.Chtimes(path, then, then); err != nil {
				t.Fatal(err)
			}
			token, err := Token(home)
			if err != nil {
				t.Fatal(err)
			}
			if len(token) != 32 {
				t.Fatalf("secret length %d", len(token))
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(strings.TrimSpace(existing)) == 32 {
				st, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if token != strings.TrimSpace(existing) || string(raw) != existing || !st.ModTime().Equal(then) {
					t.Fatal("existing bookmark secret was rewritten")
				}
			} else if string(raw) != token+"\n" {
				t.Fatal("incomplete secret was not repaired")
			}
			again, err := Token(home)
			if err != nil || again != token {
				t.Fatalf("retry changed secret: %v", err)
			}
		})
	}
}

func TestTokenReadFailureDoesNotReplaceItsFile(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "serve.token")
	if err := os.Symlink("serve.token", path); err != nil {
		t.Fatal(err)
	}
	if _, err := Token(home); err == nil {
		t.Fatal("unreadable secret was replaced")
	}
	st, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode()&os.ModeSymlink == 0 {
		t.Fatal("read failure overwrote original file")
	}
}
