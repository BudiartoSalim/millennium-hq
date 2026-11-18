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
	h := &harness{guard: &fakeGuard{decision: domain.GuardDecision{Deny: true, Reason: "protected"}}, root: "/repo", config: fakeConfig{enabled: true}}
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
	got := h.guard.got
	if got.Role != "software-engineer" || got.Tool != "Edit" || !slices.Equal(got.Targets, []string{"/repo/.millennium/config.json"}) || !slices.Equal(got.Paths, []string{".millennium/config.json"}) {
		t.Fatalf("guard got %+v", got)
	}
}

// The hook works out only what the guard's rules read: nothing for a call
// no path rule applies to, the targets for a write, and the paths in the
// worktrees for a role rule.
func TestPreToolUsePaths(t *testing.T) {
	tests := []struct {
		tool, role   string
		input        map[string]any
		targets, rel []string
	}{
		{"Read", "", map[string]any{"file_path": "/r/a.go"}, nil, nil},
		{"Grep", "", map[string]any{"pattern": "x"}, nil, nil},
		{"Read", "qa-tester", map[string]any{"file_path": "/r/a.go"}, []string{"/r/a.go"}, []string{"a.go"}},
		{"Grep", "millennium:qa-tester", map[string]any{"pattern": "x"}, []string{"/r"}, []string{""}},
		{"Glob", "qa-tester", map[string]any{"pattern": "*.go", "path": "src"}, []string{"/r/src"}, []string{"src"}},
		{"NotebookEdit", "", map[string]any{"notebook_path": "/r/n.ipynb"}, []string{"/r/n.ipynb"}, nil},
		{"Write", "qa-tester", map[string]any{"file_path": "/r/tests/acceptance/a_test.go"}, []string{"/r/tests/acceptance/a_test.go"}, nil},
		{"MultiEdit", "software-engineer", map[string]any{"file_path": "/r/x/y.go"}, []string{"/r/x/y.go"}, []string{"x/y.go"}},
		{"Bash", "qa-tester", map[string]any{"command": "ls"}, nil, nil},
		{"Edit", "", map[string]any{}, nil, nil},
	}
	for _, tt := range tests {
		h := &harness{root: "/r", config: fakeConfig{enabled: true}}
		event := map[string]any{"hook_event_name": "PreToolUse", "cwd": "/r", "tool_name": tt.tool, "tool_input": tt.input, "agent_type": tt.role}
		if code, out := runHook(t, h, event); code != 0 || out != nil {
			t.Fatalf("%s: exit %d, output %v", tt.tool, code, out)
		}
		if !slices.Equal(h.guard.got.Targets, tt.targets) || !slices.Equal(h.guard.got.Paths, tt.rel) {
			t.Errorf("%s by %q: targets %q, paths %q; want %q, %q", tt.tool, tt.role, h.guard.got.Targets, h.guard.got.Paths, tt.targets, tt.rel)
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
	return Deps{Guard: v.guard, Stop: &fakeStop{}, Config: fakeConfig{}, RepoPath: repoPath, ResolvePath: resolveAsNamed, ResolveLocation: locateAsNamed, OtherWorktrees: noWorktrees, Close: release}, nil
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
