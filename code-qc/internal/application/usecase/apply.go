package usecase

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/domain"
)

// PluginID is how the project's Claude settings name the plugin: plugin
// "millennium" from marketplace "millennium-hq".
const PluginID = "millennium@millennium-hq"

// ProposalFile is where agents write proposals, at the repository root.
const ProposalFile = "millennium.proposal.json"

// ProposalDir holds what a proposal refers to, such as draft convention
// packs, beside ProposalFile; it is git-ignored like the proposal.
const ProposalDir = "millennium.proposal"

// WorktreesDirs are where agent worktrees go: Claude Code's subagents, and
// the agents millennium's pi extension starts.
func WorktreesDirs() []string { return []string{".claude/worktrees/", ".pi/worktrees/"} }

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
	// Config is the config in force once the plan is applied, resolved
	// against its packs: the proposed config, or the current one with the
	// proposed packs. It is nil when the plan touches neither.
	Config *domain.Config
	// Canonical is the proposed config as it will be written; nil keeps the
	// current config file.
	Canonical  []byte
	Packs      []port.ProposedPack
	Approvals  []domain.Approval
	Loosenings []string
	// Warning is set when the current config could not be read, so its
	// loosenings could not be listed.
	Warning string
	// Digest identifies this exact proposal, its packs' files included, for
	// --confirm.
	Digest string
	Writes []string
}

