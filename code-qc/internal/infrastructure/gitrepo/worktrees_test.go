package gitrepo

import (
	"os"
	"path/filepath"
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

func TestParseWorktrees(t *testing.T) {
	line := func(s string) string { return s + "\n" }
	main := filepath.Join(t.TempDir(), "main")
	linked := filepath.Join(main, "w")
	tests := []struct {
		name    string
		out     string
		want    []string
		wantErr string
	}{
		{"main and a linked one", line("worktree "+main) + line("HEAD abc") + line("branch refs/heads/main") + line("") +
			line("worktree "+linked) + line("HEAD def") + line("detached") + line(`locked "reason\nworktree /elsewhere"`) + line(""), []string{main, linked}, ""},
		{"listed twice", line("worktree "+main) + line("") + line("worktree "+main+string(filepath.Separator)) + line(""), []string{main}, ""},
		{"a newline in a path", line("worktree "+main) + line("") + line("worktree "+linked) + line("x") + line(""), []string{main, linked}, ""},
		{"a relative path", line("worktree main") + line(""), nil, "not an absolute path"},
		{"nothing", "", nil, "git listed no worktree"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseWorktrees([]byte(tt.out))
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
