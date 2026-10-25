package gate

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"millennium/internal/application/port"
	"millennium/internal/domain"
)

// Deps guards the supply chain of a change. In every tier: each package a
// changed lockfile adds or changes, transitive ones included, needs an
// approval record; new npm packages may not run install scripts; manifests
// must pin (exact versions, hashes, action SHAs, image digests) and npm
// projects must keep install scripts off. In the full tier it also asks the
// registries: new releases must be older than the cooldown, no locked package
// may have a known vulnerability, and the package managers' own integrity
// checks must be on.
type Deps struct {
	source    port.SourceReader
	manifests port.ManifestReader
	approvals port.ApprovalSource
	registry  port.Registry
	integrity port.IntegrityTools
	now       func() time.Time
}

// NewDeps returns the supply-chain gate.
func NewDeps(source port.SourceReader, manifests port.ManifestReader, approvals port.ApprovalSource,
	registry port.Registry, integrity port.IntegrityTools, now func() time.Time) *Deps {
	return &Deps{source: source, manifests: manifests, approvals: approvals, registry: registry, integrity: integrity, now: now}
}

// Rules lists the rules the gate reports.
func (*Deps) Rules() []port.Rule {
	return []port.Rule{
		{Name: "unapproved-dependency", Disableable: true},
		{Name: "install-script", Disableable: true},
		{Name: "unpinned", Disableable: true},
		{Name: "unhashed", Disableable: true},
		{Name: "unpinned-action", Disableable: true},
		{Name: "unpinned-image", Disableable: true},
		{Name: "npmrc-unsafe", Disableable: true},
		{Name: "too-new", Disableable: true},
		{Name: "known-vulnerability", Disableable: true},
		{Name: "integrity-weakened", Disableable: true},
		{Name: "signatures-failed", Disableable: true},
		{Name: "manifest-unreadable", Disableable: false},
		{Name: "invalid-approval", Disableable: false},
		{Name: "registry-failed", Disableable: false},
	}
}

// Name identifies the gate in findings.
func (*Deps) Name() string { return "deps" }

// depsScan is what the changed manifests say.
type depsScan struct {
	added     []domain.LockedPackage
	locked    []domain.Package
	npmDirs   map[string]bool
	goModules map[string]bool
	findings  []domain.Finding
}

// Run checks the manifests of the change.
func (g *Deps) Run(ctx context.Context, in port.GateInput) ([]domain.Finding, error) {
	scan, err := g.scan(ctx, in)
	if err != nil {
		return nil, err
	}
	findings := append(scan.findings, g.approvalFindings(scan.added)...)
	findings = append(findings, g.npmrcFindings(scan.npmDirs)...)
	if in.Tier == domain.TierFull {
		findings = append(findings, g.registryFindings(ctx, scan)...)
		findings = append(findings, g.integrityFindings(ctx, scan)...)
	}
	return label(g.Name(), findings), nil
}

func (g *Deps) scan(ctx context.Context, in port.GateInput) (depsScan, error) {
	scan := depsScan{npmDirs: map[string]bool{}, goModules: map[string]bool{}}
	for _, file := range in.Changes.Files {
		if file.Kind == domain.ChangeDeleted {
			continue
		}
		if err := g.scanFile(ctx, in.Changes.Base, file, &scan); err != nil {
			return depsScan{}, err
		}
	}
	return scan, nil
}

func (g *Deps) scanFile(ctx context.Context, base string, file domain.ChangedFile, scan *depsScan) error {
	content, err := g.source.ReadWorking(file.Path)
	if err != nil {
		return nil // only manifests matter here; other gates report unreadable files
	}
	facts, ok, err := g.manifests.Read(file.Path, content)
	if !ok {
		return nil
	}
	if err != nil {
		scan.findings = append(scan.findings, domain.Finding{Rule: "manifest-unreadable", File: file.Path, Message: err.Error()})
		return nil
	}
	touched, err := touchedLines(ctx, g.source, base, file)
	if err != nil {
		return err
	}
	for _, issue := range facts.Issues {
		if touched.Contains(issue.Line) {
			scan.findings = append(scan.findings, domain.Finding{Rule: issue.Code, File: file.Path, Line: issue.Line, Message: issue.Message})
		}
	}
	g.noteProject(file.Path, scan)
	if !facts.Lockfile {
		return nil
	}
	before, err := g.baseLocked(ctx, base, file)
	if err != nil {
		return err
	}
	for _, p := range facts.Packages {
		scan.locked = append(scan.locked, p.Package)
		if !before[p.Package] {
			scan.added = append(scan.added, p)
		}
	}
	return nil
}

func (g *Deps) noteProject(p string, scan *depsScan) {
	dir := path.Dir(p)
	if dir == "." {
		dir = ""
	}
	switch path.Base(p) {
	case "package-lock.json", "package.json":
		scan.npmDirs[dir] = true
	case "go.sum":
		scan.goModules[dir] = true
	}
}

