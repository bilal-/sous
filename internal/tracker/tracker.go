// Package tracker is the layer between sous and the tracker CLIs (gh, glab):
// which account or host a project belongs to, and non-interactive commands
// that use it. Tokens are fetched once per process and cached.
package tracker

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/bilal-/sous/internal/config"
)

// InstallID is a random id created once per $SOUS_HOME. It goes into remote
// tracker markers so thread ids (which restart at 1 per install) cannot
// collide across machines or people.
func InstallID(home string) (string, error) {
	p := filepath.Join(home, "install-id")
	if b, err := os.ReadFile(p); err == nil && len(strings.TrimSpace(string(b))) == 8 {
		return strings.TrimSpace(string(b)), nil
	}
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(buf[:])
	if err := os.MkdirAll(home, 0o755); err != nil {
		return "", err
	}
	return id, os.WriteFile(p, []byte(id+"\n"), 0o644)
}

func GitHubAccount(cfg *config.Config, orgName string) string {
	return cfg.Project(orgName).GitHubAccount
}

// GH builds a non-interactive gh command. With an account, the token for
// that account comes from gh's own keyring (`gh auth token --user`), which
// is how a multi-account user switches without touching gh's global state.
// A configured account whose token cannot be fetched is an error: running
// as whoever happens to be active would write under the wrong identity.
func GH(account string, args ...string) (*exec.Cmd, error) {
	cmd := exec.Command("gh", args...)
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1")
	if account != "" {
		tok, err := ghToken(account)
		if err != nil {
			return nil, err
		}
		cmd.Env = append(cmd.Env, "GH_TOKEN="+tok)
	}
	return cmd, nil
}

var (
	tokenMu    sync.Mutex
	tokenCache = map[string]string{}

	gitlabMu    sync.Mutex
	gitlabHosts []string // nil until first asked
)

// ResetCache forgets cached tokens and GitLab hosts. Tests that swap the gh fake need it;
// nothing else does.
func ResetCache() {
	tokenMu.Lock()
	defer tokenMu.Unlock()
	tokenCache = map[string]string{}
	gitlabMu.Lock()
	gitlabHosts = nil
	gitlabMu.Unlock()
}

// ghToken reads an account's token from gh's keyring once per process: a
// board with thirty filed threads must not open the keychain thirty times.
func ghToken(account string) (string, error) {
	tokenMu.Lock()
	defer tokenMu.Unlock()
	if tok, ok := tokenCache[account]; ok {
		return tok, nil
	}
	out, err := exec.Command("gh", "auth", "token", "--user", account).Output()
	tok := strings.TrimSpace(string(out))
	if err != nil || tok == "" {
		return "", fmt.Errorf("no gh token for account %q (run: gh auth login, then gh auth status)", account)
	}
	tokenCache[account] = tok
	return tok, nil
}

var hostLine = regexp.MustCompile(`(?m)^([a-z0-9.-]+\.[a-z]+)\s*$`)

// GitLabHosts: hosts glab is logged into (parsed from `glab auth status`)
// plus any declared in config. Sorted, deduped.
//
// glab's answer is asked once per process: every project's Locate consults
// it, and `glab auth status` may touch the network. glab not installed
// means config hosts only; glab failing without naming any host is an
// error (never "no GitLab", which would read as every finding resolved)
// and is asked again next time.
func GitLabHosts(cfg *config.Config) ([]string, error) {
	gitlabMu.Lock()
	defer gitlabMu.Unlock()
	if gitlabHosts == nil {
		found, err := glabHosts()
		if err != nil {
			return nil, err
		}
		gitlabHosts = found
	}
	seen := map[string]bool{}
	for _, h := range cfg.GitLabHosts {
		seen[h] = true
	}
	for _, h := range gitlabHosts {
		seen[h] = true
	}
	var hs []string
	for h := range seen {
		hs = append(hs, h)
	}
	sort.Strings(hs)
	return hs, nil
}

// glabHosts parses `glab auth status`. It exits non-zero when any host is
// logged out, so host lines in the output count as an answer either way.
func glabHosts() ([]string, error) {
	if _, err := exec.LookPath("glab"); err != nil {
		return []string{}, nil
	}
	out, err := GLab("", "auth", "status").CombinedOutput()
	found := []string{}
	for _, m := range hostLine.FindAllStringSubmatch(string(out), -1) {
		found = append(found, m[1])
	}
	// "No hosts are configured" is glab's (non-zero) way of saying none.
	if err != nil && len(found) == 0 && !strings.Contains(string(out), "No hosts are configured") {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return nil, fmt.Errorf("glab auth status: %s", msg)
		}
		return nil, fmt.Errorf("glab auth status: %w", err)
	}
	return found, nil
}

// GHRun runs gh as account and returns stdout only; see Output.
func GHRun(account string, args ...string) ([]byte, error) {
	cmd, err := GH(account, args...)
	if err != nil {
		return nil, err
	}
	return Output(cmd)
}

// GLabRun runs glab for a host and returns stdout only; see Output.
func GLabRun(host string, args ...string) ([]byte, error) {
	return Output(GLab(host, args...))
}

// Output runs a tracker CLI and returns stdout only. Both CLIs print update
// and deprecation notices on stderr next to valid JSON, so stderr is never
// parsed; it becomes the error message when the command fails.
func Output(cmd *exec.Cmd) ([]byte, error) {
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return out, errors.New(msg)
		}
		return out, err
	}
	return out, nil
}

// GLab builds a non-interactive glab command for a host.
func GLab(host string, args ...string) *exec.Cmd {
	if host != "" {
		args = append([]string{"--hostname", host}, args...)
	}
	cmd := exec.Command("glab", args...)
	cmd.Env = append(os.Environ(), "GLAB_NO_PROMPT=1", "GLAB_SEND_TELEMETRY=0")
	return cmd
}

// ParseRemote splits "host/path/to/repo" as produced by project.Remote.
func ParseRemote(remote string) (host, path string) {
	host, path, ok := strings.Cut(remote, "/")
	if !ok || !strings.Contains(host, ".") {
		return "", ""
	}
	return host, path
}
