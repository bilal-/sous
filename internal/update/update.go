// Package update installs verified releases, leaving package managers and source
// checkouts in control of their own files. It never reads or migrates user data.
package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bilal-/sous/internal/plugin"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/text"
)

type Updater struct {
	Exe, Version, OS, Arch string
	API, Downloads         string
	HTTP                   *http.Client
	Env                    []string // supplied by the CLI, for Homebrew's noninteractive child
}

func New(exe, version string) *Updater {
	return &Updater{Exe: exe, Version: version,
		API:       "https://api.github.com/repos/bilal-/sous/releases/latest",
		Downloads: "https://github.com/bilal-/sous/releases/download",
		HTTP: &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" || len(via) >= 10 {
				return fmt.Errorf("unsafe or excessive release redirect")
			}
			return nil
		}},
	}
}

type Status struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Installed string `json:"installed"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	Available *bool  `json:"update_available"` // null when a source version cannot be compared
	Changed   bool   `json:"changed"`
}

type installation struct{ path, method, brew, source string }

func (u *Updater) installation() (installation, error) {
	p, err := filepath.EvalSymlinks(u.Exe)
	if err != nil {
		return installation{}, fmt.Errorf("locate sous: %w", err)
	}
	p, err = filepath.Abs(p)
	if err != nil {
		return installation{}, err
	}
	i := installation{path: p, method: "release"}
	// Homebrew's stable bin link resolves to Cellar/sous/<version>/bin/sous.
	versionDir := filepath.Dir(filepath.Dir(p))
	rack := filepath.Dir(versionDir)
	cellar := filepath.Dir(rack)
	if filepath.Base(p) == "sous" && filepath.Base(filepath.Dir(p)) == "bin" && filepath.Base(rack) == "sous" && filepath.Base(cellar) == "Cellar" {
		i.method, i.brew = "homebrew", filepath.Join(filepath.Dir(cellar), "bin", "brew")
		return i, nil
	}
	// A build at an exact tag can still be a checkout's bin/sous.
	root := filepath.Dir(filepath.Dir(p))
	mod, _ := os.ReadFile(filepath.Join(root, "go.mod"))
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil && strings.Contains(string(mod), "module github.com/bilal-/sous") && filepath.Base(filepath.Dir(p)) == "bin" {
		i.method, i.source = "source", root
	}
	if _, err := parseVersion(u.Version); err != nil {
		i.method = "source"
	}
	return i, nil
}

func sourceHint(i installation) string {
	if i.source != "" {
		return "update the checkout, then run make -C " + text.ShellArg(i.source) + " install"
	}
	return "update the checkout, then run make install"
}

func (u *Updater) Check(ctx context.Context) (Status, error) {
	i, err := u.installation()
	if err != nil {
		return Status{}, err
	}
	return u.check(ctx, i)
}

func (u *Updater) check(ctx context.Context, i installation) (Status, error) {
	s := Status{Current: canonical(u.Version), Installed: canonical(u.Version), Method: i.method, Path: u.Exe}
	data, err := u.get(ctx, u.API, 1<<20)
	if err != nil {
		return s, fmt.Errorf("check latest release: %w", err)
	}
	var release struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.Unmarshal(data, &release); err != nil {
		return s, fmt.Errorf("read latest release: %w", err)
	}
	latest, err := parseVersion(release.Tag)
	if err != nil || release.Draft || release.Prerelease {
		return s, fmt.Errorf("latest release is not a stable version: %q", release.Tag)
	}
	s.Latest = canonical(release.Tag)
	if current, err := parseVersion(u.Version); err == nil {
		available := current.less(latest)
		s.Available = &available
	}
	return s, nil
}

func (u *Updater) Apply(ctx context.Context) (Status, error) {
	i, err := u.installation()
	if err != nil {
		return Status{}, err
	}
	if i.method == "source" {
		return Status{}, fmt.Errorf("source build %s; %s", u.Version, sourceHint(i))
	}
	before, err := os.Stat(i.path)
	if err != nil {
		return Status{}, err
	}
	s, err := u.check(ctx, i)
	if err != nil {
		return s, err
	}
	if s.Available == nil {
		return s, fmt.Errorf("cannot compare installed version %q; update through %s", s.Current, s.Method)
	}
	if !*s.Available {
		return s, err
	}
	if i.method == "homebrew" {
		return u.brew(ctx, i, s)
	}
	if (u.OS != "darwin" && u.OS != "linux") || (u.Arch != "amd64" && u.Arch != "arm64") {
		return s, fmt.Errorf("no release download for %s/%s", u.OS, u.Arch)
	}
	if !before.Mode().IsRegular() {
		return s, fmt.Errorf("not a regular executable: %s", i.path)
	}
	// One updater per executable, including calls through different symlinks.
	// Refuse contention instead of waiting indefinitely for another process.
	lock, err := os.OpenFile(i.path+".update.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return s, fmt.Errorf("lock installation (check directory permissions): %w", err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return s, fmt.Errorf("another update is in progress; retry sous update: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	current, err := os.Stat(i.path)
	if err != nil || !os.SameFile(before, current) {
		return s, fmt.Errorf("installation changed during update; retry sous update")
	}
	name := fmt.Sprintf("sous_%s_%s_%s.tar.gz", strings.TrimPrefix(s.Latest, "v"), u.OS, u.Arch)
	base := u.Downloads + "/" + s.Latest + "/"
	checksums, err := u.get(ctx, base+"checksums.txt", 1<<20)
	if err != nil {
		return s, fmt.Errorf("download checksums: %w", err)
	}
	want, err := checksum(checksums, name)
	if err != nil {
		return s, err
	}
	data, err := u.get(ctx, base+name, 64<<20)
	if err != nil {
		return s, fmt.Errorf("download release: %w", err)
	}
	got := sha256.Sum256(data)
	if !bytes.Equal(got[:], want) {
		return s, fmt.Errorf("release checksum mismatch; installed binary unchanged")
	}
	binary, err := unpack(data)
	if err != nil {
		return s, err
	}
	old, err := os.ReadFile(i.path)
	if err != nil {
		return s, err
	}
	if bytes.Equal(old, binary) {
		s.Installed, *s.Available = s.Latest, false
		return s, nil
	}
	after, err := os.Stat(i.path)
	if err != nil || !os.SameFile(before, after) {
		return s, fmt.Errorf("installation changed during update; retry sous update")
	}
	if err := ctx.Err(); err != nil {
		return s, err
	}
	// Atomic replacement preserves permissions and the stable symlink. The
	// running process keeps its old inode; the next invocation runs the release.
	if err := store.WriteFile(i.path, binary, 0755); err != nil {
		return s, fmt.Errorf("replace binary (check directory permissions): %w", err)
	}
	s.Installed, *s.Available, s.Changed = s.Latest, false, true
	return s, nil
}

func (u *Updater) brew(ctx context.Context, i installation, s Status) (Status, error) {
	for _, args := range [][]string{{"update"}, {"upgrade", "bilal-/tap/sous"}} {
		cmd := plugin.GroupCommand(ctx, i.brew, args...)
		cmd.Env = u.Env
		out, err := cmd.CombinedOutput() // nil stdin: never ask the user for input
		if err != nil {
			return s, fmt.Errorf("update through Homebrew (%s) failed: %w: %s", args[0], err, text.Ellipsize(text.OneLine(string(out)), 500))
		}
	}
	// Homebrew can lag behind GitHub. Report what it actually installed.
	stable := filepath.Join(filepath.Dir(i.brew), "sous")
	cmd := plugin.GroupCommand(ctx, stable, "version")
	out, err := cmd.Output()
	if err != nil {
		return s, fmt.Errorf("update through Homebrew finished, but checking sous version failed: %w", err)
	}
	v := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(out)), "sous "))
	installed, err := parseVersion(v)
	if err != nil {
		return s, fmt.Errorf("update through Homebrew returned an invalid sous version: %q", v)
	}
	current, _ := parseVersion(s.Current)
	latest, _ := parseVersion(s.Latest)
	s.Installed, s.Changed, *s.Available = canonical(v), current.less(installed), installed.less(latest)
	if *s.Available {
		return s, fmt.Errorf("the Homebrew tap still has sous %s; latest release is %s; retry sous update when the tap publishes it", s.Installed, s.Latest)
	}
	return s, nil
}

func (u *Updater) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "sous-updater")
	resp, err := u.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from release server", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("release response exceeds %d bytes", limit)
	}
	return data, nil
}

func checksum(data []byte, name string) ([]byte, error) {
	var found []byte
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		digest, err := hex.DecodeString(fields[0])
		if err != nil || len(digest) != sha256.Size || found != nil {
			return nil, fmt.Errorf("invalid or duplicate checksum for %s", name)
		}
		found = digest
	}
	if found == nil {
		return nil, fmt.Errorf("no checksum for %s", name)
	}
	return found, nil
}

func unpack(data []byte) ([]byte, error) {
	g, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("read release archive: %w", err)
	}
	defer g.Close()
	r := tar.NewReader(io.LimitReader(g, 128<<20))
	var binary []byte
	for {
		h, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read release archive: %w", err)
		}
		if h.Name != "sous" {
			continue
		} // no other archive entry is written
		if binary != nil || h.Typeflag != tar.TypeReg || h.Size <= 0 || h.Size > 64<<20 {
			return nil, fmt.Errorf("release has an invalid sous binary")
		}
		binary, err = io.ReadAll(r)
		if err != nil {
			return nil, err
		}
	}
	if len(binary) == 0 {
		return nil, fmt.Errorf("release archive has no sous binary")
	}
	return binary, nil
}

type version [3]uint64

func parseVersion(s string) (version, error) {
	var v version
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) != 3 {
		return v, fmt.Errorf("invalid stable version")
	}
	for i, part := range parts {
		n, err := strconv.ParseUint(part, 10, 64)
		if err != nil || strconv.FormatUint(n, 10) != part {
			return v, fmt.Errorf("invalid stable version")
		}
		v[i] = n
	}
	return v, nil
}

func (v version) less(other version) bool {
	for i := range v {
		if v[i] != other[i] {
			return v[i] < other[i]
		}
	}
	return false
}

func canonical(s string) string {
	if _, err := parseVersion(s); err != nil {
		return s
	}
	return "v" + strings.TrimPrefix(s, "v")
}
