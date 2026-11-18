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

// A worktree moved with plain mv drops out of git's list: its rules stay
// protected all the same, since a protected path is matched wherever it
// lies. One whose path holds a newline cannot be listed unambiguously, and
// every call that needs the list blocks while it is ambiguous: the role
// rules, and every write, whose protected locations in each worktree may
// lead where it lands.
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
	movedConfig := filepath.Join(moved, ".millennium", "config.json")
	assertHookCases(t, []hookCase{
		{name: "a moved worktree's config from the main checkout", event: toolEvent(root, "Write", "", movedConfig), want: denied},
		{name: "a moved worktree's config from the path it left", event: toolEvent(w2, "Write", "", movedConfig), want: denied},
		{name: "a moved worktree's settings, in another case", event: toolEvent(root, "Edit", "", filepath.Join(moved, ".Claude", "settings.json")), want: denied},
		{name: "code in the moved worktree", event: toolEvent(root, "Write", "", filepath.Join(moved, "main.go"))},
	})
	newline := filepath.Join(root, ".claude", "worktrees", "a\nb")
	gitIn(t, root, "worktree", "add", "-q", "-b", "nl", newline)
	ambiguous := "cannot read git's list of worktrees unambiguously"
	assertHookCases(t, []hookCase{
		{name: "the config of a worktree named with a newline", event: toolEvent(root, "Edit", "", filepath.Join(newline, ".millennium", "config.json")), wantCode: 2, want: ambiguous},
		{name: "a role call while the list is ambiguous", event: toolEvent(root, "Read", "millennium:qa-tester", filepath.Join(root, "README.md")), wantCode: 2, want: ambiguous},
		{name: "a write while the list is ambiguous", event: toolEvent(root, "Write", "", filepath.Join(moved, "main.go")), wantCode: 2, want: ambiguous},
		{name: "a read, which needs no list", event: toolEvent(root, "Read", "", filepath.Join(root, "README.md"))},
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

// symlink makes link lead to target, a directory it creates.
func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := errors.Join(os.MkdirAll(target, 0o755), os.Symlink(target, link)); err != nil {
		t.Fatal(err)
	}
}

// A protected location reached through a symlink is protected by its real
// path too: with `.pi` a symlink to `conf`, pi loads `conf/extensions` as
// its extensions. This holds for every write, the main session's and
// qa-tester's included, in the acting checkout and in every other
// worktree, whichever checkout the session acts in.
func TestHookProtectsLocationsReachedThroughASymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks on Windows needs a privilege tests do not have")
	}
	root, other, _ := optedInRepo(t)
	symlink(t, filepath.Join(root, "conf"), filepath.Join(root, ".pi"))
	symlink(t, filepath.Join(other, "cl"), filepath.Join(other, ".claude"))
	if err := os.Rename(filepath.Join(other, ".millennium"), filepath.Join(other, "m2")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(other, "m2"), filepath.Join(other, ".millennium")); err != nil {
		t.Fatal(err)
	}
	engineer, qa := "millennium:software-engineer", "millennium:qa-tester"
	assertHookCases(t, []hookCase{
		{name: "a pi extension by its real path", event: toolEvent(root, "Write", "", filepath.Join(root, "conf", "extensions", "evil.ts")), want: denied},
		{name: "pi's settings by its real path", event: toolEvent(root, "Edit", "", filepath.Join(root, "conf", "settings.json")), want: denied},
		{name: "a file beside them", event: toolEvent(root, "Write", "", filepath.Join(root, "conf", "notes.md"))},
		{name: "Claude settings by their real path, in the acting worktree", event: toolEvent(other, "Write", "", filepath.Join(other, "cl", "settings.json")), want: denied},
		{name: "another worktree's config by its real path, for an engineer", event: toolEvent(root, "Write", engineer, filepath.Join(other, "m2", "config.json")), want: denied},
		{name: "another worktree's config by its real path, for the main session", event: toolEvent(root, "Write", "", filepath.Join(other, "m2", "config.json")), want: denied},
		{name: "another worktree's Claude settings by their real path, for qa-tester", event: toolEvent(root, "Edit", qa, filepath.Join(other, "cl", "settings.local.json")), want: denied},
		{name: "the main checkout's pi extension by its real path, from a linked worktree's main session", event: toolEvent(other, "Write", "", filepath.Join(root, "conf", "extensions", "evil.ts")), want: denied},
		{name: "the main checkout's pi extension by its .pi path, from a linked worktree", event: toolEvent(other, "Write", "", filepath.Join(root, ".pi", "extensions", "evil.ts")), want: denied},
	})
}

