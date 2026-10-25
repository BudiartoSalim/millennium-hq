package gate

import (
	"context"
	"errors"
	"strings"
	"testing"

	"millennium/internal/application/port"
	"millennium/internal/domain"
)

type fakeShellTools struct {
	syntax  map[string][]domain.LintIssue
	lint    map[string][]domain.LintIssue
	err     error
	lintErr error
	linted  []string
}

func (f *fakeShellTools) Syntax(_ context.Context, path string) ([]domain.LintIssue, error) {
	return f.syntax[path], f.err
}

func (f *fakeShellTools) Lint(_ context.Context, path, dialect string) ([]domain.LintIssue, error) {
	f.linted = append(f.linted, path+"|"+dialect)
	return f.lint[path], f.lintErr
}

const strictScript = "#!/usr/bin/env bash\nset -euo pipefail\necho hi\n"

func shellConfig(t *testing.T, lint string) domain.Config {
	t.Helper()
	in := domain.ConfigInput{Version: ptr(1), Components: []domain.ComponentInput{
		{Paths: []string{"scripts"}, Languages: []string{"shell"}},
		{Paths: []string{"svc"}, Languages: []string{"go"}},
	}}
	if lint != "" {
		in.ShellLint = &lint
	}
	cfg, err := domain.NewConfig(in)
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	return cfg
}

func runShell(t *testing.T, cfg domain.Config, source *fakeSource, tools *fakeShellTools, base string, files ...domain.ChangedFile) []domain.Finding {
	t.Helper()
	findings, err := NewShell(source, tools).Run(t.Context(), port.GateInput{Config: cfg, Changes: domain.ChangeSet{Base: base, Files: files}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return findings
}

func rules(findings []domain.Finding) []string {
	var out []string
	for _, f := range findings {
		out = append(out, f.Gate+"/"+f.Rule)
	}
	return out
}

func TestShellGate(t *testing.T) {
	sc2086 := []domain.LintIssue{{Line: 3, Code: "SC2086", Message: "Double quote"}}
	tests := []struct {
		name    string
		path    string
		content string
		tools   *fakeShellTools
		want    string
	}{
		{"clean script", "scripts/a.sh", strictScript, &fakeShellTools{}, ""},
		{"syntax error stops there", "scripts/a.sh", strictScript, &fakeShellTools{
			syntax: map[string][]domain.LintIssue{"scripts/a.sh": {{Line: 2, Code: "syntax", Message: "unexpected token"}}},
			lint:   map[string][]domain.LintIssue{"scripts/a.sh": sc2086},
		}, "shell/syntax-error"},
		{"no strict mode", "scripts/a.sh", "#!/bin/bash\necho hi\n", &fakeShellTools{}, "shell/strict-mode"},
		{"shellcheck finding", "scripts/a.sh", strictScript, &fakeShellTools{lint: map[string][]domain.LintIssue{"scripts/a.sh": sc2086}}, "shell/shellcheck"},
		{"extensionless script by shebang", "scripts/bin/deploy", "#!/bin/bash\necho hi\n", &fakeShellTools{}, "shell/strict-mode"},
		{"extensionless non-script is ignored", "scripts/bin/data", "hello\n", &fakeShellTools{}, ""},
		{"outside a shell component", "svc/x.sh", "#!/bin/bash\n", &fakeShellTools{}, ""},
		{"library without shebang needs no strict mode", "scripts/lib.sh", "helper() { :; }\n", &fakeShellTools{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := &fakeSource{working: map[string]string{tt.path: tt.content}}
			got := rules(runShell(t, shellConfig(t, ""), source, tt.tools, "", added(tt.path)))
			if (tt.want == "" && len(got) > 0) || (tt.want != "" && (len(got) != 1 || got[0] != tt.want)) {
				t.Fatalf("findings %q, want %q", got, tt.want)
			}
		})
	}
}

func added(path string) domain.ChangedFile {
	return domain.ChangedFile{Path: path, Kind: domain.ChangeAdded}
}

func TestShellGateOnlyReportsTouchedLinesOfExistingScripts(t *testing.T) {
	issues := []domain.LintIssue{{Line: 3, Code: "SC2086", Message: "old"}, {Line: 7, Code: "SC2034", Message: "new"}}
	tools := &fakeShellTools{lint: map[string][]domain.LintIssue{"scripts/a.sh": issues}}
	source := &fakeSource{
		working: map[string]string{"scripts/a.sh": "#!/bin/bash\necho\n"},
		base:    map[string]string{"scripts/a.sh": "#!/bin/bash\necho\n"},
		changed: map[string]domain.LineSet{"scripts/a.sh": {Ranges: []domain.LineRange{{First: 7, Last: 7}}}},
	}
	findings := runShell(t, shellConfig(t, ""), source, tools, "tree1", modified("scripts/a.sh"))
	if len(findings) != 1 || !strings.Contains(findings[0].Message, "SC2034: new") {
		t.Fatalf("want only the finding on the touched line, got %+v", findings)
	}
	// The script lacked strict mode before the change too, so that is left
	// to an audit; an audit (no base) reports everything.
	all := runShell(t, shellConfig(t, ""), source, tools, "", modified("scripts/a.sh"))
	if got := rules(all); len(got) != 3 || got[0] != "shell/strict-mode" {
		t.Fatalf("audit findings = %q", got)
	}
}

func TestShellGateLintOffAndDialect(t *testing.T) {
	tools := &fakeShellTools{lint: map[string][]domain.LintIssue{"scripts/a.sh": {{Line: 3, Code: "SC2086"}}}}
	source := &fakeSource{working: map[string]string{"scripts/a.sh": strictScript, "scripts/lib.sh": "f() { :; }\n"}}
	if findings := runShell(t, shellConfig(t, "off"), source, tools, "", added("scripts/a.sh")); len(findings) != 0 {
		t.Fatalf("shell.lint off: %+v", findings)
	}
	runShell(t, shellConfig(t, ""), source, tools, "", added("scripts/a.sh"), added("scripts/lib.sh"))
	if want := []string{"scripts/a.sh|", "scripts/lib.sh|bash"}; strings.Join(tools.linted, ",") != strings.Join(want, ",") {
		t.Fatalf("linted %q, want %q", tools.linted, want)
	}
}

func TestShellGateFailsWhenToolsFail(t *testing.T) {
	tools := &fakeShellTools{err: errors.New("bash crashed")}
	source := &fakeSource{working: map[string]string{"scripts/a.sh": strictScript}}
	_, err := NewShell(source, tools).Run(t.Context(), port.GateInput{Config: shellConfig(t, ""), Changes: domain.ChangeSet{Files: []domain.ChangedFile{added("scripts/a.sh")}}})
	if err == nil {
		t.Fatal("a failing tool must fail the gate, which the run turns into a blocking finding")
	}
}
