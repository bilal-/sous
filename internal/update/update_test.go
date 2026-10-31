package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
)

func archive(t *testing.T, name, content string, kind byte) []byte {
	t.Helper()
	var b bytes.Buffer
	g := gzip.NewWriter(&b)
	w := tar.NewWriter(g)
	if kind != tar.TypeReg {
		content = ""
	}
	if err := w.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(content)), Typeflag: kind, Linkname: "/outside"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func fixture(t *testing.T, payload []byte, checksum string) (*Updater, *atomic.Int64) {
	t.Helper()
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/latest":
			fmt.Fprint(w, `{"tag_name":"v0.24.0","draft":false,"prerelease":false}`)
		case "/v0.24.0/checksums.txt":
			fmt.Fprintf(w, "%s  sous_0.24.0_darwin_arm64.tar.gz\n", checksum)
		case "/v0.24.0/sous_0.24.0_darwin_arm64.tar.gz":
			w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	exe := filepath.Join(dir, "sous")
	if err := os.WriteFile(exe, []byte("original binary"), 0755); err != nil {
		t.Fatal(err)
	}
	u := New(exe, "v0.23.0")
	u.API, u.Downloads, u.OS, u.Arch = srv.URL+"/latest", srv.URL, "darwin", "arm64"
	u.HTTP = srv.Client()
	return u, &requests
}

func TestFailedDownloadsLeaveBinaryUntouched(t *testing.T) {
	for _, tc := range []struct {
		name, entry string
		kind        byte
		badSum      bool
	}{
		{"checksum", "sous", tar.TypeReg, true},
		{"missing-binary", "README.md", tar.TypeReg, false},
		{"path-traversal", "../sous", tar.TypeReg, false},
		{"symlink", "sous", tar.TypeSymlink, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := archive(t, tc.entry, "new binary", tc.kind)
			sum := fmt.Sprintf("%x", sha256.Sum256(data))
			if tc.badSum {
				sum = strings.Repeat("0", 64)
			}
			u, _ := fixture(t, data, sum)
			if _, err := u.Apply(context.Background()); err == nil {
				t.Fatal("bad release accepted")
			}
			if got, _ := os.ReadFile(u.Exe); string(got) != "original binary" {
				t.Fatalf("binary changed: %q", got)
			}
		})
	}
}

func TestReleaseUpdateCheckRetryAndSymlink(t *testing.T) {
	data := archive(t, "sous", "new binary", tar.TypeReg)
	u, requests := fixture(t, data, fmt.Sprintf("%x", sha256.Sum256(data)))
	real := u.Exe
	u.Exe = filepath.Join(filepath.Dir(real), "linked-sous")
	if err := os.Symlink("sous", u.Exe); err != nil {
		t.Fatal(err)
	}
	status, err := u.Check(context.Background())
	if err != nil || status.Available == nil || !*status.Available || status.Method != "release" || requests.Load() != 1 {
		t.Fatalf("%+v %v requests=%d", status, err, requests.Load())
	}
	if got, _ := os.ReadFile(real); string(got) != "original binary" {
		t.Fatal("check changed the binary")
	}
	status, err = u.Apply(context.Background())
	if err != nil || !status.Changed || status.Installed != "v0.24.0" {
		t.Fatalf("%+v %v", status, err)
	}
	if got, _ := os.ReadFile(real); string(got) != "new binary" {
		t.Fatalf("not installed: %q", got)
	}
	if got, _ := os.Readlink(u.Exe); got != "sous" {
		t.Fatal("symlink replaced")
	}
	fi, _ := os.Stat(real)
	if fi.Mode().Perm() != 0755 {
		t.Fatal("executable permission lost")
	}
	// Even an already-running old process must not rewrite the same bytes.
	status, err = u.Apply(context.Background())
	if err != nil || status.Changed {
		t.Fatalf("retry: %+v %v", status, err)
	}
	again, _ := os.Stat(real)
	if !os.SameFile(fi, again) {
		t.Fatal("retry replaced the binary")
	}
	u.Version = "0.24.0" // release archives omit the leading v
	before := requests.Load()
	status, err = u.Apply(context.Background())
	if err != nil || status.Changed || requests.Load() != before+1 {
		t.Fatalf("up to date: %+v %v", status, err)
	}
	u.Version = "v0.25.0"
	status, err = u.Apply(context.Background())
	if err != nil || status.Changed || status.Available == nil || *status.Available {
		t.Fatalf("must not downgrade: %+v %v", status, err)
	}
}

func TestSourceBuildRefusesBeforeNetwork(t *testing.T) {
	for _, v := range []string{"dev", "v0.23.0-dirty", "v0.23.0-2-gabcdef"} {
		u, requests := fixture(t, nil, "")
		u.Version = v
		if _, err := u.Apply(context.Background()); err == nil || !strings.Contains(err.Error(), "source build") {
			t.Fatalf("%s: %v", v, err)
		}
		if requests.Load() != 0 {
			t.Fatal("source update used the network")
		}
	}
	u, requests := fixture(t, nil, "")
	root := filepath.Dir(u.Exe)
	os.Mkdir(filepath.Join(root, ".git"), 0755)
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/bilal-/sous\n"), 0600)
	os.Mkdir(filepath.Join(root, "bin"), 0755)
	u.Exe = filepath.Join(root, "bin", "sous")
	os.WriteFile(u.Exe, []byte("tagged source build"), 0755)
	if _, err := u.Apply(context.Background()); err == nil || !strings.Contains(err.Error(), "source build") {
		t.Fatalf("tagged source: %v", err)
	}
	if requests.Load() != 0 {
		t.Fatal("tagged checkout used the network")
	}
}

