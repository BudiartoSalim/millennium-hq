package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"millennium-code-qc/internal/application/usecase"
	"millennium-code-qc/internal/domain"
)

type fakeGuard struct {
	decision domain.GuardDecision
	got      domain.ToolCall
}

func (f *fakeGuard) Execute(_ context.Context, call domain.ToolCall) domain.GuardDecision {
	f.got = call
	return f.decision
}

type fakeStop struct {
	verdict usecase.StopVerdict
	err     error
	gotKey  string
}

func (f *fakeStop) Execute(_ context.Context, key string) (usecase.StopVerdict, error) {
	f.gotKey = key
	return f.verdict, f.err
}

type fakeScope struct {
	report domain.Report
	err    error
	got    usecase.ScopeInput
}

func (f *fakeScope) Execute(_ context.Context, in usecase.ScopeInput) (domain.Report, error) {
	f.got = in
	return f.report, f.err
}

func hookEvent(fields map[string]any) string {
	data, _ := json.Marshal(fields)
	return string(data)
}

func runHook(t *testing.T, h *harness, fields map[string]any) (int, map[string]any) {
	t.Helper()
	h.stdin.WriteString(hookEvent(fields))
	code := h.run(t, "hook")
	var out map[string]any
	if h.stdout.Len() > 0 {
		if err := json.Unmarshal(h.stdout.Bytes(), &out); err != nil {
			t.Fatalf("hook output is not JSON: %q", h.stdout.String())
		}
	}
	return code, out
}

func TestPreToolUseHook(t *testing.T) {
	h := &harness{guard: &fakeGuard{decision: domain.GuardDecision{Deny: true, Reason: "protected"}}}
	code, out := runHook(t, h, map[string]any{
		"hook_event_name": "PreToolUse", "cwd": "/repo/sub", "session_id": "s",
		"tool_name": "Edit", "tool_input": map[string]any{"file_path": "/repo/.millennium/config.json"},
		"agent_type": "millennium:software-engineer",
	})
	if code != 0 || h.gotDir != "/repo/sub" {
		t.Fatalf("exit %d, deps built in %q", code, h.gotDir)
	}
	specific, _ := out["hookSpecificOutput"].(map[string]any)
	if specific["permissionDecision"] != "deny" || specific["permissionDecisionReason"] != "protected" {
		t.Fatalf("output = %v", out)
	}
	if h.guard.got.Role != "software-engineer" || h.guard.got.Tool != "Edit" || !slices.Equal(h.guard.got.Paths, []string{"sub//repo/.millennium/config.json"}) {
		t.Fatalf("guard got %+v", h.guard.got)
	}
}

func TestPreToolUsePaths(t *testing.T) {
	tests := []struct {
		tool  string
		input map[string]any
		want  []string
	}{
		{"Read", map[string]any{"file_path": "/r/a.go"}, []string{"sub//r/a.go"}},
		{"Grep", map[string]any{"pattern": "x"}, []string{"sub//r"}},
		{"Glob", map[string]any{"pattern": "*.go", "path": "src"}, []string{"sub//r/src"}},
		{"NotebookEdit", map[string]any{"notebook_path": "/r/n.ipynb"}, []string{"sub//r/n.ipynb"}},
		{"Bash", map[string]any{"command": "ls"}, nil},
		{"Edit", map[string]any{}, nil},
	}
	for _, tt := range tests {
		h := &harness{}
		if code, out := runHook(t, h, map[string]any{"hook_event_name": "PreToolUse", "cwd": "/r", "tool_name": tt.tool, "tool_input": tt.input}); code != 0 || out != nil {
			t.Fatalf("%s: exit %d, output %v", tt.tool, code, out)
		}
		if !slices.Equal(h.guard.got.Paths, tt.want) {
			t.Errorf("%s: paths %q, want %q", tt.tool, h.guard.got.Paths, tt.want)
		}
	}
}