// ConfigFiles are the two places a config, its convention packs and its
// dependency approvals can live.
type ConfigFiles struct {
	Committed     port.ConfigFile
	CommittedPath string
	Local         port.ConfigFile
	LocalPath     string
	// CommittedApprovals and LocalApprovals are the approval directories.
	CommittedApprovals string
	LocalApprovals     string
	// CommittedPacks and LocalPacks are the convention pack directories.
	CommittedPacks string
	LocalPacks     string
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

// Prepare validates a proposal, whose pack folders are relative to dir, and
// compares it with the current config.
func (u *ApplyConfig) Prepare(proposal []byte, dir string, target ApplyTarget) (ApplyPlan, error) {
	p, err := u.codec.ParseProposal(proposal, dir)
	if err != nil {
		return ApplyPlan{}, err
	}
	plan := ApplyPlan{Target: target, Canonical: p.Canonical, Packs: p.Packs, Approvals: p.Approvals, Digest: digest(proposal, p.Packs)}
	if p.Config != nil || len(p.Packs) > 0 {
		if err := u.resolve(&plan, p.Config); err != nil {
			return ApplyPlan{}, err
		}
		plan.Writes = u.writes(plan)
	}
	for _, a := range p.Approvals {
		plan.Writes = append(plan.Writes, fmt.Sprintf("%s: approves %s (%s)", u.approvalPath(target, a), a, a.Reason))
	}
	return plan, nil
}

// digest identifies the proposal and every pack file it would write, so a
// pack edited after the user saw the plan cannot be applied under it.
func digest(proposal []byte, packs []port.ProposedPack) string {
	h := sha256.New()
	h.Write(proposal)
	for _, p := range packs {
		for _, data := range [][]byte{p.Files.Pack, p.Files.Guide} {
			fmt.Fprintf(h, "\x00%s\x00%d\x00", p.Pack.Name, len(data))
			h.Write(data)
		}
	}
	return hex.EncodeToString(h.Sum(nil)[:6])
}

// resolve checks the proposal against the packs it would leave in place,
// and lists what it loosens against the current config.
func (u *ApplyConfig) resolve(plan *ApplyPlan, proposed *domain.Config) error {
	file := u.file(plan.Target)
	catalog, err := file.Packs()
	if err != nil {
		return fmt.Errorf("the current convention packs cannot be read, so the proposal cannot be checked against them; fix or remove the broken pack by hand: %w", err)
	}
	packs := make([]domain.Pack, len(plan.Packs))
	for i, p := range plan.Packs {
		packs[i] = p.Pack
	}
	if catalog, err = catalog.With(packs); err != nil {
		return fmt.Errorf("proposal packs: %w", err)
	}
	current, err := file.Load()
	switch {
	case errors.Is(err, port.ErrConfigNotFound):
	case err != nil && proposed == nil:
		return fmt.Errorf("the current config cannot be read, so the proposed packs cannot be checked against it: %w", err)
	case err != nil:
		plan.Warning = "the current config cannot be read, so what this proposal loosens cannot be listed: " + err.Error()
	}
	next := proposed
	if next == nil && err == nil {
		next = &current
	}
	if next == nil {
		return nil
	}
	resolved, rerr := next.Resolve(catalog)
	if rerr != nil {
		return fmt.Errorf("with the proposed packs: %w", rerr)
	}
	plan.Config = &resolved
	if err == nil {
		plan.Loosenings = domain.Loosenings(&current, resolved)
	}
	return nil
}

func (u *ApplyConfig) approvalPath(target ApplyTarget, a domain.Approval) string {
	dir := u.files.CommittedApprovals
	if target == ApplyLocal {
		dir = u.files.LocalApprovals
	}
	return filepath.Join(dir, filepath.FromSlash(domain.ApprovalFile(a.Package)))
}

func (u *ApplyConfig) packDir(target ApplyTarget, name domain.Convention) string {
	dir := u.files.CommittedPacks
	if target == ApplyLocal {
		dir = u.files.LocalPacks
	}
	return filepath.Join(dir, string(name))
}

func (u *ApplyConfig) file(target ApplyTarget) port.ConfigFile {
	if target == ApplyLocal {
		return u.files.Local
	}
	return u.files.Committed
}

func (u *ApplyConfig) writes(plan ApplyPlan) []string {
	var out []string
	for _, p := range plan.Packs {
		out = append(out, fmt.Sprintf("%s: convention %s (pack.json, guide.md)", u.packDir(plan.Target, p.Pack.Name), p.Pack.Name))
	}
	switch {
	case plan.Target == ApplyLocal && plan.Canonical != nil:
		out = append(out, u.files.LocalPath+" (outside the repository, only on this machine)")
	case plan.Target == ApplyLocal:
	case plan.Canonical != nil:
		out = append(out,
			u.files.CommittedPath,
			".claude/settings.json: the plugin enabled for the project, the sandbox on with unsandboxed commands off, .millennium write-protected, the package registries and OSV reachable for the full tier, subagent worktrees branched from HEAD",
			"AGENTS.md: the millennium section (and an @AGENTS.md import in CLAUDE.md, if there is one)",
			".gitignore: "+ProposalFile+", "+ProposalDir+"/ and the agent worktrees ("+strings.Join(WorktreesDirs(), ", ")+")",
		)
	case plan.Config != nil:
		out = append(out, "AGENTS.md: the millennium section, with the packs' limits")
	}
	return out
}

// Apply writes a confirmed plan: the convention packs, the approval
// records, then the config and the files around it.
func (u *ApplyConfig) Apply(plan ApplyPlan) error {
	for _, p := range plan.Packs {
		dir := u.packDir(plan.Target, p.Pack.Name)
		if err := u.writer.WriteFile(filepath.Join(dir, "pack.json"), p.Files.Pack); err != nil {
			return err
		}
		if err := u.writer.WriteFile(filepath.Join(dir, "guide.md"), p.Files.Guide); err != nil {
			return err
		}
	}
	for _, a := range plan.Approvals {
		record, err := u.codec.FormatApproval(a)
		if err != nil {
			return err
		}
		if err := u.writer.WriteFile(u.approvalPath(plan.Target, a), record); err != nil {
			return err
		}
	}
	return runSteps(u.configSteps(plan))
}

func (u *ApplyConfig) configSteps(plan ApplyPlan) []func() error {
	switch {
	case plan.Config == nil:
		return nil
	case plan.Target == ApplyLocal && plan.Canonical == nil:
		return nil
	case plan.Target == ApplyLocal:
		return []func() error{func() error { return u.writer.WriteFile(u.files.LocalPath, plan.Canonical) }}
	case plan.Canonical == nil:
		return []func() error{func() error { return u.writer.SetAgentsSection(AgentsSection(*plan.Config)) }}
	}
	steps := []func() error{
		func() error { return u.writer.WriteFile(u.files.CommittedPath, plan.Canonical) },
		func() error { return u.writer.MergeSettings(PluginID) },
		func() error { return u.writer.SetAgentsSection(AgentsSection(*plan.Config)) },
		func() error { return u.writer.Ignore(ProposalFile) },
		func() error { return u.writer.Ignore(ProposalDir + "/") },
	}
	for _, dir := range WorktreesDirs() {
		steps = append(steps, func() error { return u.writer.Ignore(dir) })
	}
	return steps
}

func runSteps(steps []func() error) error {
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
	var b strings.Builder
	b.WriteString("## Quality gates (millennium)\n\n")
	b.WriteString("This repository is checked by millennium. Before calling any change done, run\n")
	b.WriteString("`millennium-code-qc gate --tier full` and fix every finding; hooks run the fast tier\n")
	b.WriteString("whenever an agent stops.\n\n")
	for _, c := range cfg.Components {
		limits := cfg.LimitsFor(c)
		fmt.Fprintf(&b, "- `%s`: %s%s; functions at most %d lines, nesting depth %d, cognitive complexity %d.\n",
			c.Name(), joinLanguages(c.Languages), conventionsNote(c.Conventions), limits.FunctionLines, limits.Nesting, limits.Cognitive)
	}
	if len(cfg.Packs) > 0 {
		b.WriteString("- Follow each convention's guide: `millennium-code-qc convention show <name>`.\n")
	}
	b.WriteString("- Changed logic in tested layers needs changed tests, and every changed line must run in a test.\n")
	b.WriteString("- A suppression or waiver needs a reason (`-- <reason>`); TODOs need an issue link.\n")
	b.WriteString("- Never edit `.millennium/`: propose rule changes in `" + ProposalFile + "`; the user applies them.\n")
	b.WriteString("- `millennium-code-qc config explain <file>` shows which rules apply to a file.\n")
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
