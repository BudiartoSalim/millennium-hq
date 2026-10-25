package gate

import (
	"context"
	"fmt"
	"path"
	"strings"

	"millennium/internal/application/port"
	"millennium/internal/domain"
)

// Shell checks shell scripts: they must parse, executable scripts must
// switch strict mode on, and shellcheck must find nothing on the lines a
// change touched. Existing scripts without strict mode are reported by an
// audit, not by every change to them.
type Shell struct {
	source port.SourceReader
	tools  port.ShellChecker
}

// NewShell returns the shell gate.
func NewShell(source port.SourceReader, tools port.ShellChecker) *Shell {
	return &Shell{source: source, tools: tools}
}

// Name identifies the gate in findings.
func (*Shell) Name() string { return "shell" }

// Run checks every added or modified script of a component that lists shell.
func (g *Shell) Run(ctx context.Context, in port.GateInput) ([]domain.Finding, error) {
	var findings []domain.Finding
	for _, file := range in.Changes.Files {
		if file.Kind == domain.ChangeDeleted || !ownsShell(in.Config, file.Path) {
			continue
		}
		content, err := g.source.ReadWorking(file.Path)
		if err != nil {
			findings = append(findings, domain.Finding{Rule: "analysis-failed", File: file.Path, Message: err.Error()})
			continue
		}
		if lang, ok := domain.DetectLanguage(file.Path, content); !ok || lang != domain.LanguageShell {
			continue
		}
		fileFindings, err := g.checkScript(ctx, in, file, content)
		if err != nil {
			return nil, err
		}
		findings = append(findings, fileFindings...)
	}
	return label(g.Name(), findings), nil
}

// ownsShell reports whether a component that lists shell owns path; files
// elsewhere are never read.
func ownsShell(cfg domain.Config, p string) bool {
	match, ok, err := cfg.ComponentFor(p)
	return err == nil && ok && match.Component.Has(domain.LanguageShell)
}

func (g *Shell) checkScript(ctx context.Context, in port.GateInput, file domain.ChangedFile, content []byte) ([]domain.Finding, error) {
	syntax, err := g.tools.Syntax(ctx, file.Path)
	if err != nil {
		return nil, err
	}
	if len(syntax) > 0 {
		// A script that does not parse cannot be linted meaningfully.
		return issueFindings("syntax-error", file.Path, syntax), nil
	}
	findings, err := g.strictMode(ctx, in.Changes.Base, file, content)
	if err != nil {
		return nil, err
	}
	if in.Config.ShellLint == domain.ShellLintOff {
		return findings, nil
	}
	lint, err := g.lint(ctx, in.Changes.Base, file, content)
	return append(findings, lint...), err
}

func (g *Shell) strictMode(ctx context.Context, base string, file domain.ChangedFile, content []byte) ([]domain.Finding, error) {
	missing := domain.StrictModeMissing(content)
	if len(missing) == 0 {
		return nil, nil
	}
	if base != "" && file.Kind == domain.ChangeModified {
		old, found, err := g.source.ReadAt(ctx, base, file.Path)
		if err != nil {
			return nil, err
		}
		if found && len(domain.StrictModeMissing(old)) > 0 {
			return nil, nil
		}
	}
	return []domain.Finding{{Rule: "strict-mode", File: file.Path, Line: 1, Message: fmt.Sprintf(
		"executable scripts must enable %s near the top (set -euo pipefail for bash)", strings.Join(missing, ", "))}}, nil
}

func (g *Shell) lint(ctx context.Context, base string, file domain.ChangedFile, content []byte) ([]domain.Finding, error) {
	issues, err := g.tools.Lint(ctx, file.Path, lintDialect(file.Path, content))
	if err != nil {
		return nil, err
	}
	touched := domain.AllLines()
	if base != "" && file.Kind == domain.ChangeModified {
		if touched, err = g.source.ChangedLines(ctx, base, file.Path); err != nil {
			return nil, err
		}
	}
	var kept []domain.LintIssue
	for _, issue := range issues {
		if touched.Contains(issue.Line) {
			kept = append(kept, issue)
		}
	}
	return issueFindings("shellcheck", file.Path, kept), nil
}

// lintDialect tells shellcheck to read a sourced library without a shebang
// or shell directive as bash; otherwise shellcheck decides from the file.
func lintDialect(p string, content []byte) string {
	if _, ok := domain.ShellDialect(content); ok || path.Ext(p) == ".bash" || strings.Contains(string(content), "shellcheck shell=") {
		return ""
	}
	return "bash"
}

func issueFindings(rule, file string, issues []domain.LintIssue) []domain.Finding {
	findings := make([]domain.Finding, 0, len(issues))
	for _, issue := range issues {
		message := issue.Message
		if issue.Code != "" && issue.Code != "syntax" {
			message = issue.Code + ": " + message
		}
		findings = append(findings, domain.Finding{Rule: rule, File: file, Line: issue.Line, Message: message})
	}
	return findings
}
