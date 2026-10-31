package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

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
		"agent_type": "millennium:engineer",
	})
	if code != 0 || h.gotDir != "/repo/sub" {
		t.Fatalf("exit %d, deps built in %q", code, h.gotDir)
	}
	specific, _ := out["hookSpecificOutput"].(map[string]any)
	if specific["permissionDecision"] != "deny" || specific["permissionDecisionReason"] != "protected" {
		t.Fatalf("output = %v", out)
	}
	if h.guard.got.Role != "engineer" || h.guard.got.Tool != "Edit" || !slices.Equal(h.guard.got.Paths, []string{"sub//repo/.millennium/config.json"}) {
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
	failing := domain.Report{Enabled: true, Results: []domain.GateResult{{Gate: "code", Findings: []domain.Finding{{Gate: "code", Rule: "secret", Message: "a key"}}}}}
	tests := []struct {
		name    string
		verdict usecase.StopVerdict
		key     string
		want    string
	}{
		{"pass", usecase.StopVerdict{}, "", ""},
		{"block", usecase.StopVerdict{Block: true, Attempt: 1, Report: failing}, "decision", "[code/secret]"},
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
