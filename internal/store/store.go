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
	tmp, err := os.CreateTemp(s.Home, "."+name+".*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), s.path(name))
}

// Read returns the migrated document; if a migration ran it is persisted.
func (s *Store) Read(name string, m Migrator) ([]byte, error) {
	raw, migrated, err := s.readNoLock(name, m)
	if err != nil {
		return nil, err
	}
	if migrated {
		unlock, err := s.lock(name)
		if err != nil {
			return nil, err
		}
		defer unlock()
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