// baseLocked is what the lockfile pinned before the change: nothing for a
// new file or an audit, so every package counts as added.
func (g *Deps) baseLocked(ctx context.Context, base string, file domain.ChangedFile) (map[domain.Package]bool, error) {
	before := map[domain.Package]bool{}
	if base == "" || file.Kind != domain.ChangeModified {
		return before, nil
	}
	content, found, err := g.source.ReadAt(ctx, base, file.Path)
	if err != nil || !found {
		return before, err
	}
	facts, _, err := g.manifests.Read(file.Path, content)
	if err != nil {
		// An unreadable old lockfile: hold every package to the rules.
		return before, nil
	}
	for _, p := range facts.Packages {
		before[p.Package] = true
	}
	return before, nil
}

func (g *Deps) approvalFindings(added []domain.LockedPackage) []domain.Finding {
	if len(added) == 0 {
		return nil
	}
	approvals, err := g.approvals.Approvals()
	if err != nil {
		return []domain.Finding{{Rule: "invalid-approval", Message: err.Error()}}
	}
	var findings []domain.Finding
	for _, p := range added {
		if _, ok := approvals.Covering(p.Package); !ok {
			findings = append(findings, domain.Finding{Rule: "unapproved-dependency", Message: fmt.Sprintf(
				"%s has no approval record; vet it (the vet-dependency skill drafts one) and have the user apply the record to %s", p, domain.ApprovalsDir)})
		}
		if p.InstallScript {
			findings = append(findings, domain.Finding{Rule: "install-script", Message: fmt.Sprintf(
				"%s runs an install script; with ignore-scripts on it will not work, and with it off it can run anything", p)})
		}
	}
	return findings
}

func (g *Deps) npmrcFindings(dirs map[string]bool) []domain.Finding {
	var findings []domain.Finding
	for _, dir := range sortedKeys(dirs) {
		npmrc := path.Join(dir, ".npmrc")
		content, _ := g.source.ReadWorking(npmrc)
		if problems := domain.NpmrcProblems(string(content)); len(problems) > 0 {
			findings = append(findings, domain.Finding{Rule: "npmrc-unsafe", File: npmrc, Message: "the project's .npmrc must set " + strings.Join(problems, "; and ")})
		}
	}
	return findings
}

func (g *Deps) registryFindings(ctx context.Context, scan depsScan) []domain.Finding {
	var findings []domain.Finding
	approvals, _ := g.approvals.Approvals()
	for _, p := range scan.added {
		published, err := g.registry.PublishedAt(ctx, p.Package)
		if err != nil {
			findings = append(findings, domain.Finding{Rule: "registry-failed", Message: fmt.Sprintf("release date of %s: %v", p, err)})
			continue
		}
		approval, _ := approvals.Covering(p.Package)
		if age := g.now().Sub(published); age < domain.ReleaseCooldown && approval.SecurityPatch == "" {
			findings = append(findings, domain.Finding{Rule: "too-new", Message: fmt.Sprintf(
				"%s was released %s ago; wait until it is %s old, or record the advisory it fixes as securityPatch", p, age.Round(time.Hour), domain.ReleaseCooldown)})
		}
	}
	return append(findings, g.vulnerabilityFindings(ctx, scan.locked)...)
}

func (g *Deps) vulnerabilityFindings(ctx context.Context, locked []domain.Package) []domain.Finding {
	if len(locked) == 0 {
		return nil
	}
	found, err := g.registry.Vulnerabilities(ctx, locked)
	if err != nil {
		return []domain.Finding{{Rule: "registry-failed", Message: "vulnerability lookup (OSV): " + err.Error()}}
	}
	keys := make([]string, 0, len(found))
	for key := range found {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	findings := make([]domain.Finding, 0, len(keys))
	for _, key := range keys {
		findings = append(findings, domain.Finding{Rule: "known-vulnerability", Message: fmt.Sprintf(
			"%s has known vulnerabilities: %s; upgrade to a fixed version", key, strings.Join(found[key], ", "))})
	}
	return findings
}

func (g *Deps) integrityFindings(ctx context.Context, scan depsScan) []domain.Finding {
	var findings []domain.Finding
	for _, dir := range sortedKeys(scan.goModules) {
		env, err := g.integrity.GoEnv(ctx, dir)
		if err != nil {
			findings = append(findings, domain.Finding{Rule: "registry-failed", Message: err.Error()})
			continue
		}
		for _, problem := range domain.GoIntegrityProblems(env) {
			findings = append(findings, domain.Finding{Rule: "integrity-weakened", File: path.Join(dir, "go.sum"), Message: problem})
		}
	}
	for _, dir := range sortedKeys(scan.npmDirs) {
		if err := g.integrity.NpmAuditSignatures(ctx, dir); err != nil {
			findings = append(findings, domain.Finding{Rule: "signatures-failed", File: path.Join(dir, "package-lock.json"), Message: err.Error()})
		}
	}
	return findings
}
