// Package configfile reads .millennium/config.json strictly: unknown fields,
// duplicate keys, trailing data, oversized files and anything that is not a
// regular file are all errors, never ignored.
package configfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"millennium/internal/application/port"
	"millennium/internal/domain"
)

// maxConfigBytes bounds what the engine reads; a real config is a few hundred
// bytes.
const maxConfigBytes = 64 << 10

// Loader loads the config from one path.
type Loader struct {
	path string
}

// NewLoader returns a loader for the config file at path.
func NewLoader(path string) *Loader {
	return &Loader{path: path}
}

// Load reads, decodes and validates the config. It returns
// port.ErrConfigNotFound only when the file does not exist.
func (l *Loader) Load() (domain.Config, error) {
	data, err := readRegularFile(l.path)
	if err != nil {
		return domain.Config{}, err
	}
	in, err := decode(data)
	if err != nil {
		return domain.Config{}, fmt.Errorf("%s: %w", l.path, err)
	}
	cfg, err := domain.NewConfig(in)
	if err != nil {
		return domain.Config{}, fmt.Errorf("%s: %w", l.path, err)
	}
	return cfg, nil
}

// readRegularFile refuses symlinks, so the config cannot be redirected to a
// file outside the reviewed tree, and checks after opening that the file is
// still the one it inspected.
func readRegularFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, port.ErrConfigNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s must be a regular file, found %s", path, describeMode(info.Mode()))
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", path, err)
	}
	if !os.SameFile(info, opened) {
		return nil, fmt.Errorf("%s changed while it was being read", path)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > maxConfigBytes {
		return nil, fmt.Errorf("%s is larger than %d bytes", path, maxConfigBytes)
	}
	return data, nil
}

func describeMode(mode fs.FileMode) string {
	switch {
	case mode.IsDir():
		return "a directory"
	case mode&fs.ModeSymlink != 0:
		return "a symlink"
	}
	return "a special file"
}

func decode(data []byte) (domain.ConfigInput, error) {
	if err := rejectDuplicateKeys(data); err != nil {
		return domain.ConfigInput{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var dto configDTO
	if err := dec.Decode(&dto); err != nil {
		return domain.ConfigInput{}, fmt.Errorf("parse: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return domain.ConfigInput{}, errors.New("parse: unexpected data after the config object")
	}
	return dto.toInput(), nil
}
