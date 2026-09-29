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
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

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
	cmd.Env = childEnv("GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1")
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
	if unlock := tokenLock(); unlock != nil {
		defer unlock()
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
	if GLabInstalled() != nil {
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
	cmd.Env = childEnv("GLAB_NO_PROMPT=1", "GLAB_SEND_TELEMETRY=0")
	return cmd
}

// ParseRemote splits "host/path/to/repo" as produced by project.Remote.
//
// A host that ~/.ssh/config maps to another (Host gh-work, HostName
// github.com) is read as that host, so gh-work:acme/api counts as GitHub.
func ParseRemote(remote string) (host, path string) {
	host, path, ok := strings.Cut(remote, "/")
	if !ok {
		return "", ""
	}
	host = sshAlias(host)
	if !strings.Contains(host, ".") {
		return "", ""
	}
	return host, path
}

// GitHubHost is GitHub's host, the one tracker whose host is fixed.
const GitHubHost = "github.com"

var (
	envMu    sync.Mutex
	userHome string   // set by Init from cli; "" reads no ssh config
	cacheDir string   // for the gh token lock
	baseEnv  []string // the environment gh and glab run with
	sshOnce  sync.Once
	sshHosts []sshBlock
)

// Init tells tracker the person's home and cache folders and the
// environment gh and glab should run with. cli calls it once; tracker reads
// none of these itself.
func Init(home, cache string, env []string) {
	envMu.Lock()
	defer envMu.Unlock()
	userHome, cacheDir, baseEnv = home, cache, env
	sshOnce = sync.Once{}
}

// childEnv is the environment for gh and glab, plus extra settings.
func childEnv(extra ...string) []string {
	envMu.Lock()
	defer envMu.Unlock()
	return append(slices.Clone(baseEnv), extra...)
}

func homeAndCache() (string, string) {
	envMu.Lock()
	defer envMu.Unlock()
	return userHome, cacheDir
}

// sshBlock is one Host block that sets a HostName.
type sshBlock struct {
	patterns []string // "!pat" excludes
	hostname string
}

func (b sshBlock) matches(alias string) bool {
	matched := false
	for _, p := range b.patterns {
		neg := strings.HasPrefix(p, "!")
		if ok, _ := filepath.Match(strings.TrimPrefix(p, "!"), alias); ok {
			if neg {
				return false
			}
			matched = true
		}
	}
	return matched
}

// sshAlias is the host an ssh remote's host name really means, from
// ~/.ssh/config, read (never run: ssh -G would run Match exec lines) once
// per Init. A name without a dot is always looked up. A name with a dot is
// only renamed by a Host entry naming it exactly (Host github.com-work),
// never by a wildcard, and a tracker's own host is never renamed (Host
// gitlab.com, HostName altssh.gitlab.com is a port trick, not a new host).
func sshAlias(host string) string {
	home, _ := homeAndCache()
	sshOnce.Do(func() { sshHosts = readSSHConfig(filepath.Join(home, ".ssh", "config"), home) })
	alias := strings.ToLower(host)
	dotted := strings.Contains(alias, ".")
	if alias == GitHubHost || dotted && strings.Contains(alias, "gitlab") {
		return alias
	}
	for _, b := range sshHosts {
		if b.matches(alias) && (!dotted || slices.Contains(b.patterns, alias)) {
			return strings.ReplaceAll(b.hostname, "%h", alias)
		}
	}
	return alias
}

// readSSHConfig reads the Host blocks that set a HostName, in order.
// Include lines are read in place, as part of the block they sit in; Match
// blocks are skipped, since their conditions may run commands.
func readSSHConfig(path, home string) []sshBlock {
	if home == "" {
		return nil
	}
	r := &sshReader{home: home}
	r.read(path, 0)
	return r.out
}

type sshReader struct {
	home     string
	out      []sshBlock
	patterns []string // the current Host block's; nil inside Match
	hostSet  bool     // the current block already gave a HostName
}

func (r *sshReader) read(path string, depth int) {
	b, err := os.ReadFile(path)
	if err != nil || depth > 8 {
		return
	}
	for _, line := range strings.Split(string(b), "\n") {
		key, args := sshLine(line)
		switch key {
		case "host":
			r.patterns, r.hostSet = args, false
			for i := range r.patterns {
				r.patterns[i] = strings.ToLower(r.patterns[i])
			}
		case "match":
			r.patterns = nil
		case "hostname":
			if len(r.patterns) > 0 && len(args) > 0 && !r.hostSet {
				r.out = append(r.out, sshBlock{patterns: r.patterns, hostname: strings.ToLower(args[0])})
				r.hostSet = true
			}
		case "include":
			if depth == 0 || r.patterns != nil { // skipped inside Match
				for _, f := range sshIncludes(args, r.home) {
					r.read(f, depth+1)
				}
			}
		}
	}
}

// sshIncludes are the files an Include line names: globs, relative to
// ~/.ssh unless absolute or under ~.
func sshIncludes(args []string, home string) []string {
	var files []string
	for _, inc := range args {
		if !filepath.IsAbs(inc) && !strings.HasPrefix(inc, "~") {
			inc = filepath.Join(home, ".ssh", inc)
		}
		matches, _ := filepath.Glob(strings.Replace(inc, "~", home, 1))
		files = append(files, matches...)
	}
	return files
}

// sshLine splits one ssh config line into its lowercased keyword and its
// arguments, with quotes removed. "Key=value" is allowed.
func sshLine(line string) (string, []string) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", nil
	}
	cut := strings.IndexFunc(line, func(r rune) bool { return r == ' ' || r == '\t' || r == '=' })
	if cut < 0 {
		return strings.ToLower(line), nil
	}
	rest := strings.TrimLeft(line[cut:], " \t=")
	var args []string
	for _, f := range strings.Fields(rest) {
		args = append(args, strings.Trim(f, `"`))
	}
	return strings.ToLower(line[:cut]), args
}

