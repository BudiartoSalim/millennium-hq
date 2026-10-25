package usecase

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"millennium/internal/application/port"
	"millennium/internal/domain"
)

// PluginID is how the project's Claude settings name the plugin: plugin
// "millennium" from marketplace "millennium-hq".
const PluginID = "millennium@millennium-hq"

// ProposalFile is where agents write proposals, at the repository root.
const ProposalFile = "millennium.proposal.json"

// WorktreesDir is where Claude Code puts subagent worktrees.
const WorktreesDir = ".claude/worktrees/"

// ApplyTarget says where a config goes.
type ApplyTarget int

const (
	// ApplyCommitted writes the repository's own config and the files around
	// it: the Claude settings, the AGENTS.md section, .gitignore.
	ApplyCommitted ApplyTarget = iota
	// ApplyLocal writes only the local config, outside the repository.
	ApplyLocal
)

// ApplyPlan is a validated proposal, ready for the user to confirm.
type ApplyPlan struct {
	Target ApplyTarget
	// Config is nil when the proposal only approves dependencies.
	Config     *domain.Config
	Canonical  []byte
	Approvals  []domain.Approval
	Loosenings []string
	// Warning is set when the current config could not be read, so its
	// loosenings could not be listed.
	Warning string
	// Digest identifies this exact proposal for --confirm.
	Digest string
	Writes []string
}

// ConfigFiles are the two places a config, and its dependency approvals,
// can live.
type ConfigFiles struct {
	Committed     port.ConfigFile
	CommittedPath string
	Local         port.ConfigFile
	LocalPath     string
	// CommittedApprovals and LocalApprovals are the approval directories.
	CommittedApprovals string
	LocalApprovals     string
}

// ApplyConfig is the only way the rules change: it validates a proposal,
// lists what it loosens, and, once the user confirms, writes it.
type ApplyConfig struct {
	codec  port.ProposalCodec
	files  ConfigFiles
	writer port.ProjectWriter
}

// NewApplyConfig returns the apply use-case.
func NewApplyConfig(codec port.ProposalCodec, files ConfigFiles, writer port.ProjectWriter) *ApplyConfig {
	return &ApplyConfig{codec: codec, files: files, writer: writer}
}

// Prepare validates a proposal and compares it with the current config.
func (u *ApplyConfig) Prepare(proposal []byte, target ApplyTarget) (ApplyPlan, error) {
	p, err := u.codec.ParseProposal(proposal)
	if err != nil {
		return ApplyPlan{}, err
	}
	sum := sha256.Sum256(proposal)
	plan := ApplyPlan{Target: target, Config: p.Config, Canonical: p.Canonical, Approvals: p.Approvals, Digest: hex.EncodeToString(sum[:6])}
	if p.Config != nil {
		u.compare(&plan)
		plan.Writes = u.writes(target)
	}
	for _, a := range p.Approvals {
		plan.Writes = append(plan.Writes, fmt.Sprintf("%s: approves %s (%s)", u.approvalPath(target, a), a, a.Reason))
	}
	return plan, nil
}

// compare lists what the proposed config loosens against the current one.
func (u *ApplyConfig) compare(plan *ApplyPlan) {
	current, err := u.file(plan.Target).Load()
	switch {
	case err == nil:
		plan.Loosenings = domain.Loosenings(&current, *plan.Config)
	case !errors.Is(err, port.ErrConfigNotFound):
		plan.Warning = "the current config cannot be read, so what this proposal loosens cannot be listed: " + err.Error()
	}
}

func (u *ApplyConfig) approvalPath(target ApplyTarget, a domain.Approval) string {
	dir := u.files.CommittedApprovals
	if target == ApplyLocal {
		dir = u.files.LocalApprovals
	}
	return filepath.Join(dir, filepath.FromSlash(domain.ApprovalFile(a.Package)))
}

