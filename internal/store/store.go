// Package store keeps versioned JSON documents under SOUS_HOME. Every write is
// temp + rename under an exclusive flock; every read migrates forward.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

var ErrNewer = errors.New("file was written by a newer sous")

type Store struct{ Home string }

// Migrator describes one document's schema: its empty form, its current
// version, and how to step an older version forward by one. Each document
// versions independently.
type Migrator interface {
	Empty() []byte
	Current() int
	Migrate(from int, raw []byte) ([]byte, error)
}

func (s *Store) path(name string) string { return filepath.Join(s.Home, name+".json") }

func (s *Store) lock(name string) (func(), error) {
	if err := os.MkdirAll(s.Home, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(s.Home, name+".lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}

func version(raw []byte) (int, error) {
	var v struct {
		Version *int `json:"version"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, err
	}
	if v.Version == nil {
		return 0, nil
	}
	return *v.Version, nil
}

// readNoLock returns migrated bytes and whether a migration ran.
func (s *Store) readNoLock(name string, m Migrator) ([]byte, bool, error) {
	raw, err := os.ReadFile(s.path(name))
	if os.IsNotExist(err) {
		return m.Empty(), false, nil
	}
	if err != nil {
		return nil, false, err
	}
	v, err := version(raw)
	if err != nil {
		return nil, false, fmt.Errorf("%s.json: %w", name, err)
	}
	current := m.Current()
	if v > current {
		return nil, false, fmt.Errorf("%s.json is version %d; this sous understands %d: %w", name, v, current, ErrNewer)
	}
	migrated := false
	for v < current {
		raw, err = m.Migrate(v, raw)
		if err != nil {
			return nil, false, fmt.Errorf("migrating %s.json from v%d: %w", name, v, err)
		}
		v++
		migrated = true
	}
	return raw, migrated, nil
}

func (s *Store) writeNoLock(name string, raw []byte) error {
	if !json.Valid(raw) {
		return fmt.Errorf("refusing to write invalid JSON to %s.json", name)
	}
	return replace(s.path(name), raw, 0o600) // notes and sessions are private
}

// Read returns the migrated document. The common case (already current) is
// read without the lock. An old file is upgraded only under the lock, from a
// fresh read, so a write another process made meanwhile is never lost.
func (s *Store) Read(name string, m Migrator) ([]byte, error) {
	raw, err := os.ReadFile(s.path(name))
	if os.IsNotExist(err) {
		return m.Empty(), nil
	}
	if err != nil {
		return nil, err
	}
	if v, verr := version(raw); verr == nil && v == m.Current() {
		return raw, nil
	}
	unlock, err := s.lock(name)
	if err != nil {
		return nil, err
	}
	defer unlock()
	raw, migrated, err := s.readNoLock(name, m)
	if err != nil {
		return nil, err
	}
	if migrated {
		if err := s.writeNoLock(name, raw); err != nil {
			return nil, err
		}
	}
	return raw, nil
}

// Update is read-modify-write under the lock. fn's error aborts without writing.
func (s *Store) Update(name string, m Migrator, fn func([]byte) ([]byte, error)) ([]byte, error) {
	unlock, err := s.lock(name)
	if err != nil {
		return nil, err
	}
	defer unlock()
	raw, _, err := s.readNoLock(name, m)
	if err != nil {
		return nil, err
	}
	out, err := fn(raw)
	if err != nil {
		return nil, err
	}
	if err := s.writeNoLock(name, out); err != nil {
		return nil, err
	}
	return out, nil
}

// Load and Modify are typed conveniences over Read and Update.
func Load[T any](s *Store, name string, m Migrator) (*T, error) {
	raw, err := s.Read(name, m)
	if err != nil {
		return nil, err
	}
	var t T
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("%s.json: %w", name, err)
	}
	return &t, nil
}

func Modify[T any](s *Store, name string, m Migrator, fn func(*T) error) (*T, error) {
	var result T
	_, err := s.Update(name, m, func(raw []byte) ([]byte, error) {
		var t T
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, err
		}
		if err := fn(&t); err != nil {
			return nil, err
		}
		result = t
		return json.MarshalIndent(t, "", "  ")
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// WriteFile replaces path whole: a uniquely named temp file beside it, then
// a rename, so a reader never sees half a file and two writers never share
// a temp file. For files that are not versioned documents (config.toml, an
// agent's settings, a shell startup file, resume files).
//
// A symlink is written through, even when its target does not exist yet (a
// dotfiles repo's .zshrc stays a link). An existing file keeps its
// permissions; perm is for a new one.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	path, err := resolveLinks(path)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(path); err == nil {
		perm = fi.Mode().Perm()
	}
	return replace(path, data, perm)
}

// resolveLinks follows a chain of symlinks to the file it ends at, which
// need not exist.
func resolveLinks(path string) (string, error) {
	for range 40 {
		target, err := os.Readlink(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.EINVAL) {
				return path, nil // not there yet, or not a link
			}
			return "", err
		}
		if !filepath.IsAbs(target) {
			dir := filepath.Dir(path)
			if real, err := filepath.EvalSymlinks(dir); err == nil {
				dir = real // relative to where the link really is
			}
			target = filepath.Join(dir, target)
		}
		path = target
	}
	return "", fmt.Errorf("%s: too many levels of symlinks", path)
}

// replace writes data to a uniquely named temp file beside path, then
// renames it over path. The temp file never survives a failure.
func replace(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// EditFile reads path (empty if missing), passes it to edit, and writes
// what edit returns through WriteFile; nil means no change. The whole edit
// holds a lock on the file's folder, so two sous processes editing the
// same file never lose each other's change.
func EditFile(path string, perm os.FileMode, edit func(old []byte) ([]byte, error)) error {
	path, err := resolveLinks(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := syscall.Flock(int(dir.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(dir.Fd()), syscall.LOCK_UN)
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	out, err := edit(old)
	if err != nil || out == nil {
		return err
	}
	return WriteFile(path, out, perm)
}

// Locked runs fn holding the store's lock for name, the same lock the
// document of that name is written under. For rules that must check and
// act as one step across processes.
func (s *Store) Locked(name string, fn func() error) error {
	unlock, err := s.lock(name)
	if err != nil {
		return err
	}
	defer unlock()
	return fn()
}