// A protected location that leads nowhere blocks nothing: a dangling
// symlink, such as a committed relative link that dangles in a worktree, is
// followed as it reads, and what it would lead to is protected; a location
// below a file reaches nothing. Before, either blocked every write in its
// checkout, and every engineer's write in every worktree.
func TestHookToleratesProtectedLocationsThatLeadNowhere(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks on Windows needs a privilege tests do not have")
	}
	root, other, _ := optedInRepo(t)
	if err := os.Symlink(filepath.Join("..", "conf"), filepath.Join(other, ".pi")); err != nil {
		t.Fatal(err)
	}
	filed := filepath.Join(root, ".claude", "worktrees", "filed")
	gitIn(t, root, "worktree", "add", "-q", "-b", "filed", filed)
	writeFile(t, filed, ".claude", "a file where Claude's directory should be")
	engineer := "millennium:software-engineer"
	assertHookCases(t, []hookCase{
		{name: "a write in the checkout with the dangling link", event: toolEvent(other, "Write", "", filepath.Join(other, "main.go"))},
		{name: "an engineer's write in another worktree", event: toolEvent(root, "Write", engineer, filepath.Join(root, "main.go"))},
		{name: "a write in the checkout with the file", event: toolEvent(filed, "Write", "", filepath.Join(filed, "main.go"))},
		{name: "a pi extension where the dangling link leads", event: toolEvent(root, "Write", "", filepath.Join(filepath.Dir(other), "conf", "extensions", "evil.ts")), want: denied},
		{name: "beside it", event: toolEvent(root, "Write", "", filepath.Join(filepath.Dir(other), "conf", "notes.md"))},
	})
}

// A protected location that is a link loop reaches nothing, since the OS
// opens nothing through it, so it blocks no write: before, one in the
// main checkout blocked every write there and in every agent worktree. A
// tool's path through the loop still blocks.
func TestHookToleratesALinkLoopAsAProtectedLocation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks on Windows needs a privilege tests do not have")
	}
	root, other, _ := optedInRepo(t)
	if err := errors.Join(os.Symlink(".pi", filepath.Join(root, ".pi")), os.Symlink("x", filepath.Join(other, ".claude")), os.Symlink(".claude", filepath.Join(other, "x"))); err != nil {
		t.Fatal(err)
	}
	engineer := "millennium:software-engineer"
	assertHookCases(t, []hookCase{
		{name: "a write in the checkout with the loop", event: toolEvent(root, "Write", "", filepath.Join(root, "main.go"))},
		{name: "an engineer's write in an agent worktree", event: toolEvent(other, "Write", engineer, filepath.Join(other, "main.go"))},
		{name: "a write in the main checkout from the worktree with the loop", event: toolEvent(other, "Edit", "", filepath.Join(root, "docs", "a.md"))},
		{name: "a path through the loop", event: toolEvent(root, "Write", "", filepath.Join(root, ".pi", "x.ts")), wantCode: 2, want: "cannot check the tool's path, so the call is blocked"},
	})
}

// A link into /proc/self, /dev/fd and the like leads somewhere else for
// each process, so the engine cannot know where it leads for pi: the
// location is unknown, and every write blocks until it is removed. Before,
// `other/.pi` a link to /proc/self/cwd/conf was resolved in the engine's
// own working directory, and a write of other/conf/extensions/evil.ts
// passed.
func TestHookBlocksWritesWhileALocationLeadsThroughAPerProcessPath(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/proc/self is Linux's")
	}
	root, other, _ := optedInRepo(t)
	if err := os.Symlink("/proc/self/cwd/conf", filepath.Join(other, ".pi")); err != nil {
		t.Fatal(err)
	}
	unknown := "cannot resolve the protected location " + filepath.ToSlash(filepath.Join(other, ".pi", "settings.json")) + ", so every write is blocked until it can be: resolve /proc/self"
	assertHookCases(t, []hookCase{
		{name: "an extension where the link leads", event: toolEvent(root, "Write", "", filepath.Join(other, "conf", "extensions", "evil.ts")), wantCode: 2, want: unknown},
		{name: "any other write", event: toolEvent(root, "Write", "", filepath.Join(root, "main.go")), wantCode: 2, want: unknown},
		{name: "a read", event: toolEvent(root, "Read", "", filepath.Join(root, "main.go"))},
	})
}

