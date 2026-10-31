package gitrepo

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"millennium-code-qc/internal/domain"
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

func TestChangedLines(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.sh", "one\ntwo\nthree\nfour\nfive\n")
	commitAll(t, dir, "base")
	write(t, dir, "a.sh", "one\nTWO\nthree\nfive\nsix\nseven\n")
	repo := open(t, dir)
	set, err := repo.Changes(t.Context(), "")
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	lines, err := repo.ChangedLines(t.Context(), set.Base, "a.sh")
	if err != nil {
		t.Fatalf("ChangedLines: %v", err)
	}
	for line, want := range map[int]bool{1: false, 2: true, 3: false, 4: false, 5: true, 6: true, 7: false} {
		if got := lines.Contains(line); got != want {
			t.Errorf("line %d changed = %v, want %v (set %+v)", line, got, want, lines)
		}
	}
	if _, err := repo.ChangedLines(t.Context(), "-x", "a.sh"); err == nil {
		t.Fatal("an option passed as revision was accepted")
	}
}

func TestParseHunks(t *testing.T) {
	set, err := parseHunks([]byte("diff --git a/x b/x\n@@ -3 +3 @@\n-a\n+b\n@@ -10,2 +9,0 @@\n-c\n-d\n@@ -20,0 +18,3 @@\n+e\n"))
	if err != nil {
		t.Fatalf("parseHunks: %v", err)
	}
	want := []domain.LineRange{{First: 3, Last: 3}, {First: 18, Last: 20}}
	if !slices.Equal(set.Ranges, want) {
		t.Fatalf("ranges = %+v, want %+v (a pure deletion adds nothing)", set.Ranges, want)
	}
	if _, err := parseHunks([]byte("@@ garbage @@\n")); err == nil {
		t.Fatal("a malformed hunk header must be an error")
	}
}

func TestNearestUp(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "go.mod", "module root")
	write(t, dir, "services/game/api/go.mod", "module game")
	write(t, dir, "services/game/api/internal/domain/a.go", "package domain")
	repo := open(t, dir)
	tests := []struct {
		from, within, want string
		ok                 bool
	}{
		{"services/game/api/internal/domain", "", "services/game/api", true},
		{"services/game/api", "services/game/api", "services/game/api", true},
		{"services/other", "", "", true},
		{"services/other", "services/other", "", false},
	}
	for _, tt := range tests {
		got, ok := repo.NearestUp(tt.from, "go.mod", tt.within)
		if got != tt.want || ok != tt.ok {
			t.Errorf("NearestUp(%q, within %q) = %q, %v; want %q, %v", tt.from, tt.within, got, ok, tt.want, tt.ok)
		}
	}
}

func TestIdentityIsSharedByWorktrees(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.go", "package a")
	commitAll(t, dir, "base")
	worktree := filepath.Join(t.TempDir(), "wt")
	gitT(t, dir, "worktree", "add", "-q", "-b", "engineer", worktree)
	main, err := open(t, dir).Identity(t.Context())
	if err != nil {
		t.Fatalf("Identity: %v", err)
	}
	other, err := open(t, worktree).Identity(t.Context())
	if err != nil || other != main {
		t.Fatalf("worktree identity %q, %v; want the clone's %q", other, err, main)
	}
}
