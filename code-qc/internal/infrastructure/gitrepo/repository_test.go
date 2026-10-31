package gitrepo

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"millennium-code-qc/internal/domain"
)

// These tests run real git in a temporary repository, isolated from the
// user's global and system git config.

func isolateGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("git is required for these tests: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.invalid")
}

func newRepo(t *testing.T) string {
	t.Helper()
	isolateGit(t)
	dir := t.TempDir()
	gitT(t, dir, "init", "-q", "-b", "main")
	return dir
}

func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitAll(t *testing.T, dir, message string) {
	t.Helper()
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-q", "-m", message)
}

func open(t *testing.T, dir string) *Repository {
	t.Helper()
	repo, err := Open(t.Context(), dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return repo
}

func changes(t *testing.T, repo *Repository, base string) []domain.ChangedFile {
	t.Helper()
	set, err := repo.Changes(t.Context(), base)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if set.Base == "" {
		t.Fatal("a change set must name its base tree")
	}
	return set.Files
}

func assertChanges(t *testing.T, got []domain.ChangedFile, want ...domain.ChangedFile) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("changes = %+v\nwant      %+v", got, want)
	}
}

func TestOpenFindsRootFromSubdirectory(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "sub/deeper/file.txt", "x")
	repo := open(t, filepath.Join(dir, "sub", "deeper"))
	want, _ := filepath.EvalSymlinks(dir)
	got, _ := filepath.EvalSymlinks(repo.Root())
	if got != want {
		t.Fatalf("Root() = %q, want %q", got, want)
	}
}

func TestOpenOutsideRepositoryFails(t *testing.T) {
	isolateGit(t)
	if _, err := Open(t.Context(), t.TempDir()); !errors.Is(err, ErrNotRepository) {
		t.Fatalf("err = %v, want ErrNotRepository outside a git repository", err)
	}
	if _, err := Open(t.Context(), filepath.Join(t.TempDir(), "missing")); err == nil || errors.Is(err, ErrNotRepository) {
		t.Fatalf("a directory that does not exist is a different error: %v", err)
	}
}

func TestChangesWithoutCommitsListsEveryFileAsAdded(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "b.go", "package b")
	write(t, dir, "a/x.py", "x = 1")
	gitT(t, dir, "add", "b.go")

	got := changes(t, open(t, dir), "")
	assertChanges(t, got,
		domain.ChangedFile{Path: "a/x.py", Kind: domain.ChangeAdded},
		domain.ChangedFile{Path: "b.go", Kind: domain.ChangeAdded},
	)
}

func TestChangesAgainstHead(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, ".gitignore", "build/\n")
	write(t, dir, "keep.go", "package keep")
	write(t, dir, "edit.go", "package edit")
	write(t, dir, "gone.go", "package gone")
	commitAll(t, dir, "base")

	write(t, dir, "edit.go", "package edit // changed")
	write(t, dir, "with space/new file.ts", "export {}")
	write(t, dir, "build/out.js", "ignored")
	if err := os.Remove(filepath.Join(dir, "gone.go")); err != nil {
		t.Fatal(err)
	}

	got := changes(t, open(t, dir), "")
	assertChanges(t, got,
		domain.ChangedFile{Path: "edit.go", Kind: domain.ChangeModified},
		domain.ChangedFile{Path: "gone.go", Kind: domain.ChangeDeleted},
		domain.ChangedFile{Path: "with space/new file.ts", Kind: domain.ChangeAdded},
	)
}

func TestChangesAgainstBranchIncludeCommittedWork(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "main.go", "package main")
	commitAll(t, dir, "base")
	gitT(t, dir, "switch", "-q", "-c", "feature")
	write(t, dir, "feature.go", "package main")
	commitAll(t, dir, "feature")

	got := changes(t, open(t, dir), "main")
	assertChanges(t, got, domain.ChangedFile{Path: "feature.go", Kind: domain.ChangeAdded})
}

func TestChangesRejectsBadBase(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "main.go", "package main")
	commitAll(t, dir, "base")
	repo := open(t, dir)
	smuggled := filepath.Join(t.TempDir(), "pwned")
	for _, base := range []string{"--output=" + smuggled, "-p", "no-such-branch"} {
		t.Run(base, func(t *testing.T) {
			if _, err := repo.Changes(t.Context(), base); err == nil {
				t.Fatalf("base %q accepted", base)
			}
		})
	}
	if _, err := os.Stat(smuggled); err == nil {
		t.Fatal("an option smuggled in as base was executed")
	}
}

func TestTracked(t *testing.T) {
	dir := newRepo(t)
	repo := open(t, dir)
	if tracked, err := repo.Tracked(t.Context(), "HEAD", domain.ConfigPath); err != nil || tracked {
		t.Fatalf("no commits: tracked = %v, err = %v; want false, nil", tracked, err)
	}

	write(t, dir, domain.ConfigPath, "{}")
	write(t, dir, "other.txt", "x")
	commitAll(t, dir, "base")
	tests := []struct {
		path string
		want bool
	}{
		{domain.ConfigPath, true},
		{"absent.json", false},
	}
	for _, tt := range tests {
		tracked, err := repo.Tracked(t.Context(), "HEAD", tt.path)
		if err != nil || tracked != tt.want {
			t.Errorf("Tracked(%q) = %v, %v; want %v", tt.path, tracked, err, tt.want)
		}
	}
	if _, err := repo.Tracked(t.Context(), "--all", domain.ConfigPath); err == nil {
		t.Error("an option passed as revision was accepted")
	}
}

func TestChangesIgnoresRepositoryFsmonitor(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "main.go", "package main")
	commitAll(t, dir, "base")
	marker := filepath.Join(t.TempDir(), "fsmonitor-ran")
	script := filepath.Join(t.TempDir(), "monitor.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, dir, "config", "core.fsmonitor", script)

	if _, err := open(t, dir).Changes(t.Context(), ""); err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the repository's core.fsmonitor program was executed")
	}
}
