package usecase

import (
	"context"
	"fmt"

	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/domain"
)

// AuditInput limits an audit to part of the repository.
type AuditInput struct {
	// Path is a repository-relative directory or file; empty audits everything.
	Path string
	Tier domain.Tier
	// Config, when set, audits against it instead of the repository's
	// config: a dry run of a proposal before the user applies it.
	Config *domain.Config
}

// RunAudit runs every gate over existing code instead of a change, holding
// every file to the full rules. Its report is the starting point for
// refactoring; hooks never run it.
type RunAudit struct {
	config *LoadConfig
	repo   port.Repository
	gates  []port.Gate
}

// NewRunAudit returns the audit use-case.
func NewRunAudit(config *LoadConfig, repo port.Repository, gates []port.Gate) *RunAudit {
	return &RunAudit{config: config, repo: repo, gates: gates}
}

// Execute audits the files under in.Path. An audit needs a config: without
// one there are no rules to audit against.
func (u *RunAudit) Execute(ctx context.Context, in AuditInput) (domain.Report, error) {
	if in.Path != "" && !domain.IsRepoRelative(in.Path) {
		return domain.Report{}, fmt.Errorf("audit path %q must be inside the repository", in.Path)
	}
	cfg, err := u.auditConfig(ctx, in)
	if err != nil {
		return domain.Report{}, err
	}
	paths, err := u.repo.Files(ctx)
	if err != nil {
		return domain.Report{}, fmt.Errorf("list files: %w", err)
	}
	// An empty Base tells the gates there is no "before": every file is new
	// code to them.
	changes := domain.ChangeSet{}
	for _, p := range paths {
		if domain.Within(p, in.Path) {
			changes.Files = append(changes.Files, domain.ChangedFile{Path: p, Kind: domain.ChangeAdded})
		}
	}
	changes.Files = withoutExcluded(cfg, changes.Files)
	return runAll(ctx, u.gates, port.GateInput{Config: cfg, Changes: changes, Tier: tierOrFull(in.Tier)})
}

func (u *RunAudit) auditConfig(ctx context.Context, in AuditInput) (domain.Config, error) {
	if in.Config != nil {
		return *in.Config, nil
	}
	cfg, enabled, err := u.config.Execute(ctx)
	if err != nil {
		return domain.Config{}, err
	}
	if !enabled {
		return domain.Config{}, ErrConfigRequired
	}
	return cfg, nil
}
