package cli

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"millennium-code-qc/internal/domain"
)

// fakeResolver is a fake Deps.ResolvePath and Deps.ResolveLocation, which
// it is asked a location below a root as: a path in resolved resolves as
// given ("" a location that reaches nothing), one in errs fails as given,
// and any other resolves to itself. It records what it was asked.
type fakeResolver struct {
	resolved map[string]string
	errs     map[string]error
	asked    []string
}

func (r *fakeResolver) resolve(abs string) (string, error) {
	r.asked = append(r.asked, abs)
	if err := r.errs[abs]; err != nil {
		return "", err
	}
	if p, ok := r.resolved[abs]; ok {
		return p, nil
	}
	return filepath.Clean(abs), nil
}

func (r *fakeResolver) location(root, protected string) (string, error) {
	return r.resolve(filepath.Join(root, filepath.FromSlash(protected)))
}

// fakeWorktrees is a fake Deps.OtherWorktrees that counts the listings.
type fakeWorktrees struct {
	roots  []string
	err    error
	listed int
}

func (w *fakeWorktrees) list(context.Context) ([]string, error) {
	w.listed++
	return w.roots, w.err
}

// domainGuard decides as the guard use case does: nothing outside an
// opted-in repository, the domain's guard inside, so a hook test sees the
// real decision on the paths the hook located.
type domainGuard struct{ config fakeConfig }

func (g domainGuard) Execute(_ context.Context, call domain.ToolCall) domain.GuardDecision {
	if !g.config.enabled {
		return domain.GuardDecision{}
	}
	return domain.Guard(g.config.cfg, call)
}

// roleConfig is an opted-in config with one component, src, and the
// default acceptance tests, tests/acceptance.
func roleConfig(t *testing.T) fakeConfig {
	t.Helper()
	cfg, err := domain.NewConfig(domain.ConfigInput{Version: ptr(1), Components: []domain.ComponentInput{{Paths: []string{"src"}, Languages: []string{"go"}}}})
	if err != nil {
		t.Fatal(err)
	}
	return fakeConfig{cfg: cfg, enabled: true}
}

// preToolUseIn sends one PreToolUse event, in cwd, to an App over deps and
// returns the exit status and everything it printed.
func preToolUseIn(t *testing.T, deps Deps, cwd, tool, role string, input map[string]any) (int, string) {
	t.Helper()
	factory := func(context.Context, string) (Deps, error) { return deps, nil }
	var stdout, stderr bytes.Buffer
	stdin := strings.NewReader(hookEvent(map[string]any{"hook_event_name": "PreToolUse", "cwd": cwd, "tool_name": tool, "tool_input": input, "agent_type": role}))
	code := New("1.2.3", stdin, &stdout, &stderr, factory).Run(t.Context(), []string{"hook"})
	return code, stdout.String() + stderr.String()
}

func filePath(p string) map[string]any { return map[string]any{"file_path": p} }

const denied = `"permissionDecision":"deny"`

// assertHookAnswer checks an answer: allowed with no output when want is
// "", otherwise output holding want; denied only for a decision.
func assertHookAnswer(t *testing.T, code int, out string, wantCode int, want string) {
	t.Helper()
	if code != wantCode || want == "" && out != "" || !strings.Contains(out, want) {
		t.Fatalf("exit %d (want %d), output %q, want %q", code, wantCode, out, want)
	}
}

// guardedCase is one Edit by the main session, whose path the hook
// resolves for the guard.
type guardedCase struct {
	name     string
	cwd      string
	path     string
	resolved map[string]string
	errs     map[string]error
	want     []string
	asked    []string
	wantCode int
	wantErr  string
}

