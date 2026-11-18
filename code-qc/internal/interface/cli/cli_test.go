package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"millennium-code-qc/internal/application/usecase"
	"millennium-code-qc/internal/domain"
)

type fakeGates struct {
	report domain.Report
	err    error
	got    usecase.RunGatesInput
}

func (f *fakeGates) Execute(_ context.Context, in usecase.RunGatesInput) (domain.Report, error) {
	f.got = in
	return f.report, f.err
}

type fakeConfig struct {
	cfg     domain.Config
	enabled bool
	err     error
}

func (f fakeConfig) Execute(context.Context) (domain.Config, bool, error) {
	return f.cfg, f.enabled, f.err
}

type fakeAudit struct {
	report domain.Report
	err    error
	got    usecase.AuditInput
}

func (f *fakeAudit) Execute(_ context.Context, in usecase.AuditInput) (domain.Report, error) {
	f.got = in
	return f.report, f.err
}

type fakeExplain struct {
	explanation usecase.Explanation
	err         error
	got         string
}

func (f *fakeExplain) Execute(_ context.Context, path string) (usecase.Explanation, error) {
	f.got = path
	f.explanation.Path = path
	return f.explanation, f.err
}

// repoPath stands in for the real resolver: "sub/" plus the argument, and an
// error for anything starting with "..".
func repoPath(arg string) (string, error) {
	if strings.HasPrefix(arg, "..") {
		return "", errors.New(arg + " is outside the repository")
	}
	return "sub/" + arg, nil
}

// mapPath stands in for the real MapPath: lexical, and for the harness's
// unnamed root "" the same as repoPath.
func mapPath(root, abs string) (string, error) {
	if root == "" {
		return repoPath(abs)
	}
	if rel, ok := domain.RelativeFold(root, abs); ok {
		return rel, nil
	}
	return "", fmt.Errorf("%s: %w", abs, ErrOutsideRepository)
}

type harness struct {
	gates          *fakeGates
	audit          *fakeAudit
	explain        *fakeExplain
	guard          *fakeGuard
	stop           *fakeStop
	scope          *fakeScope
	applier        *fakeApplier
	discover       *fakeDiscover
	conventions    *fakeConventions
	root           string
	localDir       string
	worktrees      []string
	terminal       bool
	config         fakeConfig
	depsErr        error
	gotDir         string
	stdin          bytes.Buffer
	stdout, stderr bytes.Buffer
}

func (h *harness) run(t *testing.T, args ...string) int {
	t.Helper()
	if h.gates == nil {
		h.gates = &fakeGates{}
	}
	if h.audit == nil {
		h.audit = &fakeAudit{}
	}
	if h.explain == nil {
		h.explain = &fakeExplain{}
	}
	if h.guard == nil {
		h.guard = &fakeGuard{}
	}
	if h.stop == nil {
		h.stop = &fakeStop{}
	}
	if h.scope == nil {
		h.scope = &fakeScope{}
	}
	if h.applier == nil {
		h.applier = &fakeApplier{}
	}
	if h.discover == nil {
		h.discover = &fakeDiscover{}
	}
	if h.conventions == nil {
		h.conventions = &fakeConventions{}
	}
	deps := func(_ context.Context, dir string) (Deps, error) {
		h.gotDir = dir
		return Deps{Gates: h.gates, Audit: h.audit, Config: h.config, Explain: h.explain, Guard: h.guard,
			Stop: h.stop, Scope: h.scope, Apply: h.applier, Discover: h.discover, Conventions: h.conventions, Root: h.root, LocalConfigDir: h.localDir, RepoPath: repoPath, ReadProposal: readProposalFile,
			OtherWorktrees: func(context.Context) ([]string, error) { return h.worktrees, nil }, MapPath: mapPath}, h.depsErr
	}
	app := New("1.2.3", &h.stdin, &h.stdout, &h.stderr, deps)
	app.terminal = func() bool { return h.terminal }
	return app.Run(t.Context(), args)
}

// readProposalFile stands in for the strict reader main wires in, which
// configfile tests.
func readProposalFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read the proposal: %w", err)
	}
	return data, nil
}

