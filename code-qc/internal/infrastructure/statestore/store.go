// Package statestore keeps the hooks' small counters in files. It lives in
// the plugin's data directory, which hooks reach (they run outside the
// sandbox) and sandboxed agents cannot write.
package statestore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Store counts per key in one directory.
type Store struct {
	dir string
}

// New returns a store in dir, created on first use.
func New(dir string) *Store { return &Store{dir: dir} }

// Increment adds one to key's count and returns the new value.
func (s *Store) Increment(key string) (int, error) {
	path := s.path(key)
	count := 0
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if count, err = strconv.Atoi(strings.TrimSpace(string(data))); err != nil {
			return 0, fmt.Errorf("unreadable counter %s: %w", path, err)
		}
	case !errors.Is(err, fs.ErrNotExist):
		return 0, fmt.Errorf("read counter: %w", err)
	}
	count++
	if err := s.write(path, strconv.Itoa(count)); err != nil {
		return 0, err
	}
	return count, nil
}

// Reset forgets key's count.
func (s *Store) Reset(key string) error {
	if err := os.Remove(s.path(key)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("reset counter: %w", err)
	}
	return nil
}

// write replaces the file atomically, so a parallel hook never reads half.
func (s *Store) write(path, value string) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	tmp, err := os.CreateTemp(s.dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("write counter: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(value); err != nil {
		tmp.Close()
		return fmt.Errorf("write counter: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write counter: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write counter: %w", err)
	}
	return nil
}

// path hashes the key: session and agent ids come from hook input and are
// never used as file names directly.
func (s *Store) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:16])+".count")
}
