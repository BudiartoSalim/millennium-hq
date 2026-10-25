package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"millennium/internal/application/usecase"
	"millennium/internal/domain"
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
	enabled bool
	err     error
}

func (f fakeConfig) Execute(context.Context) (domain.Config, bool, error) {
	return domain.Config{}, f.enabled, f.err
}

type harness struct {
	gates          *fakeGates
	config         fakeConfig
	depsErr        error
	stdout, stderr bytes.Buffer
}

func (h *harness) run(t *testing.T, args ...string) int {
	t.Helper()
	if h.gates == nil {
		h.gates = &fakeGates{}
	}
	deps := func(context.Context) (Deps, error) {
		return Deps{Gates: h.gates, Config: h.config}, h.depsErr
	}
	return New("1.2.3", &h.stdout, &h.stderr, deps).Run(t.Context(), args)
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
		{"version", []string{"version"}, 0, "millennium 1.2.3"},
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
