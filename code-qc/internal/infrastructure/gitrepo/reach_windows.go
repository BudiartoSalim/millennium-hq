//go:build windows

package gitrepo

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

// maxLinks bounds the links one walk follows: as many reparse points as
// Windows follows in one path, so a path the OS can open is never one the
// walk gives up on.
const maxLinks = 63

// The standard library has no call that lists a volume's mount paths, so
// the engine calls kernel32 itself; kernel32.dll is one of Windows' known
// DLLs, which the loader takes from the system directory only.
var procGetVolumePathNames = syscall.NewLazyDLL("kernel32.dll").NewProc("GetVolumePathNamesForVolumeNameW")

const (
	// errorMoreData is ERROR_MORE_DATA: the buffer was too small, and the
	// call reported the size it needs.
	errorMoreData syscall.Errno = 234
	// maxVolumePathChars bounds the list of a volume's paths the engine
	// reads, in UTF-16 code units.
	maxVolumePathChars = 32 * 1024
)

// volumePathNames are the paths Windows mounts volume at (`\\?\Volume{GUID}\`),
// its drive letter and mounted folders (`E:\`, `C:\dev\`), as
// GetVolumePathNamesForVolumeNameW lists them.
func volumePathNames(volume string) ([]string, error) {
	name, err := syscall.UTF16PtrFromString(volume)
	if err != nil {
		return nil, fmt.Errorf("list the paths of volume %s: %w", volume, err)
	}
	size := uint32(syscall.MAX_PATH + 1)
	for {
		buf := make([]uint16, size)
		var needed uint32
		ok, _, callErr := procGetVolumePathNames.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&buf[0])), uintptr(size), uintptr(unsafe.Pointer(&needed)))
		if ok != 0 {
			return multiString(buf), nil
		}
		if !errors.Is(callErr, errorMoreData) || needed <= size || needed > maxVolumePathChars {
			return nil, fmt.Errorf("list the paths of volume %s: %w", volume, callErr)
		}
		size = needed
	}
}

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