// tokenLock takes a per-user lock around gh token reads, waiting at most a
// minute (long enough to answer a keychain prompt). It lives in the user's
// cache folder, not the shared temp folder. nil means no lock was taken,
// and the read goes ahead anyway.
func tokenLock() func() {
	_, cache := homeAndCache()
	if cache == "" {
		return nil
	}
	dir := filepath.Join(cache, "sous")
	if os.MkdirAll(dir, 0o700) != nil {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(dir, "gh-token.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil
	}
	for deadline := time.Now().Add(time.Minute); ; time.Sleep(50 * time.Millisecond) {
		if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil {
			return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil
		}
	}
}

// GHReady says why gh cannot be used as account ("" for gh's active
// account): not installed, or not logged in. nil when it can.
func GHReady(account string) error {
	if err := installed("gh"); err != nil {
		return err
	}
	if _, err := GHRun(account, "auth", "status"); err != nil {
		if account == "" && loggedOut(err) {
			return errors.New("not logged in (run gh auth login)")
		}
		return err // the tool's own words: a keyring or network problem, or which account
	}
	return nil
}

// GLabReady says why glab cannot be used for host: not installed, or not
// logged in there. nil when it can.
func GLabReady(host string) error {
	if err := installed("glab"); err != nil {
		return err
	}
	if _, err := GLabRun(host, "auth", "status"); err != nil {
		if loggedOut(err) {
			return fmt.Errorf("not logged in to %s (run glab auth login --hostname %s)", host, host)
		}
		return fmt.Errorf("%s: %w", host, err)
	}
	return nil
}

// loggedOut: the tool's error says there is no login, as opposed to any
// other failure worth showing as it is.
func loggedOut(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not logged in") || strings.Contains(msg, "not authenticated") ||
		strings.Contains(msg, "has not been authenticated") || strings.Contains(msg, "no hosts are configured")
}

// GHInstalled and GLabInstalled: is the tool on PATH?
func GHInstalled() error   { return installed("gh") }
func GLabInstalled() error { return installed("glab") }

// installed: is tool on PATH? The error says it is not.
func installed(tool string) error {
	if _, err := exec.LookPath(tool); err != nil {
		return errors.New(tool + " not installed")
	}
	return nil
}