// The guard sees each path a tool names both as named and with symlinks
// resolved, never through the process's working directory. A path that
// cannot be resolved blocks the call, and so does one with a ".." part,
// which the OS takes after following a symlink while text cleaning takes it
// before.
func TestPreToolUseResolvesPathsForTheGuard(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "repo")
	protected := filepath.Join(root, ".millennium", "config.json")
	alias := filepath.Join(root, "alias.json")
	gone := filepath.Join(root, "worktrees", "agent")
	sep := string(filepath.Separator)
	slash := filepath.ToSlash
	tests := []guardedCase{
		{name: "a relative path without '..' joins the removed cwd", cwd: gone, path: filepath.Join("src", "new.go"),
			want: []string{slash(filepath.Join(gone, "src", "new.go"))}, asked: slices.Concat([]string{filepath.Join(gone, "src", "new.go"), root}, locationsOf(root))},
		{name: "a relative path with '..' blocks", cwd: gone, path: ".." + sep + ".." + sep + filepath.Join(".millennium", "config.json"),
			wantCode: 2, wantErr: "has a '..' part, which the hook cannot check"},
		{name: "an absolute path with '..' blocks", cwd: root, path: filepath.Join(parent, "l") + sep + ".." + sep + filepath.Join(".millennium", "config.json"),
			wantCode: 2, wantErr: "name the path without '..', such as its absolute path"},
		{name: "a path is resolved once", cwd: root, path: protected, want: []string{slash(protected)}, asked: slices.Concat([]string{protected, root}, locationsOf(root))},
		{name: "a symlink is checked as named and as resolved", cwd: root, path: alias,
			resolved: map[string]string{alias: protected}, want: []string{slash(alias), slash(protected)}, asked: slices.Concat([]string{alias, root}, locationsOf(root))},
		{name: "a path that cannot be resolved blocks", cwd: root, path: protected,
			errs:  map[string]error{protected: errors.New("inspect " + protected + ": permission denied")},
			asked: []string{protected}, wantCode: 2, wantErr: "cannot check the tool's path, so the call is blocked: inspect"},
		{name: "a relative cwd blocks", cwd: filepath.Join("relative", "dir"), path: "a.go",
			wantCode: 2, wantErr: "is not absolute"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &fakeResolver{resolved: tt.resolved, errs: tt.errs}
			guard := &fakeGuard{}
			deps := Deps{Guard: guard, Root: root, Config: fakeConfig{enabled: true}, ResolvePath: resolver.resolve, ResolveLocation: resolver.location, OtherWorktrees: noWorktrees}
			code, out := preToolUseIn(t, deps, tt.cwd, "Edit", "", filePath(tt.path))
			if code != tt.wantCode || !strings.Contains(out, tt.wantErr) || !slices.Equal(resolver.asked, tt.asked) {
				t.Fatalf("exit %d (want %d), asked %q (want %q), output %q (want %q)", code, tt.wantCode, resolver.asked, tt.asked, out, tt.wantErr)
			}
			if !slices.Equal(guard.got.Targets, tt.want) || guard.got.Paths != nil {
				t.Fatalf("the guard got %+v, want targets %q and no paths", guard.got, tt.want)
			}
		})
	}
}

// worktreeCase is one PreToolUse event in a repository with several
// worktrees.
type worktreeCase struct {
	name      string
	root      string
	worktrees []string
	listErr   error
	errs      map[string]error
	tool      string
	role      string
	path      string
	want      string // "" means allowed
	wantCode  int
}

// The role rules apply in every worktree of the repository, with the acting
// session's config: a path is placed in the worktree it lies in, the
// innermost one, since Claude Code's worktrees lie inside the main
// checkout. The protected paths need no listing: they are protected
// wherever they lie.
func TestPreToolUseAppliesRoleRulesInEveryWorktree(t *testing.T) {
	main := filepath.Join(t.TempDir(), "repo")
	linked := filepath.Join(main, ".claude", "worktrees", "agent")
	other := filepath.Join(main, ".claude", "worktrees", "other")
	tests := []worktreeCase{
		{name: "another worktree's acceptance test", root: linked, worktrees: []string{main, other}, tool: "Write", role: "software-engineer",
			path: filepath.Join(other, "tests", "acceptance", "a_test.go"), want: "is an acceptance test"},
		{name: "the main checkout's acceptance test", root: linked, worktrees: []string{main, other}, tool: "Edit", role: "millennium:software-engineer",
			path: filepath.Join(main, "Tests", "Acceptance", "a_test.go"), want: "is an acceptance test"},
		{name: "code in the engineer's own worktree", root: linked, worktrees: []string{main, other}, tool: "Edit", role: "software-engineer",
			path: filepath.Join(linked, "src", "a.go")},
		{name: "qa-tester reads another worktree's code", root: main, worktrees: []string{linked, other}, tool: "Read", role: "qa-tester",
			path: filepath.Join(other, "src", "a.go"), want: "src/a.go is implementation code"},
		{name: "qa-tester reads its own acceptance tests in a nested worktree", root: linked, worktrees: []string{main}, tool: "Read", role: "qa-tester",
			path: filepath.Join(linked, "tests", "acceptance", "a_test.go")},
		{name: "qa-tester searches a directory that holds worktrees", root: main, worktrees: []string{linked}, tool: "Grep", role: "qa-tester",
			path: filepath.Join(main, ".claude"), want: "the repository root is implementation code"},
		{name: "a moved worktree's config, not listed", root: main, worktrees: []string{linked}, tool: "Write",
			path: filepath.Join(filepath.Dir(main), "moved", ".millennium", "config.json"), want: ".millennium is protected"},
		{name: "a worktree whose root cannot be resolved is compared as named", root: main, worktrees: []string{other},
			errs: map[string]error{other: errors.New("resolve " + other + ": no such file or directory")}, tool: "Write", role: "software-engineer",
			path: filepath.Join(other, "tests", "acceptance", "a_test.go"), want: "is an acceptance test"},
		{name: "worktrees that cannot be listed block a role call", root: main, listErr: errors.New("cannot read git's list of worktrees unambiguously"),
			tool: "Read", role: "qa-tester", path: filepath.Join(main, "docs", "a.md"), wantCode: 2,
			want: "cannot check the tool's path in every worktree of the repository, so the call is blocked: cannot read git's list"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &fakeResolver{errs: tt.errs}
			listing := &fakeWorktrees{roots: tt.worktrees, err: tt.listErr}
			config := roleConfig(t)
			deps := Deps{Guard: domainGuard{config}, Config: config, Root: tt.root, ResolvePath: resolver.resolve, ResolveLocation: resolver.location, OtherWorktrees: listing.list}
			input := filePath(tt.path)
			if tt.tool == "Grep" {
				input = map[string]any{"pattern": "x", "path": tt.path}
			}
			code, out := preToolUseIn(t, deps, tt.root, tt.tool, tt.role, input)
			assertHookAnswer(t, code, out, tt.wantCode, tt.want)
			if denial := strings.Contains(out, denied); denial != (tt.want != "" && tt.wantCode == 0) {
				t.Fatalf("denied %v, output %q", denial, out)
			}
		})
	}
}

