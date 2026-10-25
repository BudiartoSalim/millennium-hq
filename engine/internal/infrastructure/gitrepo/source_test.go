package gitrepo

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestFilesListsTrackedAndUntrackedButNotIgnoredOrDeleted(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, ".gitignore", "build/\n")
	write(t, dir, "kept.go", "package a")
	write(t, dir, "gone.go", "package a")
	commitAll(t, dir, "base")
	write(t, dir, "new/file.ts", "export {}")
	write(t, dir, "build/out.js", "ignored")
	if err := os.Remove(filepath.Join(dir, "gone.go")); err != nil {
		t.Fatal(err)
	}

	files, err := open(t, dir).Files(t.Context())
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if want := []string{".gitignore", "kept.go", "new/file.ts"}; !slices.Equal(files, want) {
		t.Fatalf("files = %q, want %q", files, want)
	}
}

func TestReadWorking(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.go", "package a")
	repo := open(t, dir)
	if data, err := repo.ReadWorking("a.go"); err != nil || string(data) != "package a" {
		t.Fatalf("ReadWorking = %q, %v", data, err)
	}
	if err := os.Symlink(filepath.Join(dir, "a.go"), filepath.Join(dir, "link.go")); err == nil {
		if _, err := repo.ReadWorking("link.go"); err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("symlink: err = %v", err)
		}
	}
	write(t, dir, "big.go", strings.Repeat("x", maxSourceBytes+1))
	if _, err := repo.ReadWorking("big.go"); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("oversized file: err = %v", err)
	}
	if _, err := repo.ReadWorking("../outside.go"); err == nil {
		t.Fatal("a path outside the repository was read")
	}
}

func TestReadAtReturnsTheBaseVersion(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.go", "package a // v1")
	commitAll(t, dir, "base")
	write(t, dir, "a.go", "package a // v2")
	repo := open(t, dir)
	set, err := repo.Changes(t.Context(), "")
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}

	data, found, err := repo.ReadAt(t.Context(), set.Base, "a.go")
	if err != nil || !found || string(data) != "package a // v1" {
		t.Fatalf("ReadAt(a.go) = %q, %v, %v; want the committed version", data, found, err)
	}
	if _, found, err := repo.ReadAt(t.Context(), set.Base, "absent.go"); err != nil || found {
		t.Fatalf("absent file: found %v, err %v", found, err)
	}
	if _, _, err := repo.ReadAt(t.Context(), "--all", "a.go"); err == nil {
		t.Fatal("an option passed as revision was accepted")
	}
}

func TestReadAtOnEmptyTree(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.go", "package a")
	repo := open(t, dir)
	set, err := repo.Changes(t.Context(), "")
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if _, found, err := repo.ReadAt(t.Context(), set.Base, "a.go"); err != nil || found {
		t.Fatalf("before the first commit nothing exists at the base: found %v, err %v", found, err)
	}
}

func TestRepoPath(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "sub/deeper/a.go", "package a")
	repo := open(t, dir)
	tests := []struct {
		cwd, arg, want string
		ok             bool
	}{
		{dir, "sub/deeper/a.go", "sub/deeper/a.go", true},
		{filepath.Join(dir, "sub"), "deeper", "sub/deeper", true},
		{filepath.Join(dir, "sub", "deeper"), "..", "sub", true},
		{dir, ".", "", true},
		{dir, filepath.Join(dir, "sub"), "sub", true},
		{dir, "..", "", false},
		{filepath.Join(dir, "sub"), "../../elsewhere", "", false},
	}
	for _, tt := range tests {
		got, err := repo.relativeTo(tt.cwd, tt.arg)
		if (err == nil) != tt.ok || got != tt.want {
			t.Errorf("relativeTo(%q, %q) = %q, %v; want %q, ok %v", tt.cwd, tt.arg, got, err, tt.want, tt.ok)
		}
	}
}