func TestStopHook(t *testing.T) {
	failing := domain.Report{Enabled: true, Results: []domain.GateResult{{Gate: "hygiene", Findings: []domain.Finding{{Gate: "hygiene", Rule: "secret", Message: "a key"}}}}}
	tests := []struct {
		name    string
		verdict usecase.StopVerdict
		key     string
		want    string
	}{
		{"pass", usecase.StopVerdict{}, "", ""},
		{"block", usecase.StopVerdict{Block: true, Attempt: 1, Report: failing}, "decision", "[hygiene/secret]"},
		{"escalate", usecase.StopVerdict{Escalate: true, Attempt: 4, Report: failing}, "systemMessage", "the work is NOT done"},
		{"problem", usecase.StopVerdict{Block: true, Attempt: 2, Problem: "config removed"}, "decision", "BLOCKED: config removed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &harness{stop: &fakeStop{verdict: tt.verdict}}
			code, out := runHook(t, h, map[string]any{"hook_event_name": "SubagentStop", "cwd": "/w", "session_id": "s", "agent_id": "a1"})
			if code != 0 || h.stop.gotKey != "s/a1" {
				t.Fatalf("exit %d, key %q", code, h.stop.gotKey)
			}
			assertStopOutput(t, out, tt.key, tt.want)
		})
	}
}

// assertStopOutput checks the hook's answer: nothing for a pass, otherwise
// the expected field carrying the expected text.
func assertStopOutput(t *testing.T, out map[string]any, key, want string) {
	t.Helper()
	if key == "" {
		if out != nil {
			t.Fatalf("a passing stop prints nothing, got %v", out)
		}
		return
	}
	text := fmt.Sprint(out["reason"], out["systemMessage"])
	if _, ok := out[key]; !ok || !strings.Contains(text, want) {
		t.Fatalf("output = %v, want %s containing %q", out, key, want)
	}
	if key == "decision" && out["decision"] != "block" {
		t.Fatalf("decision = %v", out["decision"])
	}
}

func TestSessionStartHook(t *testing.T) {
	h := &harness{config: fakeConfig{enabled: true}}
	code, out := runHook(t, h, map[string]any{"hook_event_name": "SessionStart", "cwd": "/r"})
	specific, _ := out["hookSpecificOutput"].(map[string]any)
	if code != 0 || !strings.Contains(fmt.Sprint(specific["additionalContext"]), "run `millennium-code-qc gate --tier full`") {
		t.Fatalf("exit %d, output %v", code, out)
	}
	if got := sessionContext("'/p/scripts/millennium-code-qc'"); !strings.Contains(got, "run `'/p/scripts/millennium-code-qc' gate --tier full`") {
		t.Fatalf("the context must name the launcher: %s", got)
	}
	quiet := &harness{config: fakeConfig{enabled: false}}
	if code, out := runHook(t, quiet, map[string]any{"hook_event_name": "SessionStart", "cwd": "/r"}); code != 0 || out != nil {
		t.Fatalf("not opted in: exit %d, output %v", code, out)
	}
}

func TestHookFailures(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		h        *harness
		wantCode int
	}{
		{"garbage", "not json", &harness{}, 2},
		{"no event", `{"cwd":"/r"}`, &harness{}, 2},
		{"outside any repository", `{"cwd":"/tmp","hook_event_name":"Stop"}`, &harness{depsErr: fmt.Errorf("wrapped: %w", ErrNoRepository)}, 0},
		{"broken dependencies", `{"cwd":"/r","hook_event_name":"Stop"}`, &harness{depsErr: errors.New("packs broken")}, 2},
		{"stop check fails", `{"cwd":"/r","hook_event_name":"Stop"}`, &harness{stop: &fakeStop{err: errors.New("disk full")}}, 2},
		{"unknown event", `{"cwd":"/r","hook_event_name":"Notification"}`, &harness{}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.h.stdin.WriteString(tt.input)
			if code := tt.h.run(t, "hook"); code != tt.wantCode {
				t.Fatalf("exit %d, want %d; stderr %q", code, tt.wantCode, tt.h.stderr.String())
			}
		})
	}
	big := &harness{}
	big.stdin.WriteString(`{"cwd":"/r","hook_event_name":"Stop","pad":"` + strings.Repeat("x", maxHookInput) + `"}`)
	if code := big.run(t, "hook"); code != 2 {
		t.Fatalf("an oversized event must block, exit %d", code)
	}
}