// The worktrees are listed only for a call a path rule applies to, in an
// opted-in repository: every write, whose protected locations in every
// worktree may lead where it lands, and qa-tester's reads. Listing costs a
// tenth of a second on some file systems, and a broken worktree must not
// block calls where no rule needs it.
func TestPreToolUseListsWorktreesOnlyForPathRules(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	tests := []struct {
		name       string
		config     fakeConfig
		tool, role string
		listed     int
	}{
		{"an engineer's write", roleConfig(t), "Write", "software-engineer", 1},
		{"qa-tester's read", roleConfig(t), "Glob", "qa-tester", 1},
		{"the main session's write", roleConfig(t), "Write", "", 1},
		{"the main session's read", roleConfig(t), "Read", "", 0},
		{"qa-tester's write", roleConfig(t), "Edit", "qa-tester", 1},
		{"a Bash command", roleConfig(t), "Bash", "", 0},
		{"an engineer's read", roleConfig(t), "Read", "software-engineer", 0},
		{"a repository that is not opted in", fakeConfig{}, "Write", "software-engineer", 0},
		{"a config that cannot be read", fakeConfig{err: errors.New("bad json")}, "Read", "qa-tester", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listing := &fakeWorktrees{err: errors.New("listed")}
			deps := Deps{Guard: domainGuard{tt.config}, Config: tt.config, Root: root, ResolvePath: resolveAsNamed, ResolveLocation: locateAsNamed, OtherWorktrees: listing.list}
			code, out := preToolUseIn(t, deps, root, tt.tool, tt.role, filePath(filepath.Join(root, "docs", "a.md")))
			if listing.listed != tt.listed || code != 2*tt.listed || tt.listed == 0 && out != "" {
				t.Fatalf("listed %d times (want %d), exit %d, output %q", listing.listed, tt.listed, code, out)
			}
		})
	}
}

