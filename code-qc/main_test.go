package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"millennium-code-qc/internal/interface/cli"
)

// These tests run the engine as main wires it, on real git repositories in
// temporary directories, isolated from the user's git config and the
// engine's own state.

func isolate(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("git is required for these tests: %v", err)
	}
	for name, value := range map[string]string{
		"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME": "test", "GIT_AUTHOR_EMAIL": "test@example.invalid",
		"GIT_COMMITTER_NAME": "test", "GIT_COMMITTER_EMAIL": "test@example.invalid",
		"MILLENNIUM_LOCAL_DIR": t.TempDir(), "MILLENNIUM_STATE_DIR": t.TempDir(),
	} {
		t.Setenv(name, value)
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// optedInRepo is an opted-in repository with a linked worktree that still
// exists (other) and one removed while a session was in it (gone), both
// under .claude/worktrees as Claude Code makes them.
func optedInRepo(t *testing.T) (root, other, gone string) {
	t.Helper()
	isolate(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, root, "init", "-q", "-b", "main")
	writeFile(t, root, ".millennium/config.json", `{"version": 1, "languages": ["go"]}`)
	writeFile(t, root, ".gitignore", ".claude/worktrees/\n")
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "commit", "-q", "-m", "base")
	other = filepath.Join(root, ".claude", "worktrees", "other")
	gone = filepath.Join(root, ".claude", "worktrees", "gone")
	gitIn(t, root, "worktree", "add", "-q", "-b", "other", other)
	gitIn(t, root, "worktree", "add", "-q", "-b", "gone", gone)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	return root, other, gone
}

// runHook sends one event to the engine as main wires it.
func runHook(t *testing.T, event map[string]any) (int, string) {
	t.Helper()
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := cli.New("test", bytes.NewReader(data), &stdout, &stderr, buildDeps).Run(t.Context(), []string{"hook"})
	return code, stdout.String() + stderr.String()
}

// hookCase is one hook event and what the engine must answer.
type hookCase struct {
	name     string
	event    map[string]any
	wantCode int
	want     string // "" means no output at all
}

// A session whose worktree was removed is evaluated in the main checkout,
// with every protection in place: the main checkout's rules, and those of
// every other worktree. An agent in a linked worktree cannot write the main
// checkout's rules either, nor a case variant of them.
func TestHookProtectsEveryWorktree(t *testing.T) {
	root, other, gone := optedInRepo(t)
	edit := func(cwd, tool, path string) map[string]any {
		return map[string]any{"hook_event_name": "PreToolUse", "cwd": cwd, "tool_name": tool, "tool_input": map[string]any{"file_path": path}}
	}
	denied := `"permissionDecision":"deny"`
	tests := []hookCase{
		{name: "cd back into the repository from the removed worktree",
			event: map[string]any{"hook_event_name": "PreToolUse", "cwd": gone, "tool_name": "Bash", "tool_input": map[string]any{"command": "cd " + root}}},
		{name: "the main checkout's config from the removed worktree", event: edit(gone, "Edit", filepath.Join(root, ".millennium", "config.json")), want: denied},
		{name: "another worktree's config from the removed worktree", event: edit(gone, "Write", filepath.Join(other, ".millennium", "config.json")), want: denied},
		{name: "the main checkout's config from a linked worktree", event: edit(other, "Write", filepath.Join(root, ".millennium", "config.json")), want: denied},
		{name: "a case variant of the config", event: edit(other, "Write", filepath.Join(other, ".MILLENNIUM", "Config.json")), want: denied},
		{name: "code in another worktree", event: edit(other, "Write", filepath.Join(root, "main.go"))},
		{name: "a path with '..' from the removed worktree", event: edit(gone, "Edit", strings.Join([]string{gone, "..", "..", "..", ".millennium", "config.json"}, string(filepath.Separator))),
			wantCode: 2, want: "has a '..' part"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, out := runHook(t, tt.event)
			if code != tt.wantCode || tt.want == "" && out != "" || !strings.Contains(out, tt.want) {
				t.Fatalf("exit %d (want %d), output %q, want %q", code, tt.wantCode, out, tt.want)
			}
		})
	}
}

// A symlink in the repository does not reach the local configs: the path is
// resolved before it is compared.
func TestHookProtectsTheLocalConfigsThroughASymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks on Windows needs a privilege tests do not have")
	}
	root, _, _ := optedInRepo(t)
	if err := os.Symlink(os.Getenv("MILLENNIUM_LOCAL_DIR"), filepath.Join(root, "lclink")); err != nil {
		t.Fatal(err)
	}
	code, out := runHook(t, map[string]any{"hook_event_name": "PreToolUse", "cwd": root, "tool_name": "Write",
		"tool_input": map[string]any{"file_path": filepath.Join(root, "lclink", "abc", "config.json")}})
	if code != 0 || !strings.Contains(out, "local configs are protected") {
		t.Fatalf("exit %d, output %q", code, out)
	}
}

// The Stop hook in a removed worktree runs the gates of the main checkout
// and blocks while they fail.
func TestStopInARemovedWorktreeRunsTheGates(t *testing.T) {
	root, _, gone := optedInRepo(t)
	writeFile(t, root, "go.mod", "module example.invalid/a\n\ngo 1.22\n")
	writeFile(t, root, "a.go", "package a\n\n// "+"FIX"+"ME: finish this\nfunc A() {}\n")
	code, out := runHook(t, map[string]any{"hook_event_name": "Stop", "cwd": gone, "session_id": "s"})
	if code != 0 || !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, "todo-without-link") {
		t.Fatalf("exit %d, output %q", code, out)
	}
}

// openRepository reports a directory that does not exist and one outside
// any repository as the CLI's errors, which the hooks act on, and names the
// directory once.
func TestOpenRepositoryReportsTheCLIErrors(t *testing.T) {
	isolate(t)
	outside := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(outside))
	missing := filepath.Join(outside, "gone")
	tests := []struct {
		dir  string
		want error
	}{
		{missing, cli.ErrNoDirectory},
		{outside, cli.ErrNoRepository},
	}
	for _, tt := range tests {
		_, err := openRepository(t.Context(), tt.dir)
		if !errors.Is(err, tt.want) || err.Error() != tt.dir+": "+tt.want.Error() {
			t.Errorf("openRepository(%q): err = %v, want %q", tt.dir, err, tt.dir+": "+tt.want.Error())
		}
	}
}

// A path outside the tree is the CLI's ErrOutsideRepository, which the
// guard skips; any other mapping failure is not.
func TestPathMappersReportOutsideAsTheCLIError(t *testing.T) {
	root, _, _ := optedInRepo(t)
	repo, err := openRepository(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(filepath.Dir(root), "elsewhere.go")
	if _, err := mapPath(root, elsewhere); !errors.Is(err, cli.ErrOutsideRepository) {
		t.Errorf("mapPath outside: err = %v", err)
	}
	if _, err := repoPath(repo)(elsewhere); !errors.Is(err, cli.ErrOutsideRepository) {
		t.Errorf("repoPath outside: err = %v", err)
	}
	if _, err := mapPath(root, "relative.go"); err == nil || errors.Is(err, cli.ErrOutsideRepository) {
		t.Errorf("mapPath of a relative path: err = %v, want a failure other than outside", err)
	}
	if p, err := mapPath(root, filepath.Join(root, "a.go")); err != nil || p != "a.go" {
		t.Errorf("mapPath inside: %q, %v", p, err)
	}
}