// lock makes dir unreadable and unsearchable, and unlocks it again so
// that t.TempDir can remove it.
func lock(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Errorf("unlock %s: %v", dir, err)
		}
	})
}

// While a protected location of any listed checkout cannot be resolved,
// such as one in a worktree the engine may not search, every write blocks
// with a message naming the location and how to fix it: the location may
// lead anywhere, the main checkout's files included. Before, another
// checkout's unknown location was skipped, so `chmod 000` on a worktree
// whose `.pi` leads to the main checkout's `conf` let a write of
// `conf/extensions/evil.ts` through.
func TestHookBlocksWritesWhileAWorktreesLocationIsUnknown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks on Windows needs a privilege tests do not have")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores the permissions these cases rely on")
	}
	root, other, _ := optedInRepo(t)
	via := filepath.Join(other, "d")
	if err := os.Mkdir(via, 0o755); err != nil {
		t.Fatal(err)
	}
	symlink(t, filepath.Join(root, "conf"), filepath.Join(via, "link"))
	if err := os.Symlink(filepath.Join("d", "link"), filepath.Join(other, ".pi")); err != nil {
		t.Fatal(err)
	}
	evil := filepath.Join(root, "conf", "extensions", "evil.ts")
	assertHookCases(t, []hookCase{{name: "while it can be resolved", event: toolEvent(root, "Write", "", evil), want: denied}})
	fix := "restore access to it (such as with chmod), fix or remove the link it leads through, or, if " + filepath.ToSlash(other) + " is a linked worktree you no longer need, remove it"
	for _, locked := range []string{via, other} {
		t.Run(filepath.Base(locked)+" locked", func(t *testing.T) {
			lock(t, locked)
			assertHookCases(t, []hookCase{
				{name: "an extension where it led", event: toolEvent(root, "Write", "", evil), wantCode: 2, want: "cannot resolve the protected location " + filepath.ToSlash(filepath.Join(other, ".pi"))},
				{name: "a write in the main checkout", event: toolEvent(root, "Write", "", filepath.Join(root, "main.go")), wantCode: 2, want: fix},
				{name: "an engineer's write in the main checkout", event: toolEvent(root, "Edit", "millennium:software-engineer", filepath.Join(root, "main.go")), wantCode: 2, want: fix},
				{name: "a read", event: toolEvent(root, "Read", "", filepath.Join(root, "main.go"))},
			})
		})
	}
}

// On Windows a directory junction, which `mklink /J` makes without any
// privilege, is followed like a symlink, end to end: with `.pi` a junction
// to `conf`, pi loads `conf/extensions`, so a write there is denied, by
// the main session and by an engineer, from the checkout and from a
// linked worktree.
func TestHookFollowsJunctionsOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("directory junctions are Windows'")
	}
	root, other, _ := optedInRepo(t)
	if err := os.MkdirAll(filepath.Join(root, "conf"), 0o755); err != nil {
		t.Fatal(err)
	}
	// millennium:allow banned-construct -- mklink is a cmd built-in with no program of its own; every argument is a constant
	mklink := exec.Command("cmd", "/c", "mklink", "/J", ".pi", "conf")
	mklink.Dir = root
	if out, err := mklink.CombinedOutput(); err != nil {
		t.Fatalf("mklink /J: %v\n%s", err, out)
	}
	engineer := "millennium:software-engineer"
	assertHookCases(t, []hookCase{
		{name: "an extension by its real path", event: toolEvent(root, "Write", "", filepath.Join(root, "conf", "extensions", "x.ts")), want: denied},
		{name: "an engineer's extension by its real path, from a linked worktree", event: toolEvent(other, "Write", engineer, filepath.Join(root, "conf", "extensions", "x.ts")), want: denied},
		{name: "pi's settings by their real path", event: toolEvent(root, "Edit", "", filepath.Join(root, "conf", "settings.json")), want: denied},
		{name: "a file beside them", event: toolEvent(root, "Write", "", filepath.Join(root, "conf", "notes.md"))},
	})
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
