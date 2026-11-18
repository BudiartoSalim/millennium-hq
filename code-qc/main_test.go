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

// denied marks a guard denial in a hook answer.
const denied = `"permissionDecision":"deny"`

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

// toolEvent is a PreToolUse event of a file tool.
func toolEvent(cwd, tool, role, path string) map[string]any {
	return map[string]any{"hook_event_name": "PreToolUse", "cwd": cwd, "tool_name": tool, "agent_type": role, "tool_input": map[string]any{"file_path": path}}
}

// assertHookCases runs each case through the engine as main wires it.
func assertHookCases(t *testing.T, tests []hookCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, out := runHook(t, tt.event)
			if code != tt.wantCode || tt.want == "" && out != "" || !strings.Contains(out, tt.want) {
				t.Fatalf("exit %d (want %d), output %q, want %q", code, tt.wantCode, out, tt.want)
			}
		})
	}
}

// A worktree moved with plain mv drops out of git's list, and one whose
// path holds a newline cannot be listed unambiguously: their rules stay
// protected all the same, since a protected path is matched wherever it
// lies. A role rule, which needs the list, blocks while it is ambiguous.
func TestHookProtectsCheckoutsGitDoesNotList(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows file names cannot hold a newline")
	}
	root, _, _ := optedInRepo(t)
	w2 := filepath.Join(root, ".claude", "worktrees", "w2")
	gitIn(t, root, "worktree", "add", "-q", "-b", "w2", w2)
	moved := filepath.Join(t.TempDir(), "x", "moved")
	if err := errors.Join(os.MkdirAll(filepath.Dir(moved), 0o755), os.Rename(w2, moved)); err != nil {
		t.Fatal(err)
	}
	newline := filepath.Join(root, ".claude", "worktrees", "a\nb")
	gitIn(t, root, "worktree", "add", "-q", "-b", "nl", newline)
	movedConfig := filepath.Join(moved, ".millennium", "config.json")
	assertHookCases(t, []hookCase{
		{name: "a moved worktree's config from the main checkout", event: toolEvent(root, "Write", "", movedConfig), want: denied},
		{name: "a moved worktree's config from the path it left", event: toolEvent(w2, "Write", "", movedConfig), want: denied},
		{name: "a moved worktree's settings, in another case", event: toolEvent(root, "Edit", "", filepath.Join(moved, ".Claude", "settings.json")), want: denied},
		{name: "the config of a worktree named with a newline", event: toolEvent(root, "Edit", "", filepath.Join(newline, ".millennium", "config.json")), want: denied},
		{name: "a role call while the list is ambiguous", event: toolEvent(root, "Read", "millennium:qa-tester", filepath.Join(root, "README.md")),
			wantCode: 2, want: "cannot read git's list of worktrees unambiguously"},
		{name: "code in the moved worktree", event: toolEvent(root, "Write", "", filepath.Join(moved, "main.go"))},
	})
}

// qa-tester reads no implementation code through another worktree's path;
// the documents there stay readable.
func TestHookKeepsQATesterOutOfEveryWorktreesCode(t *testing.T) {
	root, other, _ := optedInRepo(t)
	qa := "millennium:qa-tester"
	assertHookCases(t, []hookCase{
		{name: "another worktree's code from the main checkout", event: toolEvent(root, "Read", qa, filepath.Join(other, "main.go")), want: "main.go is implementation code"},
		{name: "the main checkout's code from a linked worktree", event: toolEvent(other, "Read", qa, filepath.Join(root, "src", "a.go")), want: "src/a.go is implementation code"},
		{name: "another worktree's code in another case", event: toolEvent(root, "Read", qa, filepath.Join(other, "SRC", "A.go")), want: "SRC/A.go is implementation code"},
		{name: "another worktree's spec", event: toolEvent(root, "Read", qa, filepath.Join(other, "docs", "spec.md"))},
	})
}

// breakWorktreesDir replaces the directory that holds root's linked
// worktrees with what replace leaves at its path.
func breakWorktreesDir(t *testing.T, root string, replace func(path string) error) {
	t.Helper()
	dir := filepath.Join(root, ".claude", "worktrees")
	if err := errors.Join(os.Rename(dir, dir+".old"), replace(dir)); err != nil {
		t.Fatal(err)
	}
}

// A worktree whose root cannot be resolved, its parent replaced by a
// dangling symlink or a file, blocks no call: it is compared as git names
// it, and a repository that is not opted in never lists its worktrees.
func TestHookToleratesAWorktreeThatCannotBeResolved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks on Windows needs a privilege tests do not have")
	}
	dangling := func(path string) error { return os.Symlink(filepath.Join(filepath.Dir(path), "nowhere"), path) }
	file := func(path string) error { return os.WriteFile(path, []byte("not a directory"), 0o644) }
	engineer := "millennium:software-engineer"
	for name, replace := range map[string]func(string) error{"a dangling symlink": dangling, "a file": file} {
		t.Run(name, func(t *testing.T) {
			root, other, _ := optedInRepo(t)
			breakWorktreesDir(t, root, replace)
			assertHookCases(t, []hookCase{
				{name: "a read", event: toolEvent(root, "Read", "", filepath.Join(root, "main.go"))},
				{name: "an engineer's write", event: toolEvent(root, "Write", engineer, filepath.Join(root, "main.go"))},
				{name: "an acceptance test of the main checkout", event: toolEvent(root, "Write", engineer, filepath.Join(root, "tests", "acceptance", "a_test.go")),
					want: "is an acceptance test"},
				{name: "a path into the broken worktree, which cannot be checked", event: toolEvent(root, "Read", "", filepath.Join(other, "main.go"))},
				{name: "a write there, which cannot be checked", event: toolEvent(root, "Write", "", filepath.Join(other, "main.go")),
					wantCode: 2, want: "cannot check the tool's path, so the call is blocked"},
			})
			plain := filepath.Join(t.TempDir(), "plain")
			gitIn(t, filepath.Dir(plain), "init", "-q", "-b", "main", plain)
			gitIn(t, plain, "commit", "-q", "--allow-empty", "-m", "base")
			gitIn(t, plain, "worktree", "add", "-q", "-b", "w", filepath.Join(plain, ".claude", "worktrees", "w"))
			breakWorktreesDir(t, plain, replace)
			assertHookCases(t, []hookCase{{name: "not opted in", event: toolEvent(plain, "Write", engineer, filepath.Join(plain, "main.go"))}})
		})
	}
}

// The hooks compare absolute paths, so a relative local config directory
// is refused rather than compared with nothing.
func TestLocalConfigNeedsAnAbsoluteDirectory(t *testing.T) {
	root, _, _ := optedInRepo(t)
	t.Setenv("MILLENNIUM_LOCAL_DIR", filepath.Join("relative", "dir"))
	code, out := runHook(t, toolEvent(root, "Write", "", filepath.Join(root, "main.go")))
	if code != 2 || !strings.Contains(out, "MILLENNIUM_LOCAL_DIR is "+filepath.Join("relative", "dir")+", which is not an absolute path") {
		t.Fatalf("exit %d, output %q", code, out)
	}
}
