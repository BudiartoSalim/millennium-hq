//go:build unix

package gitrepo

import (
	"path/filepath"
	"syscall"
	"testing"

	"millennium/internal/domain"
)

// Claude Code's sandbox mounts character devices over protected paths, and
// git lists them as untracked files. A named pipe stands in for them here:
// creating a device node needs root.
func TestChangesSkipsSpecialFiles(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "main.go", "package main")
	commitAll(t, dir, "base")
	if err := syscall.Mkfifo(filepath.Join(dir, ".bashrc"), 0o600); err != nil {
		t.Skipf("cannot create a named pipe here: %v", err)
	}
	write(t, dir, "new.go", "package main")

	got := changes(t, open(t, dir), "")
	assertChanges(t, got, domain.ChangedFile{Path: "new.go", Kind: domain.ChangeAdded})
}
