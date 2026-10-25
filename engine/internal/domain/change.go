package domain

import (
	"path"
	"strings"
)

// ChangeKind says how a file differs from the base revision.
type ChangeKind string

// Change kinds. A rename is reported as a deletion plus an addition.
const (
	ChangeAdded    ChangeKind = "added"
	ChangeModified ChangeKind = "modified"
	ChangeDeleted  ChangeKind = "deleted"
)

// ChangedFile is one entry of a change set. Path is relative to the
// repository root and uses forward slashes.
type ChangedFile struct {
	Path string
	Kind ChangeKind
}

// LanguageOf maps a path to the language whose checks apply to it.
func LanguageOf(p string) (Language, bool) {
	switch strings.ToLower(path.Ext(p)) {
	case ".go":
		return LanguageGo, true
	case ".ts", ".tsx", ".mts", ".cts":
		return LanguageTypeScript, true
	case ".py":
		return LanguagePython, true
	}
	return "", false
}

// IsRepoRelative reports whether p is a clean path inside the repository:
// non-empty, relative, forward slashes only, and free of "." and ".."
// segments and NUL bytes. Paths from git are checked with it before use.
func IsRepoRelative(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\\x00") {
		return false
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}
