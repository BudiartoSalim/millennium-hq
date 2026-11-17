//go:build unix

package gitrepo

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"millennium-code-qc/internal/domain"
)

// assertEditDenied checks that the guard denies an Edit of what arg maps to:
// the mapping is what the hooks hand the guard.
func assertEditDenied(t *testing.T, repo *Repository, arg string) {
	t.Helper()
	p, err := repo.RepoPath(arg)
	if err != nil {
		t.Fatalf("RepoPath(%q): %v", arg, err)
	}
	if decision := domain.Guard(domain.Config{}, domain.ToolCall{Tool: "Edit", Paths: []string{p}}); !decision.Deny {
		t.Fatalf("RepoPath(%q) = %q, and the guard allows an Edit of it", arg, p)
	}
}

// A hook may run in a working directory that is gone, such as a removed
// worktree. Absolute paths, the only ones the hook passes, still map, so
// the guard still sees the protected config.
func TestRepoPathWithoutAWorkingDirectory(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, ".millennium/config.json", "{}")
	repo := open(t, dir)
	worktree := filepath.Join(dir, "worktrees", "agent")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(worktree)
	if err := os.RemoveAll(filepath.Join(dir, "worktrees")); err != nil {
		t.Fatal(err)
	}

	assertEditDenied(t, repo, filepath.Join(dir, ".millennium", "config.json"))
	// The hook joins a relative tool path to the event's cwd, unresolved.
	assertEditDenied(t, repo, worktree+"/../../.millennium/config.json")
	if _, err := repo.RepoPath("a.go"); err == nil || errors.Is(err, ErrOutsideRepository) {
		t.Fatalf("a relative path without a working directory: err = %v, want a failure other than outside", err)
	}
}

// Symlinks are resolved in the root and in the longest existing part of the
// path, so a symlink cannot hide a protected path from the guard.
func TestRepoPathResolvesSymlinks(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, ".millennium/config.json", "{}")
	outside := t.TempDir()
	link := func(target, name string) string {
		t.Helper()
		if err := os.Symlink(target, name); err != nil {
			t.Fatal(err)
		}
		return name
	}
	repoLink := link(dir, filepath.Join(outside, "repo-link"))
	link(filepath.Join(dir, ".millennium", "config.json"), filepath.Join(dir, "alias.json"))
	link(filepath.Join(dir, ".millennium"), filepath.Join(dir, "rules"))
	repo := open(t, dir)

	for name, arg := range map[string]string{
		"through a symlink to the repository": filepath.Join(repoLink, ".millennium", "config.json"),
		"a symlink to the file":               filepath.Join(dir, "alias.json"),
		"through a symlinked directory":       filepath.Join(dir, "rules", "config.json"),
		"a new file in a symlinked directory": filepath.Join(dir, "rules", "new", "approval.json"),
	} {
		t.Run(name, func(t *testing.T) {
			assertEditDenied(t, repo, arg)
		})
	}
	throughRoot := &Repository{root: repoLink}
	if got, err := throughRoot.RepoPath(filepath.Join(dir, ".millennium", "config.json")); err != nil || got != ".millennium/config.json" {
		t.Fatalf("a root reached through a symlink: %q, %v", got, err)
	}
	link(outside, filepath.Join(dir, "out"))
	if _, err := repo.RepoPath(filepath.Join(dir, "out", "x.go")); !errors.Is(err, ErrOutsideRepository) {
		t.Fatalf("a symlink leading outside: err = %v, want ErrOutsideRepository", err)
	}
}

// A path whose existing part cannot be resolved or looked at maps to an
// error that is not ErrOutsideRepository, so the hook blocks rather than
// skips it.
func TestRepoPathFailsOnAnUnresolvablePath(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the permissions these cases rely on")
	}
	dir := newRepo(t)
	write(t, dir, "file.txt", "not a directory")
	if err := os.Symlink(filepath.Join(dir, ".millennium", "config.json"), filepath.Join(dir, "dangling.json")); err != nil {
		t.Fatal(err)
	}
	locked := lockedDir(t, dir)
	repo := open(t, dir)
	tests := []struct{ name, arg, want string }{
		{"a dangling symlink", filepath.Join(dir, "dangling.json"), "resolve"},
		{"below a file", filepath.Join(dir, "file.txt", "x"), "inspect"},
		{"below a directory that cannot be searched", filepath.Join(locked, "x"), "inspect"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := repo.RepoPath(tt.arg)
			if err == nil || errors.Is(err, ErrOutsideRepository) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("RepoPath(%q): err = %v, want a %q failure other than outside", tt.arg, err, tt.want)
			}
		})
	}
}