func TestScopeCommand(t *testing.T) {
	h := &harness{scope: &fakeScope{report: passing}}
	if code := h.run(t, "scope", "--base", "main", "--allow", "svc/domain/**", "--allow", "svc/app/**"); code != 0 {
		t.Fatalf("exit %d; stderr %q", code, h.stderr.String())
	}
	if h.scope.got.Base != "main" || !slices.Equal(h.scope.got.Allow, []string{"svc/domain/**", "svc/app/**"}) {
		t.Fatalf("scope input = %+v", h.scope.got)
	}
	failing := &harness{scope: &fakeScope{err: errors.New("needs --allow")}}
	if code := failing.run(t, "scope"); code != 2 || !strings.Contains(failing.stderr.String(), "needs --allow") {
		t.Fatalf("exit %d stderr %q", code, failing.stderr.String())
	}
	if (&patternFlags{"a", "b"}).String() != "a,b" {
		t.Fatal("patternFlags.String")
	}
}

func TestPreToolUseDeniesWritesToLocalConfigs(t *testing.T) {
	h := &harness{localDir: "/home/u/.config/millennium/projects"}
	code, out := runHook(t, h, map[string]any{
		"hook_event_name": "PreToolUse", "cwd": "/r", "tool_name": "Write",
		"tool_input": map[string]any{"file_path": "/home/u/.config/millennium/projects/ab/config.json"},
	})
	specific, _ := out["hookSpecificOutput"].(map[string]any)
	if code != 0 || specific["permissionDecision"] != "deny" || !strings.Contains(fmt.Sprint(specific["permissionDecisionReason"]), "local configs are protected") {
		t.Fatalf("exit %d, output %v", code, out)
	}
	read := &harness{localDir: "/home/u/.config/millennium/projects"}
	if code, out := runHook(t, read, map[string]any{"hook_event_name": "PreToolUse", "cwd": "/r", "tool_name": "Read",
		"tool_input": map[string]any{"file_path": "/home/u/.config/millennium/projects/ab/config.json"}}); code != 0 || out != nil {
		t.Fatalf("reading a local config is fine: exit %d, output %v", code, out)
	}
}

