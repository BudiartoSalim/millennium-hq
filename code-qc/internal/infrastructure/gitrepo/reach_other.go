//go:build !windows

package gitrepo

import (
	"errors"
	"io/fs"
)

// maxLinks bounds the links one walk follows: as many as Linux follows in
// one path (macOS follows fewer), so a path the OS can open is never one
// the walk gives up on.
const maxLinks = 40

// entryOf is the kind of the entry info describes (see classify); only
// Windows has file attributes to consult.
func entryOf(info fs.FileInfo) (entryKind, error) { return classify(info.Mode(), 0), nil }

// normalize spells a walked path as the guard compares it; outside
// Windows, a path has the one spelling the walk gives it.
func normalize(p string) (string, error) { return p, nil }

// volumePathNames are the paths Windows mounts a volume at; outside
// Windows a link that names a volume by its GUID leads to no known place.
func volumePathNames(string) ([]string, error) {
	return nil, errors.New("only Windows mounts volumes by their GUID")
}