// Each path the call names, each root and each protected location of a
// root is resolved once per call, and then compared in memory: resolving
// costs milliseconds on some file systems, once per root per path was too
// slow. Every write resolves the protected locations of every worktree; a
// read, which no protected path rule applies to, resolves none.
func TestPreToolUseResolvesEachPathOnce(t *testing.T) {
	main := filepath.Join(t.TempDir(), "repo")
	others := []string{filepath.Join(main, "w", "a"), filepath.Join(main, "w", "b"), filepath.Join(main, "w", "c")}
	roots := append([]string{main}, others...)
	target := filepath.Join(others[1], "docs", "x.md")
	for _, tt := range []struct {
		tool, role string
		want       []string
	}{
		{"Write", "software-engineer", slices.Concat([]string{target}, roots, locationsOf(roots...))},
		{"Read", "qa-tester", slices.Concat([]string{target}, roots)},
		{"Edit", "", slices.Concat([]string{target}, roots, locationsOf(roots...))},
		{"Write", "qa-tester", slices.Concat([]string{target}, roots, locationsOf(roots...))},
	} {
		resolver := &fakeResolver{}
		config := roleConfig(t)
		deps := Deps{Guard: domainGuard{config}, Config: config, Root: main, ResolvePath: resolver.resolve, ResolveLocation: resolver.location, OtherWorktrees: (&fakeWorktrees{roots: others}).list}
		if code, out := preToolUseIn(t, deps, main, tt.tool, tt.role, filePath(target)); code != 0 || out != "" {
			t.Fatalf("%s by %q: exit %d, output %q", tt.tool, tt.role, code, out)
		}
		if !slices.Equal(resolver.asked, tt.want) {
			t.Fatalf("%s by %q: resolved %q, want each of %q once", tt.tool, tt.role, resolver.asked, tt.want)
		}
	}
}

// locationsOf are the protected locations of each root, in the order the
// hook resolves them.
func locationsOf(roots ...string) []string {
	var locations []string
	for _, root := range roots {
		for _, protected := range domain.ProtectedPaths() {
			locations = append(locations, filepath.Join(root, filepath.FromSlash(protected)))
		}
	}
	return locations
}

// symlinkCase is one write in a repository whose protected locations the
// resolver may lead elsewhere.
type symlinkCase struct {
	name     string
	config   fakeConfig
	role     string
	path     string
	resolved map[string]string
	errs     map[string]error
	wantCode int
	want     string // "" means allowed, with no output
}