func briefedConfig(t *testing.T, guides ...string) domain.Config {
	t.Helper()
	cfg, err := domain.NewConfig(domain.ConfigInput{Version: ptr(1), Components: []domain.ComponentInput{
		{Paths: []string{"api"}, Languages: []string{"go"}, Conventions: []string{"flat"}},
		{Paths: []string{"tools"}, Languages: []string{"go"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Packs = domain.Catalog{}
	for i, guide := range guides {
		pack := samplePack(t)
		pack.Name, pack.Guide = domain.Convention([]string{"flat", "wide", "tall"}[i]), guide
		cfg.Packs[pack.Name] = pack
	}
	return cfg
}

func TestBriefingCarriesTheGuides(t *testing.T) {
	h := &harness{config: fakeConfig{enabled: true, cfg: briefedConfig(t, "Keep it flat.")}}
	code, out := runHook(t, h, map[string]any{"hook_event_name": "SubagentStart", "cwd": "/r", "agent_type": "millennium:qa-tester"})
	specific, _ := out["hookSpecificOutput"].(map[string]any)
	context := fmt.Sprint(specific["additionalContext"])
	if code != 0 || specific["hookEventName"] != "SubagentStart" {
		t.Fatalf("exit %d, output %v", code, out)
	}
	for _, want := range []string{"Conventions, by component: api: flat;", "<convention-guide name=\"flat\">\nKeep it flat.\n</convention-guide>"} {
		if !strings.Contains(context, want) {
			t.Errorf("context lacks %q:\n%s", want, context)
		}
	}
	if strings.Contains(context, "tools:") {
		t.Error("components without conventions are not listed")
	}
}

func TestBriefingNamesTheGuidesThatDoNotFit(t *testing.T) {
	long := strings.Repeat("x", domain.MaxGuideRunes)
	got := briefing("", briefedConfig(t, long, "Short.", long))
	if n := utf8.RuneCountInString(got); n > maxBriefingRunes {
		t.Fatalf("the briefing has %d characters, more than %d", n, maxBriefingRunes)
	}
	if !strings.Contains(got, "<convention-guide name=\"flat\">") || !strings.Contains(got, "<convention-guide name=\"wide\">") ||
		!strings.Contains(got, "The guides of tall did not fit here: read each with `millennium-code-qc convention show <name>`") {
		t.Fatalf("briefing:\n%.600s", got)
	}
	if plain := briefing("", domain.Config{}); strings.Contains(plain, "Conventions") {
		t.Fatalf("no conventions, no guides: %s", plain)
	}
}

// vanishedTree is a DepsFactory over a made-up directory tree: a directory
// missing reports does not exist, one in failing fails as given, and any
// other opens. It records the directories asked for and the releases.
type vanishedTree struct {
	missing func(dir string) bool
	failing map[string]error
	guard   *fakeGuard
	visited []string
	closed  int
}

func (v *vanishedTree) deps(_ context.Context, dir string) (Deps, error) {
	v.visited = append(v.visited, dir)
	release := func() { v.closed++ }
	if v.missing(dir) {
		return Deps{Close: release}, fmt.Errorf("%s: %w", dir, ErrNoDirectory)
	}
	if err := v.failing[dir]; err != nil {
		return Deps{Close: release}, err
	}
	return Deps{Guard: v.guard, Stop: &fakeStop{}, RepoPath: repoPath, MapPath: mapPath, OtherWorktrees: noWorktrees, Close: release}, nil
}

func noWorktrees(context.Context) ([]string, error) { return nil, nil }

func missingDirs(dirs ...string) func(string) bool {
	return func(dir string) bool { return slices.Contains(dirs, dir) }
}

func missingEverywhere(string) bool { return true }

// vanishedCase is one PreToolUse event in a cwd that may no longer exist.
type vanishedCase struct {
	name     string
	cwd      string
	missing  func(string) bool
	failing  map[string]error
	tool     string
	input    map[string]any
	deny     bool
	wantCode int
	visited  []string
	wantOut  string
}

// A hook event whose cwd was removed, such as a deleted worktree, is
// evaluated in the nearest parent that exists. The walk moves up only past
// a directory that does not exist, stops at the first one that does, and
// needs an absolute, clean cwd; anything else blocks.
func TestHookInAVanishedCwd(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	worktrees := filepath.Join(repo, ".claude", "worktrees")
	worktree := filepath.Join(worktrees, "agent")
	deep := filepath.Join(worktree, "src", "pkg")
	unclean := worktree + string(filepath.Separator) + ".." + string(filepath.Separator) + "other"
	relative := filepath.Join("repo", "gone")
	protected := filepath.Join(repo, ".millennium", "config.json")
	tests := []vanishedCase{
		{name: "an allowed command runs in the nearest existing parent", cwd: deep,
			missing: missingDirs(deep, filepath.Dir(deep), worktree),
			tool:    "Bash", input: map[string]any{"command": "cd " + repo},
			visited: []string{deep, filepath.Dir(deep), worktree, worktrees}},
		{name: "the guard still denies there", cwd: worktree, missing: missingDirs(worktree),
			tool: "Edit", input: map[string]any{"file_path": protected}, deny: true,
			visited: []string{worktree, worktrees}, wantOut: `"permissionDecision":"deny"`},
		{name: "a parent outside any repository passes", cwd: worktree, missing: missingDirs(worktree),
			failing: map[string]error{worktrees: fmt.Errorf("wrapped: %w", ErrNoRepository)},
			tool:    "Bash", visited: []string{worktree, worktrees}},
		{name: "the first existing parent decides", cwd: worktree, missing: missingDirs(worktree),
			failing: map[string]error{worktrees: errors.New("detected dubious ownership")},
			tool:    "Bash", wantCode: 2, visited: []string{worktree, worktrees}, wantOut: "dubious ownership"},
		{name: "any other failure does not walk", cwd: worktree, missing: missingDirs(),
			failing: map[string]error{worktree: errors.New("cannot change to it: Permission denied")},
			tool:    "Bash", wantCode: 2, visited: []string{worktree}, wantOut: "Permission denied"},
		{name: "a relative cwd does not walk", cwd: relative, missing: missingEverywhere,
			tool: "Bash", wantCode: 2, visited: []string{relative}, wantOut: "millennium-code-qc: the hook's cwd " + relative + " does not exist, and the engine looks for an existing parent only of an absolute path in clean form"},
		{name: "an unclean cwd does not walk", cwd: unclean, missing: missingEverywhere,
			tool: "Bash", wantCode: 2, visited: []string{unclean}, wantOut: "so the hook blocks: change to a directory that exists, such as the repository root\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertVanishedCwd(t, tt)
		})
	}
}

func assertVanishedCwd(t *testing.T, tt vanishedCase) {
	t.Helper()
	tree := &vanishedTree{missing: tt.missing, failing: tt.failing, guard: &fakeGuard{decision: domain.GuardDecision{Deny: tt.deny, Reason: "protected"}}}
	var stdout, stderr bytes.Buffer
	stdin := strings.NewReader(hookEvent(map[string]any{"hook_event_name": "PreToolUse", "cwd": tt.cwd, "tool_name": tt.tool, "tool_input": tt.input}))
	code := New("1.2.3", stdin, &stdout, &stderr, tree.deps).Run(t.Context(), []string{"hook"})
	if code != tt.wantCode || !slices.Equal(tree.visited, tt.visited) {
		t.Fatalf("exit %d (want %d), visited %q\nwant %q\nstderr %q", code, tt.wantCode, tree.visited, tt.visited, stderr.String())
	}
	out := stdout.String() + stderr.String()
	if tt.wantOut == "" && out != "" || !strings.Contains(out, tt.wantOut) {
		t.Fatalf("output %q, want %q", out, tt.wantOut)
	}
	if tree.closed != len(tree.visited) {
		t.Fatalf("released %d of %d dependency sets", tree.closed, len(tree.visited))
	}
	opened := tt.failing[tt.visited[len(tt.visited)-1]] == nil && !tt.missing(tt.visited[len(tt.visited)-1])
	if opened && tree.guard.got.Tool != tt.tool {
		t.Fatalf("the guard got %+v, want the %s call", tree.guard.got, tt.tool)
	}
}

// A cwd whose parents are all gone, up to the root, stops after a bounded
// number of steps and blocks.
func TestHookWalksABoundedNumberOfParents(t *testing.T) {
	cwd := t.TempDir()
	for range maxVanishedLevels + 5 {
		cwd = filepath.Join(cwd, "d")
	}
	tree := &vanishedTree{missing: missingEverywhere}
	var stdout, stderr bytes.Buffer
	stdin := strings.NewReader(hookEvent(map[string]any{"hook_event_name": "Stop", "cwd": cwd}))
	code := New("1.2.3", stdin, &stdout, &stderr, tree.deps).Run(t.Context(), []string{"hook"})
	if code != 2 || len(tree.visited) != maxVanishedLevels+1 || !strings.Contains(stderr.String(), "nearest parents") {
		t.Fatalf("exit %d after %d directories, stderr %q", code, len(tree.visited), stderr.String())
	}
	if tree.closed != len(tree.visited) {
		t.Fatalf("released %d of %d dependency sets", tree.closed, len(tree.visited))
	}
}

// pathMapper is a fake Deps.MapPath for the repository's own root: mapped
// paths map, failing ones fail as given, and any other is outside the
// repository. It records what it was asked.
type pathMapper struct {
	mapped map[string]string
	errs   map[string]error
	asked  []string
}

func (m *pathMapper) mapPath(_, arg string) (string, error) {
	m.asked = append(m.asked, arg)
	if err := m.errs[arg]; err != nil {
		return "", err
	}
	if p, ok := m.mapped[arg]; ok {
		return p, nil
	}
	return "", fmt.Errorf("%s: %w", arg, ErrOutsideRepository)
}

// guardedCase is one PreToolUse event whose path the hook maps for the guard.
type guardedCase struct {
	name     string
	cwd      string
	path     string
	mapped   map[string]string
	errs     map[string]error
	want     []string
	asked    []string
	wantCode int
	wantErr  string
}

// The guard sees each path a tool names both as named and with symlinks
// resolved, never through the process's working directory. A path outside
// the repository either way is left out; a path that cannot be mapped for
// any other reason blocks the call, and so does one with a ".." part, which
// the OS takes after following a symlink while text cleaning takes it
// before.
func TestPreToolUseMapsPathsForTheGuard(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "repo")
	protected := filepath.Join(root, ".millennium", "config.json")
	alias, rules := filepath.Join(root, "alias.json"), filepath.Join(root, "rules", "config.json")
	throughLink := filepath.Join(parent, "repo-link", ".millennium", "config.json")
	elsewhere := filepath.Join(parent, "elsewhere", "x.go")
	gone := filepath.Join(root, "worktrees", "agent")
	sep := string(filepath.Separator)
	tests := []guardedCase{
		{name: "a root named in another case is the root", cwd: root, path: filepath.Join(parent, "REPO", ".Millennium", "config.json"),
			want: []string{".Millennium/config.json"}, asked: []string{filepath.Join(parent, "REPO", ".Millennium", "config.json")}},
		{name: "a relative path without '..' joins the removed cwd", cwd: gone, path: filepath.Join("src", "new.go"),
			mapped: map[string]string{filepath.Join(gone, "src", "new.go"): "worktrees/agent/src/new.go"},
			want:   []string{"worktrees/agent/src/new.go"}, asked: []string{filepath.Join(gone, "src", "new.go")}},
		{name: "a relative path with '..' blocks", cwd: gone, path: ".." + sep + ".." + sep + filepath.Join(".millennium", "config.json"),
			wantCode: 2, wantErr: "has a '..' part, which the hook cannot check"},
		{name: "an absolute path with '..' blocks", cwd: root, path: filepath.Join(parent, "l") + sep + ".." + sep + filepath.Join(".millennium", "config.json"),
			wantCode: 2, wantErr: "name the path without '..', such as its absolute path"},
		{name: "a path in the repository is checked once", cwd: root, path: protected,
			mapped: map[string]string{protected: ".millennium/config.json"}, want: []string{".millennium/config.json"}, asked: []string{protected}},
		{name: "a symlink is checked as named and as resolved", cwd: root, path: alias,
			mapped: map[string]string{alias: ".millennium/config.json"}, want: []string{"alias.json", ".millennium/config.json"}, asked: []string{alias}},
		{name: "a path that resolves outside is checked as named", cwd: root, path: rules,
			want: []string{"rules/config.json"}, asked: []string{rules}},
		{name: "a symlink to the repository is checked as resolved", cwd: root, path: throughLink,
			mapped: map[string]string{throughLink: ".millennium/config.json"}, want: []string{".millennium/config.json"}, asked: []string{throughLink}},
		{name: "a path outside either way is left out", cwd: root, path: elsewhere, asked: []string{elsewhere}},
		{name: "a path that cannot be mapped blocks", cwd: root, path: protected,
			errs:  map[string]error{protected: errors.New("inspect " + protected + ": permission denied")},
			asked: []string{protected}, wantCode: 2, wantErr: "cannot check the tool's path, so the call is blocked: inspect"},
		{name: "a relative cwd blocks", cwd: filepath.Join("relative", "dir"), path: "a.go",
			wantCode: 2, wantErr: "is not absolute"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertGuardedPaths(t, root, tt)
		})
	}
}

func assertGuardedPaths(t *testing.T, root string, tt guardedCase) {
	t.Helper()
	mapper := &pathMapper{mapped: tt.mapped, errs: tt.errs}
	guard := &fakeGuard{}
	factory := func(context.Context, string) (Deps, error) {
		return Deps{Guard: guard, Root: root, MapPath: mapper.mapPath, OtherWorktrees: noWorktrees}, nil
	}
	var stdout, stderr bytes.Buffer
	stdin := strings.NewReader(hookEvent(map[string]any{"hook_event_name": "PreToolUse", "cwd": tt.cwd, "tool_name": "Edit", "tool_input": map[string]any{"file_path": tt.path}}))
	code := New("1.2.3", stdin, &stdout, &stderr, factory).Run(t.Context(), []string{"hook"})
	if code != tt.wantCode || !strings.Contains(stderr.String(), tt.wantErr) || !slices.Equal(mapper.asked, tt.asked) {
		t.Fatalf("exit %d (want %d), asked %q (want %q), stderr %q (want %q)", code, tt.wantCode, mapper.asked, tt.asked, stderr.String(), tt.wantErr)
	}
	if tt.wantCode != 0 {
		if guard.got.Tool != "" {
			t.Fatalf("a blocked call reached the guard: %+v", guard.got)
		}
		return
	}
	if guard.got.Tool != "Edit" || !slices.Equal(guard.got.Paths, tt.want) {
		t.Fatalf("the guard got %+v, want paths %q", guard.got, tt.want)
	}
}

// domainGuard decides with the domain's guard, so a hook test sees the real
// decision on the paths the hook mapped.
type domainGuard struct{}

func (domainGuard) Execute(_ context.Context, call domain.ToolCall) domain.GuardDecision {
	return domain.Guard(domain.Config{AcceptanceTests: "tests/acceptance"}, call)
}

// worktreeCase is one PreToolUse event in a repository with several
// worktrees.
type worktreeCase struct {
	name      string
	root      string
	worktrees []string
	listErr   error
	tool      string
	input     map[string]any
	want      string // "" means allowed
	wantCode  int
}

// No agent changes the rules of any checkout of the repository: each path
// is checked in the worktree the agent is in and in every other one, the
// main checkout included, wherever the event's cwd is.
func TestPreToolUseGuardsEveryWorktree(t *testing.T) {
	main := filepath.Join(t.TempDir(), "repo")
	linked := filepath.Join(main, ".claude", "worktrees", "agent")
	other := filepath.Join(main, ".claude", "worktrees", "other")
	file := func(p string) map[string]any { return map[string]any{"file_path": p} }
	tests := []worktreeCase{
		{name: "the main checkout's config from a linked worktree", root: linked, worktrees: []string{main, other},
			tool: "Write", input: file(filepath.Join(main, ".millennium", "config.json")), want: ".millennium is protected"},
		{name: "another worktree's config from the main checkout", root: main, worktrees: []string{linked, other},
			tool: "Write", input: file(filepath.Join(other, ".millennium", "config.json")), want: ".millennium is protected"},
		{name: "another worktree's settings in another case", root: main, worktrees: []string{other},
			tool: "Edit", input: file(filepath.Join(other, ".Claude", "Settings.json")), want: ".claude/settings.json is protected"},
		{name: "code in another worktree", root: linked, worktrees: []string{main, other},
			tool: "Write", input: file(filepath.Join(other, "src", "a.go"))},
		{name: "reading another worktree's config", root: linked, worktrees: []string{main},
			tool: "Read", input: file(filepath.Join(main, ".millennium", "config.json"))},
		{name: "worktrees that cannot be listed block a path", root: main, listErr: errors.New("git worktree list: exit status 129"),
			tool: "Read", input: file(filepath.Join(main, "a.go")), wantCode: 2, want: "cannot check the tool's path in every worktree of the repository, so the call is blocked: git worktree list"},
		{name: "a call without a path lists no worktree", root: main, listErr: errors.New("not listed"),
			tool: "Bash", input: map[string]any{"command": "ls"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertWorktreeGuard(t, tt)
		})
	}
}

func assertWorktreeGuard(t *testing.T, tt worktreeCase) {
	t.Helper()
	factory := func(context.Context, string) (Deps, error) {
		list := func(context.Context) ([]string, error) { return tt.worktrees, tt.listErr }
		return Deps{Guard: domainGuard{}, Root: tt.root, MapPath: mapPath, OtherWorktrees: list}, nil
	}
	var stdout, stderr bytes.Buffer
	stdin := strings.NewReader(hookEvent(map[string]any{"hook_event_name": "PreToolUse", "cwd": tt.root, "tool_name": tt.tool, "tool_input": tt.input}))
	code := New("1.2.3", stdin, &stdout, &stderr, factory).Run(t.Context(), []string{"hook"})
	out := stdout.String() + stderr.String()
	if code != tt.wantCode || !strings.Contains(out, tt.want) {
		t.Fatalf("exit %d (want %d), output %q, want %q", code, tt.wantCode, out, tt.want)
	}
	if denied := strings.Contains(out, `"permissionDecision":"deny"`); denied != (tt.want != "" && tt.wantCode == 0) {
		t.Fatalf("denied %v, output %q", denied, out)
	}
}

// localCase is one Write event that may reach the local configs.
type localCase struct {
	name     string
	path     string
	mapped   map[string]string
	errs     map[string]error
	wantCode int
	want     string // "" means allowed, with no output
}

// The local configs are matched like the repository's paths: through a
// symlink, as the real MapPath resolves it, and without regard to case. A
// path that cannot be mapped there blocks.
func TestPreToolUseDeniesWritesToLocalConfigsThroughAnyPath(t *testing.T) {
	local := filepath.Join(t.TempDir(), "config", "millennium", "projects")
	repo := filepath.Join(t.TempDir(), "repo")
	link := filepath.Join(repo, "lclink", "abc", "config.json")
	tests := []localCase{
		{name: "through a symlink in the repository", path: link, mapped: map[string]string{link: "abc/config.json"}, want: "local configs are protected"},
		{name: "in another case", path: filepath.Join(strings.ToUpper(local), "ab", "config.json"), want: "local configs are protected"},
		{name: "a path that cannot be resolved", path: link, errs: map[string]error{link: errors.New("resolve " + link + ": too many links")},
			wantCode: 2, want: "cannot check the tool's path, so the call is blocked: resolve"},
		{name: "a path elsewhere", path: filepath.Join(repo, "a.go")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertLocalConfigGuard(t, local, repo, tt)
		})
	}
}

func assertLocalConfigGuard(t *testing.T, local, repo string, tt localCase) {
	t.Helper()
	mapper := &pathMapper{mapped: tt.mapped, errs: tt.errs}
	factory := func(context.Context, string) (Deps, error) {
		return Deps{Guard: domainGuard{}, Root: repo, LocalConfigDir: local, MapPath: mapper.mapPath, OtherWorktrees: noWorktrees}, nil
	}
	var stdout, stderr bytes.Buffer
	stdin := strings.NewReader(hookEvent(map[string]any{"hook_event_name": "PreToolUse", "cwd": repo, "tool_name": "Write", "tool_input": map[string]any{"file_path": tt.path}}))
	code := New("1.2.3", stdin, &stdout, &stderr, factory).Run(t.Context(), []string{"hook"})
	if out := stdout.String() + stderr.String(); code != tt.wantCode || !strings.Contains(out, tt.want) || tt.want == "" && out != "" {
		t.Fatalf("exit %d (want %d), output %q, want %q", code, tt.wantCode, out, tt.want)
	}
}