func (u *ApplyConfig) file(target ApplyTarget) port.ConfigFile {
	if target == ApplyLocal {
		return u.files.Local
	}
	return u.files.Committed
}

func (u *ApplyConfig) writes(target ApplyTarget) []string {
	if target == ApplyLocal {
		return []string{u.files.LocalPath + " (outside the repository, only on this machine)"}
	}
	return []string{
		u.files.CommittedPath,
		".claude/settings.json: the plugin enabled for the project, the sandbox on with unsandboxed commands off, .millennium write-protected, the package registries and OSV reachable for the full tier, subagent worktrees branched from HEAD",
		"AGENTS.md: the millennium section (and an @AGENTS.md import in CLAUDE.md, if there is one)",
		".gitignore: " + ProposalFile + " and " + WorktreesDir,
	}
}

// Apply writes a confirmed plan: the approval records, then the config.
func (u *ApplyConfig) Apply(plan ApplyPlan) error {
	for _, a := range plan.Approvals {
		record, err := u.codec.FormatApproval(a)
		if err != nil {
			return err
		}
		if err := u.writer.WriteFile(u.approvalPath(plan.Target, a), record); err != nil {
			return err
		}
	}
	if plan.Config == nil {
		return nil
	}
	if plan.Target == ApplyLocal {
		return u.writer.WriteFile(u.files.LocalPath, plan.Canonical)
	}
	steps := []func() error{
		func() error { return u.writer.WriteFile(u.files.CommittedPath, plan.Canonical) },
		func() error { return u.writer.MergeSettings(PluginID) },
		func() error { return u.writer.SetAgentsSection(AgentsSection(*plan.Config)) },
		func() error { return u.writer.Ignore(ProposalFile) },
		func() error { return u.writer.Ignore(WorktreesDir) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

// Export returns a config as a proposal, so it can be backed up or moved
// to another machine and applied again.
func (u *ApplyConfig) Export(target ApplyTarget) ([]byte, error) {
	raw, err := u.file(target).Raw()
	if err != nil {
		return nil, err
	}
	return u.codec.FormatProposal(raw)
}

// AgentsSection is the AGENTS.md block that tells every agent, in any
// harness, how this repository is checked.
func AgentsSection(cfg domain.Config) string {
	limits := domain.DefaultLimits()
	var b strings.Builder
	b.WriteString("## Quality gates (millennium)\n\n")
	b.WriteString("This repository is checked by millennium. Before calling any change done, run\n")
	b.WriteString("`millennium gate --tier full` and fix every finding; hooks run the fast tier\n")
	b.WriteString("whenever an agent stops.\n\n")
	for _, c := range cfg.Components {
		fmt.Fprintf(&b, "- `%s`: %s%s\n", c.Name(), joinLanguages(c.Languages), conventionsNote(c.Conventions))
	}
	fmt.Fprintf(&b, "- Functions: at most %d lines, nesting depth %d, cognitive complexity %d.\n", limits.FunctionLines, limits.Nesting, limits.Cognitive)
	b.WriteString("- Changed logic in tested layers needs changed tests, and every changed line must run in a test.\n")
	b.WriteString("- A suppression or waiver needs a reason (`-- <reason>`); TODOs need an issue link.\n")
	b.WriteString("- Never edit `.millennium/`: propose rule changes in `" + ProposalFile + "`; the user applies them.\n")
	b.WriteString("- `millennium config explain <file>` shows which rules apply to a file.\n")
	return b.String()
}

func joinLanguages(langs []domain.Language) string {
	names := make([]string, len(langs))
	for i, l := range langs {
		names[i] = string(l)
	}
	return strings.Join(names, ", ")
}

func conventionsNote(conventions []domain.Convention) string {
	if len(conventions) == 0 {
		return ""
	}
	names := make([]string, len(conventions))
	for i, c := range conventions {
		names[i] = string(c)
	}
	return " (" + strings.Join(names, ", ") + ")"
}
