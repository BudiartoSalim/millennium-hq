//go:build unix

package gitrepo

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// assertMapsTo checks what arg maps to: the mapping is what the hooks hand
// the guard, whose decisions the guard's and the hook's tests cover.
func assertMapsTo(t *testing.T, repo *Repository, arg, want string) {
	t.Helper()
	if got, err := repo.RepoPath(arg); err != nil || got != want {
		t.Fatalf("RepoPath(%q) = %q, %v; want %q", arg, got, err, want)
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

	assertMapsTo(t, repo, filepath.Join(dir, ".millennium", "config.json"), ".millennium/config.json")
	assertMapsTo(t, repo, filepath.Join(worktree, "src", "new.go"), "worktrees/agent/src/new.go")
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

	for name, tt := range map[string]struct{ arg, want string }{
		"through a symlink to the repository": {filepath.Join(repoLink, ".millennium", "config.json"), ".millennium/config.json"},
		"a symlink to the file":               {filepath.Join(dir, "alias.json"), ".millennium/config.json"},
		"through a symlinked directory":       {filepath.Join(dir, "rules", "config.json"), ".millennium/config.json"},
		"a new file in a symlinked directory": {filepath.Join(dir, "rules", "new", "approval.json"), ".millennium/new/approval.json"},
	} {
		t.Run(name, func(t *testing.T) {
			assertMapsTo(t, repo, tt.arg, tt.want)
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

// MapPath maps into any tree, such as the local config directory: one that
// does not exist yet, one reached through a symlink, and one named in a
// different case, as a case-insensitive file system would read it.
func TestMapPath(t *testing.T) {
	base := t.TempDir()
	local := filepath.Join(base, "config", "millennium", "projects")
	fresh := filepath.Join(base, "fresh", "millennium", "projects")
	repo := filepath.Join(base, "repo")
	if err := errors.Join(os.MkdirAll(local, 0o755), os.MkdirAll(repo, 0o755)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(local, filepath.Join(repo, "lclink")); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, root, abs, want string
		outside               bool
	}{
		{name: "a root that does not exist yet", root: fresh, abs: filepath.Join(fresh, "ab", "config.json"), want: "ab/config.json"},
		{name: "through a symlink into the root", root: local, abs: filepath.Join(repo, "lclink", "abc", "config.json"), want: "abc/config.json"},
		{name: "the root in another case", root: repo, abs: filepath.Join(base, "REPO", "x.go"), want: "x.go"},
		{name: "the root itself", root: repo, abs: repo, want: ""},
		{name: "outside", root: local, abs: filepath.Join(repo, "x.go"), outside: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MapPath(tt.root, tt.abs)
			if errors.Is(err, ErrOutsideRepository) != tt.outside || !tt.outside && (err != nil || got != tt.want) {
				t.Fatalf("MapPath(%q, %q) = %q, %v; want %q, outside %v", tt.root, tt.abs, got, err, tt.want, tt.outside)
			}
		})
	}
	if _, err := MapPath(local, "relative/config.json"); err == nil || errors.Is(err, ErrOutsideRepository) || !strings.Contains(err.Error(), "not an absolute path") {
		t.Fatalf("a relative path: err = %v, want a failure other than outside", err)
	}
}
