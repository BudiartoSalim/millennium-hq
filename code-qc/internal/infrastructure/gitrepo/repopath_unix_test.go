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

// Resolve names what a path reaches: through a symlink, below a part that
// does not exist yet, and as an error when an existing part cannot be
// resolved, since what the path reaches is then unknown.
func TestResolve(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(base, "config", "millennium", "projects")
	repo := filepath.Join(base, "repo")
	if err := errors.Join(os.MkdirAll(local, 0o755), os.MkdirAll(repo, 0o755)); err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(os.Symlink(local, filepath.Join(repo, "lclink")), os.Symlink(filepath.Join(base, "nowhere"), filepath.Join(repo, "dangling"))); err != nil {
		t.Fatal(err)
	}
	tests := []struct{ name, abs, want, wantErr string }{
		{name: "through a symlink", abs: filepath.Join(repo, "lclink", "abc", "config.json"), want: filepath.Join(local, "abc", "config.json")},
		{name: "a path that does not exist yet", abs: filepath.Join(base, "fresh", "a", "b.json"), want: filepath.Join(base, "fresh", "a", "b.json")},
		{name: "cleaned as text", abs: repo + "//x/./y", want: filepath.Join(repo, "x", "y")},
		{name: "a dangling symlink", abs: filepath.Join(repo, "dangling", "x"), wantErr: "resolve"},
		{name: "a relative path", abs: filepath.Join("relative", "config.json"), wantErr: "not an absolute path"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(tt.abs)
			if got != tt.want || !errorMatches(err, tt.wantErr) {
				t.Fatalf("Resolve(%q) = %q, %v; want %q, error %q", tt.abs, got, err, tt.want, tt.wantErr)
			}
		})
	}
}
