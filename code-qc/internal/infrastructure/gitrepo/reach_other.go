//go:build !windows

package gitrepo

import "io/fs"

// entryOf is the kind of the entry info describes (see classify); only
// Windows has file attributes to consult.
func entryOf(info fs.FileInfo) (entryKind, error) { return classify(info.Mode(), 0), nil }

// normalize spells a walked path as the guard compares it; outside
// Windows, a path has the one spelling the walk gives it.
func normalize(p string) (string, error) { return p, nil }
