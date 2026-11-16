//go:build unix

package gitrepo

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"millennium-code-qc/internal/domain"
)

// Claude Code's sandbox mounts character devices over protected paths, and
// git lists them as untracked files. A named pipe stands in for them here:
// creating a device node needs root.
func TestChangesSkipsSpecialFiles(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "main.go", "package main")
	commitAll(t, dir, "base")
	// Not a placeholder's name, so only the special-file rule drops it.
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe.go"), 0o600); err != nil {
		t.Skipf("cannot create a named pipe here: %v", err)
	}
	write(t, dir, "new.go", "package main")

	got := changes(t, open(t, dir), "")
	assertChanges(t, got, domain.ChangedFile{Path: "new.go", Kind: domain.ChangeAdded})
}

// Only a path that does not exist is settled; any other failure to inspect
// one must block rather than drop the file unchecked. Unix only: Windows
// reports a non-directory parent as "path not found".
func TestSettleBlocksWhenAPathCannotBeInspected(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "file.txt", "not a directory")
	write(t, dir, ".claude", "a file where a directory is expected")
	repo := open(t, dir)
	// .claude/agents is a placeholder's path: the exemption covers a
	// placeholder that is empty or gone, never one that cannot be inspected.
	for _, p := range []string{"file.txt/child.go", ".claude/agents"} {
		_, err := repo.settle([]domain.ChangedFile{{Path: p, Kind: domain.ChangeAdded}}, listedUntracked)
		if err == nil || !strings.Contains(err.Error(), "changed file: inspect "+p) {
			t.Fatalf("settle(%s) error = %v, want an inspection failure", p, err)
		}
	}
	if _, err := repo.keepPaths([]string{".claude/agents"}, untrackedKept); err == nil {
		t.Fatal("keepPaths dropped a path it could not inspect")
	}
}
