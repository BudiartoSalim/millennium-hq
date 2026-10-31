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

// ChangeSet is what the gates check: the files and the revision they are
// compared against. An empty Base means there is no "before": an audit, which
// holds every file to the full rules.
type ChangeSet struct {
	Base  string
	Files []ChangedFile
}

// LanguageOf maps a path to the language whose checks apply to it, by
// extension. Extensionless scripts need DetectLanguage.
func LanguageOf(p string) (Language, bool) {
	switch strings.ToLower(path.Ext(p)) {
	case ".go":
		return LanguageGo, true
	case ".ts", ".tsx", ".mts", ".cts":
		return LanguageTypeScript, true
	case ".py":
		return LanguagePython, true
	case ".sh", ".bash":
		return LanguageShell, true
	}
	return "", false
}

// DetectLanguage is LanguageOf plus the shebang of extensionless files, so a
// script such as bin/deploy with "#!/usr/bin/env bash" is checked as shell.
// head is the start of the file; the first line is enough.
func DetectLanguage(p string, head []byte) (Language, bool) {
	if lang, ok := LanguageOf(p); ok {
		return lang, true
	}
	if path.Ext(p) != "" {
		return "", false
	}
	if _, ok := ShellDialect(head); ok {
		return LanguageShell, true
	}
	return "", false
}

// ShellDialect reads a shell shebang ("#!/bin/sh", "#!/usr/bin/env bash")
// and returns the interpreter name, or false when there is none.
func ShellDialect(head []byte) (string, bool) {
	line, _, _ := strings.Cut(string(head), "\n")
	if !strings.HasPrefix(line, "#!") {
		return "", false
	}
	fields := strings.Fields(strings.TrimPrefix(line, "#!"))
	if len(fields) == 0 {
		return "", false
	}
	interpreter := path.Base(fields[0])
	if interpreter == "env" {
		args := fields[1:]
		for len(args) > 0 && strings.HasPrefix(args[0], "-") {
			args = args[1:]
		}
		if len(args) == 0 {
			return "", false
		}
		interpreter = path.Base(args[0])
	}
	return interpreter, isShell(interpreter)
}

// isShell reports whether interpreter is a shell shellcheck understands.
func isShell(interpreter string) bool {
	switch interpreter {
	case "sh", "bash", "dash", "ksh":
		return true
	}
	return false
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