var (
	passing = domain.Report{Enabled: true, ChangedFiles: 2, Results: []domain.GateResult{{Gate: "toolchain"}}}
	failing = domain.Report{Enabled: true, Results: []domain.GateResult{{Gate: "toolchain", Findings: []domain.Finding{
		{Gate: "toolchain", Rule: "tool-missing", Message: "gofmt is required", File: "main.go", Line: 3},
	}}}}
)

func TestGateExitCodes(t *testing.T) {
	tests := []struct {
		name     string
		gates    *fakeGates
		depsErr  error
		args     []string
		wantCode int
		wantErr  string
	}{
		{"pass", &fakeGates{report: passing}, nil, nil, 0, "passed (gates: 1, changed files: 2)"},
		{"not enabled", &fakeGates{report: domain.Report{}}, nil, nil, 0, "not enabled"},
		{"findings", &fakeGates{report: failing}, nil, nil, 2, "[toolchain/tool-missing] main.go:3: gofmt is required"},
		{"findings summary", &fakeGates{report: failing}, nil, nil, 2, "by rule: toolchain/tool-missing 1"},
		{"no gate ran", &fakeGates{report: domain.Report{Enabled: true}}, nil, nil, 2, "no gate ran"},
		{"use-case error", &fakeGates{err: errors.New("config removed")}, nil, nil, 2, "BLOCKED: config removed"},
		{"dependencies fail", nil, errors.New("not a git repository"), nil, 2, "not a git repository"},
		{"bad flag", nil, nil, []string{"--nope"}, 2, "flag provided but not defined"},
		{"stray argument", nil, nil, []string{"extra"}, 2, "unexpected argument"},
		{"bad format", nil, nil, []string{"--format", "xml"}, 2, `unknown format "xml"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &harness{gates: tt.gates, depsErr: tt.depsErr}
			code := h.run(t, append([]string{"gate"}, tt.args...)...)
			if code != tt.wantCode {
				t.Fatalf("exit %d, want %d; stderr: %s", code, tt.wantCode, h.stderr.String())
			}
			if !strings.Contains(h.stderr.String(), tt.wantErr) {
				t.Fatalf("stderr %q does not contain %q", h.stderr.String(), tt.wantErr)
			}
		})
	}
}

func TestGatePassesFlagsToUseCase(t *testing.T) {
	h := &harness{gates: &fakeGates{report: passing}}
	h.run(t, "gate", "--base", "main", "--require-config")
	if h.gates.got.Base != "main" || h.gates.got.Config != usecase.ConfigRequired {
		t.Fatalf("use-case input = %+v", h.gates.got)
	}
	h.run(t, "gate")
	if h.gates.got.Base != "" || h.gates.got.Config != usecase.ConfigOptional {
		t.Fatalf("default use-case input = %+v", h.gates.got)
	}
}

func TestGateJSON(t *testing.T) {
	tests := []struct {
		name       string
		gates      *fakeGates
		wantStatus string
		wantCode   int
	}{
		{"pass", &fakeGates{report: passing}, "pass", 0},
		{"blocked", &fakeGates{report: failing}, "blocked", 2},
		{"not enabled", &fakeGates{report: domain.Report{}}, "not-enabled", 0},
		{"error", &fakeGates{err: errors.New("boom")}, "error", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &harness{gates: tt.gates}
			code := h.run(t, "gate", "--format", "json")
			var out jsonReport
			if err := json.Unmarshal(h.stdout.Bytes(), &out); err != nil {
				t.Fatalf("stdout is not JSON: %v\n%s", err, h.stdout.String())
			}
			if out.Status != tt.wantStatus || code != tt.wantCode {
				t.Fatalf("status %q exit %d, want %q exit %d", out.Status, code, tt.wantStatus, tt.wantCode)
			}
		})
	}
	h := &harness{gates: &fakeGates{report: failing}}
	h.run(t, "gate", "--format", "json")
	var out jsonReport
	_ = json.Unmarshal(h.stdout.Bytes(), &out)
	if len(out.Findings) != 1 || out.Findings[0].Rule != "tool-missing" || out.Findings[0].Line != 3 {
		t.Fatalf("findings not rendered: %+v", out.Findings)
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name     string
		config   fakeConfig
		wantCode int
		want     string
	}{
		{"valid", fakeConfig{enabled: true}, 0, "is valid"},
		{"invalid", fakeConfig{err: errors.New("invalid config: version: required")}, 2, "version: required"},
		{"absent", fakeConfig{}, 2, "no " + domain.ConfigPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &harness{config: tt.config}
			code := h.run(t, "config", "validate")
			output := h.stdout.String() + h.stderr.String()
			if code != tt.wantCode || !strings.Contains(output, tt.want) {
				t.Fatalf("exit %d output %q; want exit %d containing %q", code, output, tt.wantCode, tt.want)
			}
		})
	}
}

func TestTopLevelCommands(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCode int
		want     string
	}{
		{"version", []string{"version"}, 0, "millennium-code-qc 1.2.3"},
		{"help", []string{"help"}, 0, "Usage:"},
		{"no command", nil, 2, "Usage:"},
		{"unknown command", []string{"deploy"}, 2, `unknown command "deploy"`},
		{"config without subcommand", []string{"config"}, 2, "Usage:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &harness{depsErr: errors.New("dependencies must not be built for this command")}
			code := h.run(t, tt.args...)
			output := h.stdout.String() + h.stderr.String()
			if code != tt.wantCode || !strings.Contains(output, tt.want) {
				t.Fatalf("exit %d output %q; want exit %d containing %q", code, output, tt.wantCode, tt.want)
			}
		})
	}
}

func TestAudit(t *testing.T) {
	h := &harness{audit: &fakeAudit{report: passing}}
	if code := h.run(t, "audit", "--path", "services"); code != 0 {
		t.Fatalf("exit %d; stderr: %s", code, h.stderr.String())
	}
	if h.audit.got.Path != "sub/services" {
		t.Fatalf("audit path = %q, want it resolved to sub/services", h.audit.got.Path)
	}
	if !strings.Contains(h.stderr.String(), "audited files: 2") {
		t.Fatalf("stderr = %q", h.stderr.String())
	}

	blocked := &harness{audit: &fakeAudit{report: failing}}
	if code := blocked.run(t, "audit"); code != 2 || blocked.audit.got.Path != "" {
		t.Fatalf("exit %d, path %q; want 2 and the whole repository", code, blocked.audit.got.Path)
	}
	outside := &harness{}
	if code := outside.run(t, "audit", "--path", "../x"); code != 2 || !strings.Contains(outside.stderr.String(), "outside the repository") {
		t.Fatalf("exit %d stderr %q", code, outside.stderr.String())
	}
}

func assertContainsAll(t *testing.T, output string, wants []string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(output, want) {
			t.Errorf("output %q does not contain %q", output, want)
		}
	}
}

func TestConfigExplain(t *testing.T) {
	component := domain.Component{Patterns: []domain.Pattern{domain.RootPattern()}, Languages: []domain.Language{domain.LanguageGo}}
	tests := []struct {
		name        string
		explanation usecase.Explanation
		want        []string
	}{
		{"layered file", usecase.Explanation{
			Language:  domain.LanguageGo,
			Component: &domain.ComponentMatch{Component: component, Root: ""},
			Layers:    []usecase.LayerAssignment{{Convention: "clean-architecture", Layer: "domain"}, {Convention: "react-feature-based", Problem: "not available in this engine version"}},
			Limits:    domain.DefaultLimits(),
		}, []string{"sub/a.go", "language:  go", "component: . (root \"\")", "clean-architecture: layer domain",
			"react-feature-based: cannot be checked: not available", "function length 50 lines"}},
		{"no component", usecase.Explanation{Language: domain.LanguageGo}, []string{"component: none"}},
		{"excluded", usecase.Explanation{Excluded: true}, []string{"excluded: yes"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &harness{explain: &fakeExplain{explanation: tt.explanation}}
			if code := h.run(t, "config", "explain", "a.go"); code != 0 {
				t.Fatalf("exit %d; stderr: %s", code, h.stderr.String())
			}
			assertContainsAll(t, h.stdout.String(), tt.want)
		})
	}
	failing := &harness{explain: &fakeExplain{err: errors.New("config removed")}}
	if code := failing.run(t, "config", "explain", "a.go"); code != 2 || !strings.Contains(failing.stderr.String(), "config removed") {
		t.Fatalf("exit %d stderr %q", code, failing.stderr.String())
	}
}

// A run against a proposal says so in both formats, passing or blocked:
// its verdict is not the one the hooks reach until the proposal is applied.
func TestProposedRunsAreMarked(t *testing.T) {
	blocked := domain.Report{Enabled: true, Proposed: true, Results: []domain.GateResult{{Gate: "g", Findings: []domain.Finding{{Gate: "g", Rule: "r", Message: "m"}}}}}
	pass := passing
	pass.Proposed = true
	for _, report := range []domain.Report{pass, blocked} {
		h := &harness{gates: &fakeGates{report: report}}
		h.run(t, "gate")
		if !strings.Contains(h.stderr.String(), "against a proposal, not the applied config") {
			t.Fatalf("text report does not mark the proposal: %q", h.stderr.String())
		}
		h = &harness{gates: &fakeGates{report: report}}
		h.run(t, "gate", "--format", "json")
		var out jsonReport
		if err := json.Unmarshal(h.stdout.Bytes(), &out); err != nil || !out.Proposed {
			t.Fatalf("json report does not mark the proposal: %s, %v", h.stdout.String(), err)
		}
	}
	plain := &harness{gates: &fakeGates{report: passing}}
	plain.run(t, "gate", "--format", "json")
	if strings.Contains(plain.stdout.String(), "proposed") {
		t.Fatalf("a run against the applied config is marked as a proposal: %s", plain.stdout.String())
	}
}

func TestDisabledChecksAreAlwaysShown(t *testing.T) {
	report := passing
	report.Disabled = []domain.DisabledResult{{DisabledCheck: domain.DisabledCheck{Check: "structure/function-length", Reason: "upstream style"}, Suppressed: 3}}
	h := &harness{gates: &fakeGates{report: report}}
	if code := h.run(t, "gate"); code != 0 || !strings.Contains(h.stderr.String(), "disabled: structure/function-length (hid 3 findings): upstream style") {
		t.Fatalf("exit %d stderr %q", code, h.stderr.String())
	}
	h = &harness{gates: &fakeGates{report: report}}
	h.run(t, "gate", "--format", "json")
	var out jsonReport
	if err := json.Unmarshal(h.stdout.Bytes(), &out); err != nil || len(out.Disabled) != 1 || out.Disabled[0].Suppressed != 3 {
		t.Fatalf("json disabled = %+v, %v", out.Disabled, err)
	}
}

func TestDisabledPathsAreShown(t *testing.T) {
	pattern, err := domain.ParsePattern("tests/**", domain.RecursivePattern)
	if err != nil {
		t.Fatal(err)
	}
	report := passing
	report.Disabled = []domain.DisabledResult{{DisabledCheck: domain.DisabledCheck{Check: "shell/strict-mode", Reason: "the harness enables errexit per test", Paths: []domain.Pattern{pattern}}, Suppressed: 2}}
	h := &harness{gates: &fakeGates{report: report}}
	if h.run(t, "gate"); !strings.Contains(h.stderr.String(), "disabled: shell/strict-mode in tests/** (hid 2 findings): the harness") {
		t.Fatalf("stderr %q", h.stderr.String())
	}
	h = &harness{gates: &fakeGates{report: report}}
	h.run(t, "gate", "--format", "json")
	var out jsonReport
	if err := json.Unmarshal(h.stdout.Bytes(), &out); err != nil || len(out.Disabled) != 1 || !slices.Equal(out.Disabled[0].Paths, []string{"tests/**"}) {
		t.Fatalf("json disabled = %+v, %v", out.Disabled, err)
	}
}
