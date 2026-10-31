package usecase

import (
	"context"
	"fmt"
	"strings"

	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/domain"
)

// ScopeInput is a work package's boundary: what the branch is compared
// against and the paths the package may change.
type ScopeInput struct {
	Base  string
	Allow []string
}

// CheckScope verifies that an engineer's branch stays inside its work
// package before the tech lead merges it, however the files were written:
// every changed path must match an allowed pattern, and none may be a
// protected path or an acceptance test.
type CheckScope struct {
	config *LoadConfig
	repo   port.Repository
}

// NewCheckScope returns the scope-check use-case.
func NewCheckScope(config *LoadConfig, repo port.Repository) *CheckScope {
	return &CheckScope{config: config, repo: repo}
}

// Execute checks the change set against the package's paths.
func (u *CheckScope) Execute(ctx context.Context, in ScopeInput) (domain.Report, error) {
	allowed, err := parseAllowed(in.Allow)
	if err != nil {
		return domain.Report{}, err
	}
	cfg, enabled, err := u.config.Execute(ctx)
	if err != nil {
		return domain.Report{}, err
	}
	if !enabled {
		return domain.Report{}, ErrConfigRequired
	}
	changes, err := u.repo.Changes(ctx, in.Base)
	if err != nil {
		return domain.Report{}, fmt.Errorf("detect changes: %w", err)
	}
	result := domain.GateResult{Gate: "scope"}
	for _, f := range changes.Files {
		if finding, ok := scopeFinding(cfg, allowed, f.Path); ok {
			result.Findings = append(result.Findings, finding)
		}
	}
	return domain.Report{Enabled: true, ChangedFiles: len(changes.Files), Results: []domain.GateResult{result}}, nil
}

func parseAllowed(raw []string) ([]domain.Pattern, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("a scope check needs the package's paths (--allow)")
	}
	patterns := make([]domain.Pattern, 0, len(raw))
	for _, r := range raw {
		p, err := domain.ParsePattern(r, domain.RecursivePattern)
		if err != nil {
			return nil, fmt.Errorf("--allow: %w", err)
		}
		patterns = append(patterns, p)
	}
	return patterns, nil
}

func scopeFinding(cfg domain.Config, allowed []domain.Pattern, p string) (domain.Finding, bool) {
	for _, protected := range domain.ProtectedPaths() {
		if domain.Within(p, protected) {
			return domain.Finding{Gate: "scope", Rule: "protected-path", File: p, Message: "the branch changes " + protected + ", which no agent may change"}, true
		}
	}
	if domain.Within(p, cfg.AcceptanceTests) {
		return domain.Finding{Gate: "scope", Rule: "acceptance-test-changed", File: p, Message: "the branch changes an acceptance test; only qa-tester changes those"}, true
	}
	for _, pattern := range allowed {
		if pattern.Covers(p) {
			return domain.Finding{}, false
		}
	}
	return domain.Finding{Gate: "scope", Rule: "out-of-scope", File: p, Message: "outside the work package (" + patternList(allowed) + ")"}, true
}

func patternList(patterns []domain.Pattern) string {
	names := make([]string, len(patterns))
	for i, p := range patterns {
		names[i] = p.String()
	}
	return strings.Join(names, ", ")
}
