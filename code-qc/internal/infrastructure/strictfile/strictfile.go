// Package strictfile reads the files that hold millennium's rules (the
// config, convention packs, proposals, approval records) strictly: only
// regular files, never through a symlink, bounded in size, and JSON with
// unknown fields, duplicate keys and trailing data all rejected.
package strictfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
)

// ReadRegular reads the file at path, refusing anything but a regular file
// and anything larger than limit bytes. A missing file is an error that
// wraps fs.ErrNotExist. The file is checked after opening to be the one it
// inspected, so it cannot be swapped for a symlink in between.
func ReadRegular(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
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
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is larger than %d bytes", path, limit)
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

// DecodeJSON decodes data into v: one JSON object, no duplicate keys, no
// fields v does not declare, nothing after it. An unknown field is reported
// with where it is and the fields allowed there.
func DecodeJSON(data []byte, v any) error {
	if err := rejectDuplicateKeys(data); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return explainDecodeError(err, data, v)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("parse: unexpected data after the object")
	}
	return nil
}