func TestFailedReleaseChecksDoNotClaimCurrent(t *testing.T) {
	for _, body := range []string{`{"tag_name":"v0.24.0","prerelease":true}`, `{"tag_name":"v0.24.0","draft":true}`, `{"tag_name":"../../other"}`, `{}`, `not JSON`, `HTTP failure`} {
		t.Run(body, func(t *testing.T) {
			u, _ := fixture(t, nil, "")
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if body == "HTTP failure" {
					w.WriteHeader(http.StatusServiceUnavailable)
				}
				fmt.Fprint(w, body)
			}))
			defer srv.Close()
			u.API = srv.URL
			if _, err := u.Check(context.Background()); err == nil {
				t.Fatal("unavailable or invalid latest release reported as current")
			}
			if _, err := u.Apply(context.Background()); err == nil {
				t.Fatal("invalid release accepted")
			}
			if got, _ := os.ReadFile(u.Exe); string(got) != "original binary" {
				t.Fatalf("binary changed: %q", got)
			}
		})
	}
}

func TestConcurrentUpdateIsRefusedWithoutWaiting(t *testing.T) {
	u, _ := fixture(t, nil, "")
	lock, err := os.OpenFile(u.Exe+".update.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if _, err := u.Apply(context.Background()); err == nil || !strings.Contains(err.Error(), "in progress") {
		t.Fatalf("concurrent update: %v", err)
	}
	if got, _ := os.ReadFile(u.Exe); string(got) != "original binary" {
		t.Fatalf("binary changed: %q", got)
	}
}

func TestInstallationReplacedDuringReleaseCheckIsPreserved(t *testing.T) {
	data := archive(t, "sous", "downloaded binary", tar.TypeReg)
	u, _ := fixture(t, data, fmt.Sprintf("%x", sha256.Sum256(data)))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		other := u.Exe + ".other"
		if err := os.WriteFile(other, []byte("newer installation"), 0755); err != nil {
			t.Error(err)
		}
		if err := os.Rename(other, u.Exe); err != nil {
			t.Error(err)
		}
		fmt.Fprint(w, `{"tag_name":"v0.24.0"}`)
	}))
	defer srv.Close()
	u.API = srv.URL
	if _, err := u.Apply(context.Background()); err == nil || !strings.Contains(err.Error(), "installation changed") {
		t.Fatalf("%v", err)
	}
	if got, _ := os.ReadFile(u.Exe); string(got) != "newer installation" {
		t.Fatalf("concurrent replacement lost: %q", got)
	}
}

func TestHomebrewOwnsItsUpgrade(t *testing.T) {
	for _, tc := range []struct {
		name, installed string
		fail            bool
	}{
		{"updated", "0.24.0", false}, {"tap_lags", "0.23.0", false}, {"brew_failed", "0.23.0", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u, _ := fixture(t, nil, "")
			prefix := filepath.Dir(u.Exe)
			real := filepath.Join(prefix, "Cellar", "sous", "0.23.0", "bin", "sous")
			os.MkdirAll(filepath.Dir(real), 0755)
			os.WriteFile(real, []byte("old cellar binary"), 0755)
			os.MkdirAll(filepath.Join(prefix, "bin"), 0755)
			u.Exe = filepath.Join(prefix, "bin", "sous")
			os.Symlink(real, u.Exe)
			log := filepath.Join(prefix, "brew.log")
			brew := "#!/bin/sh\necho \"$*\" >> '" + log + "'\n"
			if tc.fail {
				brew += "echo simulated failure >&2; exit 1\n"
			} else {
				brew += "if [ \"$1\" = upgrade ]; then\nrm '" + u.Exe + "'\nprintf '#!/bin/sh\\necho sous " + tc.installed + "\\n' > '" + u.Exe + "'\nchmod +x '" + u.Exe + "'\nfi\n"
			}
			os.WriteFile(filepath.Join(prefix, "bin", "brew"), []byte(brew), 0755)
			status, err := u.Apply(context.Background())
			switch {
			case tc.fail:
				if err == nil || !strings.Contains(err.Error(), "simulated failure") {
					t.Fatalf("failed brew: %+v %v", status, err)
				}
			case tc.installed == "0.23.0":
				if err == nil || !strings.Contains(err.Error(), "tap still has") || status.Changed || status.Installed != "v0.23.0" {
					t.Fatalf("tap lag: %+v %v", status, err)
				}
			default:
				if err != nil || !status.Changed || status.Method != "homebrew" || status.Installed != "v0.24.0" {
					t.Fatalf("%+v %v", status, err)
				}
			}
			got, _ := os.ReadFile(log)
			expected := "update\nupgrade bilal-/tap/sous\n"
			if tc.fail {
				expected = "update\n"
			}
			if string(got) != expected {
				t.Fatalf("%q", got)
			}
			got, _ = os.ReadFile(real)
			if string(got) != "old cellar binary" {
				t.Fatal("self updater overwrote Cellar")
			}
		})
	}
}