// A protected location reached through a symlink is protected by its real
// path too: with `.pi` a symlink to `conf`, pi loads `conf/extensions` as
// its extensions, so a write there is a write of `.pi/extensions`. This
// holds for every write, whoever makes it, in the acting checkout and in
// every other worktree; it needs an opted-in repository, as every guard
// rule. A location whose lead is unknown, in any checkout, blocks every
// write, wherever it lands, with a message naming the location and how to
// fix it: a link from any worktree may lead anywhere. One that reaches
// nothing, and one in a worktree whose root cannot be resolved, protect
// no other spelling and block nothing.
func TestPreToolUseProtectsLocationsReachedThroughSymlinks(t *testing.T) {
	main := filepath.Join(t.TempDir(), "repo")
	other := filepath.Join(main, ".claude", "worktrees", "other")
	conf := filepath.Join(main, "conf")
	elsewhere := filepath.Join(t.TempDir(), "m")
	in := func(root, protected string) string { return filepath.Join(root, filepath.FromSlash(protected)) }
	piThroughConf := map[string]string{in(main, ".pi/extensions"): filepath.Join(conf, "extensions"), in(main, ".pi/settings.json"): filepath.Join(conf, "settings.json")}
	otherPiThroughConf := map[string]string{in(other, ".pi/extensions"): filepath.Join(other, "conf", "extensions")}
	otherPiUnknown := map[string]error{in(other, ".pi/extensions"): errors.New("inspect " + in(other, ".pi") + ": permission denied")}
	tests := []symlinkCase{
		{name: "another worktree's extension by the real path, for the main session", config: roleConfig(t), path: filepath.Join(other, "conf", "extensions", "evil.ts"),
			resolved: otherPiThroughConf, want: ".pi/extensions is protected"},
		{name: "another worktree's extension by the real path, for qa-tester", config: roleConfig(t), role: "qa-tester", path: filepath.Join(other, "conf", "extensions", "evil.ts"),
			resolved: otherPiThroughConf, want: ".pi/extensions is protected"},
		{name: "another worktree's unknown location, writing in the acting checkout", config: roleConfig(t), path: filepath.Join(main, "a.go"), errs: otherPiUnknown, wantCode: 2,
			want: "cannot resolve the protected location " + filepath.ToSlash(in(other, ".pi/extensions")) + ", so every write is blocked until it can be: inspect"},
		{name: "another worktree's unknown location, writing there", config: roleConfig(t), path: filepath.Join(other, "a.go"), errs: otherPiUnknown, wantCode: 2,
			want: "cannot resolve the protected location " + filepath.ToSlash(in(other, ".pi/extensions")) + ", so every write is blocked until it can be: inspect"},
		{name: "another worktree's unknown location, writing outside every checkout, for qa-tester", config: roleConfig(t), role: "qa-tester", path: filepath.Join(elsewhere, "a.go"),
			errs: otherPiUnknown, wantCode: 2, want: "restore access to it (such as with chmod), fix or remove the link it leads through, or, if " + filepath.ToSlash(other) +
				" is a linked worktree you no longer need, remove it (`git worktree remove --force " + filepath.ToSlash(other) + "`)"},
		{name: "the acting checkout's unknown location, writing outside it", config: roleConfig(t), path: filepath.Join(elsewhere, "a.go"),
			errs: map[string]error{in(main, ".claude/settings.json"): errors.New("inspect " + in(main, ".claude") + ": permission denied")}, wantCode: 2,
			want: "cannot resolve the protected location " + filepath.ToSlash(in(main, ".claude/settings.json")) + ", so every write is blocked until it can be: inspect"},
		{name: "a location that reaches nothing", config: roleConfig(t), path: filepath.Join(main, "a.go"), resolved: map[string]string{in(main, ".claude/settings.json"): ""}},
		{name: "an extension by the real path", config: roleConfig(t), path: filepath.Join(conf, "extensions", "evil.ts"), resolved: piThroughConf, want: ".pi/extensions is protected"},
		{name: "pi's settings by the real path, in another case", config: roleConfig(t), path: filepath.Join(main, "CONF", "Settings.json"), resolved: piThroughConf, want: ".pi/settings.json is protected"},
		{name: "a settings file that is itself a symlink", config: roleConfig(t), path: filepath.Join(main, "s.json"),
			resolved: map[string]string{in(main, ".claude/settings.local.json"): filepath.Join(main, "s.json")}, want: ".claude/settings.local.json is protected"},
		{name: "the config directory outside the repository", config: roleConfig(t), path: filepath.Join(elsewhere, "config.json"),
			resolved: map[string]string{in(main, ".millennium"): elsewhere}, want: ".millennium is protected"},
		{name: "another worktree's config, for an engineer", config: roleConfig(t), role: "software-engineer", path: filepath.Join(main, "m2", "config.json"),
			resolved: map[string]string{in(other, ".millennium"): filepath.Join(main, "m2")}, want: ".millennium is protected"},
		{name: "a file beside the real location", config: roleConfig(t), path: filepath.Join(conf, "notes.md"), resolved: piThroughConf},
		{name: "not opted in", config: fakeConfig{}, path: filepath.Join(conf, "extensions", "evil.ts"), resolved: piThroughConf},
		{name: "a location that cannot be resolved", config: roleConfig(t), path: filepath.Join(main, "a.go"),
			errs: map[string]error{in(main, ".pi/extensions"): errors.New("resolve " + in(main, ".pi") + ": no such file or directory")}, wantCode: 2,
			want: "cannot resolve the protected location " + filepath.ToSlash(in(main, ".pi/extensions")) + ", so every write is blocked until it can be: resolve"},
		{name: "a worktree whose root cannot be resolved", config: roleConfig(t), role: "software-engineer", path: filepath.Join(main, "a.go"),
			errs: map[string]error{other: errors.New("resolve " + other + ": not a directory"), in(other, ".millennium"): errors.New("never asked")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &fakeResolver{resolved: tt.resolved, errs: tt.errs}
			deps := Deps{Guard: domainGuard{tt.config}, Config: tt.config, Root: main, ResolvePath: resolver.resolve, ResolveLocation: resolver.location, OtherWorktrees: (&fakeWorktrees{roots: []string{other}}).list}
			code, out := preToolUseIn(t, deps, main, "Write", tt.role, filePath(tt.path))
			assertHookAnswer(t, code, out, tt.wantCode, tt.want)
		})
	}
}

// localCase is one Write event that may reach the local configs.
type localCase struct {
	name     string
	path     string
	resolved map[string]string
	errs     map[string]error
	wantCode int
	want     string // "" means allowed, with no output
}

