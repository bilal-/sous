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
	"syscall"

	"github.com/bilal-/sous/internal/config"
)

// InstallID is a random id created once per $SOUS_HOME. It goes into remote
// tracker markers so thread ids (which restart at 1 per install) cannot
// collide across machines or people.
func InstallID(home string) (string, error) {
	p := filepath.Join(home, "install-id")
	if b, err := os.ReadFile(p); err == nil {
		// Every marker sous left in a tracker carries this id; replacing a
		// damaged one would orphan them all, so say so instead.
		id := strings.TrimSpace(string(b))
		if _, herr := hex.DecodeString(id); len(id) != 8 || herr != nil {
			return "", fmt.Errorf("%s is damaged (want 8 hex characters); restore it from a backup rather than deleting it", p)
		}
		return id, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
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
	tok, err := readToken(account)
	if err != nil {
		return "", err
	}
	tokenCache[account] = tok
	return tok, nil
}

// readToken asks gh for an account's token, one process at a time: after a
// gh upgrade each read can raise a macOS keychain prompt, and parallel
// status checks would stack them.
func readToken(account string) (string, error) {
	if f, err := os.OpenFile(filepath.Join(os.TempDir(), "sous-gh-token.lock"), os.O_CREATE|os.O_RDWR, 0o600); err == nil {
		defer f.Close()
		if syscall.Flock(int(f.Fd()), syscall.LOCK_EX) == nil {
			defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		}
	}
	out, err := exec.Command("gh", "auth", "token", "--user", account).Output()
	tok := strings.TrimSpace(string(out))
	if err != nil || tok == "" {
		return "", fmt.Errorf("no gh token for account %q (run: gh auth login, then gh auth status)", account)
	}
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
//
// A host with no dot is taken as an alias from ~/.ssh/config (Host gh-work,
// HostName github.com), so a remote like gh-work:acme/api counts as GitHub.
func ParseRemote(remote string) (host, path string) {
	host, path, ok := strings.Cut(remote, "/")
	if ok && !strings.Contains(host, ".") {
		host = sshAlias(host)
	}
	if !ok || !strings.Contains(host, ".") {
		return "", ""
	}
	return host, path
}

var (
	sshOnce    sync.Once
	sshAliases map[string]string
)

func resetSSHAliases() { sshOnce = sync.Once{} }

// sshAlias looks an alias up in ~/.ssh/config: the HostName of the first
// Host block whose pattern matches. The file is only read, never run (ssh
// -G would run Match exec lines), and read once per process.
func sshAlias(alias string) string {
	sshOnce.Do(func() { sshAliases = readSSHConfig() })
	for _, pat := range sshAliasOrder {
		if ok, _ := filepath.Match(pat, alias); ok {
			return sshAliases[pat]
		}
	}
	return alias
}

var sshAliasOrder []string

func readSSHConfig() map[string]string {
	sshAliasOrder = nil
	out := map[string]string{}
	home, err := os.UserHomeDir()
	if err != nil {
		return out
	}
	b, err := os.ReadFile(filepath.Join(home, ".ssh", "config"))
	if err != nil {
		return out
	}
	var patterns []string
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(strings.ReplaceAll(line, "=", " "))
		if len(fields) < 2 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "host":
			patterns = fields[1:]
		case "match":
			patterns = nil // conditions sous does not evaluate
		case "hostname":
			for _, p := range patterns {
				if _, seen := out[p]; !seen && !strings.HasPrefix(p, "!") {
					out[p] = strings.ToLower(fields[1])
					sshAliasOrder = append(sshAliasOrder, p)
				}
			}
		}
	}
	return out
}
