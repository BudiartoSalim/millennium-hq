package usecase

import (
	"context"
	"fmt"

	"millennium/internal/application/port"
	"millennium/internal/domain"
)

// ConfigRequirement says whether a repository without a config is acceptable.
type ConfigRequirement int

const (
	// ConfigOptional treats a repository without a config as not opted in.
	// Claude Code hooks use it, so the plugin stays out of other projects.
	ConfigOptional ConfigRequirement = iota
	// ConfigRequired blocks when the config is missing. CI uses it.
	ConfigRequired
)

// RunGatesInput selects what to compare and how to treat a missing config.
type RunGatesInput struct {
	// Base is the revision to compare the working tree against; empty means HEAD.
	Base   string
	Config ConfigRequirement
}

// RunGates runs every gate over the change set and collects the report.
type RunGates struct {
	config *LoadConfig
	repo   port.Repository
	gates  []port.Gate
}

// NewRunGates returns the run-gates use-case.
func NewRunGates(config *LoadConfig, repo port.Repository, gates []port.Gate) *RunGates {
	return &RunGates{config: config, repo: repo, gates: gates}
}

// Execute runs the gates. An error means the run could not decide and must
// be treated as a block by the caller.
func (u *RunGates) Execute(ctx context.Context, in RunGatesInput) (domain.Report, error) {
	cfg, enabled, err := u.config.Execute(ctx)
	if err != nil {
		return domain.Report{}, err
	}
	if !enabled {
		if in.Config == ConfigRequired {
			return domain.Report{}, ErrConfigRequired
		}
		return domain.Report{}, nil
	}
	changes, err := u.repo.Changes(ctx, in.Base)
	if err != nil {
		return domain.Report{}, fmt.Errorf("detect changes: %w", err)
	}
	gateInput := port.GateInput{Config: cfg, Changes: changes}
	report := domain.Report{Enabled: true, ChangedFiles: len(changes)}
	for _, g := range u.gates {
		report.Results = append(report.Results, runGate(ctx, g, gateInput))
	}
	return report, nil
}

// runGate runs one gate and converts an error or a panic into a blocking
// finding, so a broken gate blocks instead of passing.
func runGate(ctx context.Context, g port.Gate, in port.GateInput) (result domain.GateResult) {
	name := g.Name()
	defer func() {
		if r := recover(); r != nil {
			result = gateFailed(name, fmt.Sprintf("panicked: %v", r))
		}
	}()
	findings, err := g.Run(ctx, in)
	if err != nil {
		return gateFailed(name, err.Error())
	}
	return domain.GateResult{Gate: name, Findings: findings}
}

func gateFailed(gate, reason string) domain.GateResult {
	return domain.GateResult{Gate: gate, Findings: []domain.Finding{{
		Gate:    gate,
		Rule:    "gate-failed",
		Message: "the gate could not run, so the change is blocked: " + reason,
	}}}
}
