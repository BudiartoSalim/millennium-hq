//go:build windows

package gitrepo

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// entryOf is the kind of the entry info describes, from its mode and its
// Windows file attributes (see classify). An entry without attributes
// cannot be told from a junction, so it is an error.
func entryOf(info fs.FileInfo) (entryKind, error) {
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return 0, fmt.Errorf("%s: the OS reported no file attributes, so the engine cannot tell whether it is a link", info.Name())
	}
	return classify(info.Mode(), data.FileAttributes), nil
}

// normalize spells a walked path as the directories list it. Windows opens
// a file by more than one spelling, such as its short 8.3 name
// (`PROGRA~1`); filepath.EvalSymlinks spells each existing part by its
// long name. The walk has followed every link already, so EvalSymlinks
// only respells: no junction is left for it to walk through unfollowed.
func normalize(p string) (string, error) {
	existing, rest := p, ""
	for existing != filepath.Dir(existing) {
		_, err := os.Lstat(existing)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("inspect %s: %w", existing, err)
		}
		rest = filepath.Join(filepath.Base(existing), rest)
		existing = filepath.Dir(existing)
	}
	spelled, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", fmt.Errorf("spell %s as Windows lists it: %w", existing, err)
	}
	return filepath.Join(spelled, rest), nil
}
