package gitrepo

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// worktreeRepo makes a repository with a linked worktree under
// .claude/worktrees and one that was deleted without git's knowledge, as a
// removed agent worktree is. Paths are resolved, as git reports them.
func worktreeRepo(t *testing.T) (main, linked, gone string) {
	t.Helper()
	dir := newRepo(t)
	main, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	write(t, main, "a.txt", "a")
	commitAll(t, main, "base")
	linked = filepath.Join(main, ".claude", "worktrees", "agent")
	gone = filepath.Join(main, ".claude", "worktrees", "gone")
	gitT(t, main, "worktree", "add", "-q", "-b", "agent", linked)
	gitT(t, main, "worktree", "add", "-q", "-b", "gone", gone)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	return main, linked, gone
}

// Every worktree but the one at hand is listed, whichever that is, gone
// ones included: the guard protects the rules of each.
func TestOtherWorktrees(t *testing.T) {
	main, linked, gone := worktreeRepo(t)
	for name, tt := range map[string]struct {
		from string
		want []string
	}{
		"from the main checkout":     {main, []string{linked, gone}},
		"from a linked worktree":     {linked, []string{main, gone}},
		"from a directory below one": {filepath.Join(main, ".claude"), []string{linked, gone}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := open(t, tt.from).OtherWorktrees(t.Context())
			if err != nil {
				t.Fatalf("OtherWorktrees: %v", err)
			}
			slices.Sort(got)
			slices.Sort(tt.want)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("OtherWorktrees() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A repository decides how much git prints, so the listing is bounded.
func TestWorktreeListIsBounded(t *testing.T) {
	main, _, _ := worktreeRepo(t)
	_, err := runLimited(t.Context(), main, 16, "worktree", "list", "--porcelain")
	if err == nil || !strings.Contains(err.Error(), "printed more than 16 bytes") {
		t.Fatalf("err = %v, want the output limit", err)
	}
	if out, err := runLimited(t.Context(), main, maxWorktreeList, "rev-parse", "--is-inside-work-tree"); err != nil || trimLine(out) != "true" {
		t.Fatalf("within the limit: %q, %v", out, err)
	}
	if _, err := runLimited(t.Context(), main, maxWorktreeList, "rev-parse", "--verify", "no-such-rev"); err == nil || !strings.Contains(err.Error(), "git rev-parse") {
		t.Fatalf("a failing command: err = %v", err)
	}
}

// Without the repository's worktree records there is no linked worktree,
// and git is not asked: the listing is what made every hook slow. With
// them, git is asked, as a cancelled context shows.
func TestOtherWorktreesAsksGitOnlyWhenThereAreRecords(t *testing.T) {
	single := newRepo(t)
	write(t, single, "a.txt", "a")
	commitAll(t, single, "base")
	linked, _, _ := worktreeRepo(t)
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	for name, tt := range map[string]struct {
		dir     string
		wantErr string
	}{
		"no records": {dir: single},
		"records":    {dir: linked, wantErr: "list the repository's worktrees"},
	} {
		t.Run(name, func(t *testing.T) {
			repo := open(t, tt.dir)
			if _, err := repo.Identity(t.Context()); err != nil {
				t.Fatal(err)
			}
			got, err := repo.OtherWorktrees(cancelled)
			if got != nil || !errorMatches(err, tt.wantErr) {
				t.Fatalf("OtherWorktrees = %q, %v; want no worktree, error %q", got, err, tt.wantErr)
			}
		})
	}
}

// Records that cannot be read, and a common directory git cannot name,
// fail the listing rather than hide a worktree.
func TestOtherWorktreesFailsClosed(t *testing.T) {
	dir := newRepo(t)
	if err := os.WriteFile(filepath.Join(dir, ".git", "worktrees"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := open(t, dir).OtherWorktrees(t.Context()); !errorMatches(err, "read the repository's worktree records") {
		t.Fatalf("records in a file: err = %v", err)
	}
	gone := &Repository{root: filepath.Join(dir, "gone")}
	if _, err := gone.OtherWorktrees(t.Context()); !errorMatches(err, "find the git directory") {
		t.Fatalf("no repository: err = %v", err)
	}
	if _, err := gone.Identity(t.Context()); !errorMatches(err, "find the git directory") {
		t.Fatalf("no repository: err = %v", err)
	}
}

// A worktree whose path holds a newline makes git's listing ambiguous, so
// it is refused rather than read as other worktrees.
func TestOtherWorktreesRefusesAPathWithANewline(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows file names cannot hold a newline")
	}
	main, _, _ := worktreeRepo(t)
	gitT(t, main, "worktree", "add", "-q", "-b", "nl", filepath.Join(main, ".claude", "worktrees", "a\nHEAD 0\n\nworktree /elsewhere"))
	_, err := open(t, main).OtherWorktrees(t.Context())
	if !errorMatches(err, "cannot read git's list of worktrees unambiguously") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseWorktrees(t *testing.T) {
	lines := func(s ...string) string { return strings.Join(s, "\n") + "\n" }
	main := filepath.Join(t.TempDir(), "main")
	linked := filepath.Join(main, "w")
	tests := []struct {
		name     string
		out      string
		recorded int
		want     []string
		wantErr  string
	}{
		{"main and a linked one", lines("worktree "+main, "HEAD abc", "branch refs/heads/main", "",
			"worktree "+linked, "HEAD def", "detached", `locked "reason\nworktree /elsewhere"`, "prunable gitdir file points to non-existent location", ""), 2, []string{main, linked}, ""},
		{"a bare main checkout", lines("worktree "+main, "bare", "", "worktree "+linked, "HEAD def", "locked", ""), 2, []string{main, linked}, ""},
		{"a detached HEAD without a branch line", lines("worktree "+main, "HEAD abc", "prunable", ""), 1, []string{main}, ""},
		{"a newline in a path", lines("worktree "+main, "HEAD abc", "", "worktree "+linked, "x", "HEAD def", ""), 2, nil, `"x" does not belong after a "worktree" line`},
		{"a path that reads as an attribute", lines("worktree "+linked, "HEAD 0", "HEAD def", "branch refs/heads/w", ""), 2, nil, `"HEAD def" does not belong after a "HEAD" line`},
		{"a path that reads as a whole record", lines("worktree "+main, "HEAD abc", "", "worktree "+linked, "HEAD 0", "", "worktree /elsewhere", "HEAD def", ""), 2, nil,
			"it names 3 worktrees, and git records 2"},
		{"an unfinished record", lines("worktree "+main, "HEAD abc"), 1, nil, `ends inside a record, after a "HEAD" line`},
		{"no final newline", "worktree " + main, 1, nil, "git listed no worktree"},
		{"a relative path", lines("worktree main", "HEAD abc", ""), 1, nil, "not an absolute path"},
		{"nothing", "", 1, nil, "git listed no worktree"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseWorktrees([]byte(tt.out), tt.recorded)
			if !slices.Equal(got, tt.want) || !errorMatches(err, tt.wantErr) {
				t.Fatalf("parseWorktrees = %q, %v; want %q, error %q", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

// errorMatches reports whether err is nil when want is empty, and otherwise
// mentions want.
func errorMatches(err error, want string) bool {
	if want == "" {
		return err == nil
	}
	return err != nil && strings.Contains(err.Error(), want)
}