// The local configs are matched like the repository's paths: through a
// symlink, as the real resolver resolves it, and without regard to case. A
// path that cannot be resolved blocks; a local directory that cannot be
// resolved is compared as named.
func TestPreToolUseDeniesWritesToLocalConfigsThroughAnyPath(t *testing.T) {
	local := filepath.Join(t.TempDir(), "config", "millennium", "projects")
	repo := filepath.Join(t.TempDir(), "repo")
	link := filepath.Join(repo, "lclink", "abc", "config.json")
	tests := []localCase{
		{name: "through a symlink in the repository", path: link, resolved: map[string]string{link: filepath.Join(local, "abc", "config.json")}, want: "local configs are protected"},
		{name: "in another case", path: filepath.Join(strings.ToUpper(local), "ab", "config.json"), want: "local configs are protected"},
		{name: "a path that cannot be resolved", path: link, errs: map[string]error{link: errors.New("resolve " + link + ": too many links")},
			wantCode: 2, want: "cannot check the tool's path, so the call is blocked: resolve"},
		{name: "a local directory that cannot be resolved", path: filepath.Join(local, "ab", "config.json"),
			errs: map[string]error{local: errors.New("resolve " + local + ": not a directory")}, want: "local configs are protected"},
		{name: "a path elsewhere", path: filepath.Join(repo, "a.go")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &fakeResolver{resolved: tt.resolved, errs: tt.errs}
			deps := Deps{Guard: domainGuard{}, Config: fakeConfig{}, Root: repo, LocalConfigDir: local, ResolvePath: resolver.resolve, ResolveLocation: resolver.location, OtherWorktrees: noWorktrees}
			code, out := preToolUseIn(t, deps, repo, "Write", "", filePath(tt.path))
			assertHookAnswer(t, code, out, tt.wantCode, tt.want)
		})
	}
}

// posixPaths and windowsPaths are how each OS reads the paths the rows
// below name, so the decision is tested for both on any OS.
var (
	posixPaths = pathRules{
		isAbs:       func(p string) bool { return strings.HasPrefix(p, "/") },
		volumeName:  func(string) string { return "" },
		isSeparator: func(c byte) bool { return c == '/' },
		separator:   '/',
	}
	windowsPaths = pathRules{
		isAbs: func(p string) bool {
			return len(p) > 2 && p[1] == ':' && strings.ContainsRune(`\/`, rune(p[2])) || strings.HasPrefix(p, `\\`)
		},
		volumeName: func(p string) string {
			if len(p) > 1 && p[1] == ':' {
				return p[:2]
			}
			return ""
		},
		isSeparator: func(c byte) bool { return c == '\\' || c == '/' },
		separator:   '\\',
	}
)

// A tool path is joined to the cwd only when it is plainly relative. On
// Windows a rooted path (`\repo\x`, on the process's drive) or a
// drive-relative one (`C:x`, from that drive's own directory) is read
// otherwise by the OS, so the hook refuses it rather than check another
// file than the tool opens.
func TestJoinToolPath(t *testing.T) {
	tests := []struct {
		name     string
		rules    pathRules
		cwd, p   string
		want     string
		wantFail bool
	}{
		{"posix absolute", posixPaths, "/r", "/r/.millennium/config.json", "/r/.millennium/config.json", false},
		{"posix relative", posixPaths, "/r", "src/a.go", "/r/src/a.go", false},
		{"posix backslash is a name", posixPaths, "/r", `\x`, `/r/\x`, false},
		{"posix colon is a name", posixPaths, "/r", "C:x", "/r/C:x", false},
		{"windows absolute", windowsPaths, `C:\r`, `D:\repo\x`, `D:\repo\x`, false},
		{"windows UNC", windowsPaths, `C:\r`, `\\server\share\x`, `\\server\share\x`, false},
		{"windows relative", windowsPaths, `C:\r`, `src\a.go`, `C:\r\src\a.go`, false},
		{"windows rooted", windowsPaths, `D:\r`, `\repo\.millennium\config.json`, "", true},
		{"windows rooted with a slash", windowsPaths, `D:\r`, `/repo/.millennium/config.json`, "", true},
		{"windows drive-relative", windowsPaths, `C:\r`, `C:.millennium\config.json`, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := joinToolPath(tt.cwd, tt.p, tt.rules)
			if got != tt.want || (err != nil) != tt.wantFail {
				t.Fatalf("joinToolPath(%q, %q) = %q, %v; want %q, failure %v", tt.cwd, tt.p, got, err, tt.want, tt.wantFail)
			}
			if tt.wantFail && !strings.Contains(err.Error(), "is neither absolute nor plainly relative") {
				t.Fatalf("message: %v", err)
			}
		})
	}
}

// A refused path blocks the call before anything else is looked at.
func TestToolPathsRefusesAPathItCannotPlace(t *testing.T) {
	in := hookInput{Cwd: `C:\r`, ToolName: "Write", ToolInput: map[string]any{"file_path": `\repo\.millennium\config.json`}}
	if got, err := toolPaths(in, windowsPaths); err == nil || got != nil {
		t.Fatalf("toolPaths = %q, %v; want a refusal", got, err)
	}
}
