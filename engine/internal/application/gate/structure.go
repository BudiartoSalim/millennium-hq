package gate

import (
	"context"
	"errors"
	"fmt"
	"path"

	"millennium/internal/application/port"
	"millennium/internal/domain"
)

// Structure checks the code itself: function limits for every analysed
// file, and the layer rules of each convention its component selects. With a
// base revision, existing code may not get worse and only new imports are
// checked; without one (an audit), every file is held to the full rules.
type Structure struct {
	analyzers map[domain.Language]port.Analyzer
	packs     port.PackSource
	source    port.SourceReader
	limits    domain.Limits
}

// NewStructure returns the structure gate.
func NewStructure(analyzers map[domain.Language]port.Analyzer, packs port.PackSource, source port.SourceReader) *Structure {
	return &Structure{analyzers: analyzers, packs: packs, source: source, limits: domain.DefaultLimits()}
}

// Name identifies the gate in findings.
func (*Structure) Name() string { return "structure" }

// Run checks every added or modified file of a language some component uses.
func (g *Structure) Run(ctx context.Context, in port.GateInput) ([]domain.Finding, error) {
	var findings []domain.Finding
	for _, file := range in.Changes.Files {
		if file.Kind == domain.ChangeDeleted {
			continue
		}
		findings = append(findings, g.checkFile(ctx, in, file)...)
	}
	return label(g.Name(), dedupe(findings)), nil
}

func (g *Structure) checkFile(ctx context.Context, in port.GateInput, file domain.ChangedFile) []domain.Finding {
	lang, ok := g.language(file.Path)
	if !ok {
		return nil
	}
	match, ok, err := in.Config.ComponentFor(file.Path)
	if err != nil {
		return []domain.Finding{{Rule: "component-ambiguous", File: file.Path, Message: err.Error()}}
	}
	if !ok || !match.Component.Has(lang) {
		return nil
	}
	analyzer, ok := g.analyzers[lang]
	if !ok {
		return []domain.Finding{{Rule: "analysis-unavailable", Message: fmt.Sprintf(
			"structure checks for %s are not available in this engine version, so %s code cannot pass yet", lang, lang)}}
	}
	facts, base, err := g.analyze(ctx, analyzer, in.Changes.Base, file)
	if err != nil {
		return []domain.Finding{{Rule: "analysis-failed", File: file.Path, Message: err.Error()}}
	}
	findings := g.limits.Check(facts, base)
	return append(findings, g.layerFindings(match, lang, facts, base)...)
}

// language detects a file's language by extension and, for an
// extensionless file such as bin/deploy, by its shebang.
func (g *Structure) language(p string) (domain.Language, bool) {
	if lang, ok := domain.LanguageOf(p); ok {
		return lang, true
	}
	if path.Ext(p) != "" {
		return "", false
	}
	content, err := g.source.ReadWorking(p)
	if err != nil {
		return "", false
	}
	return domain.DetectLanguage(p, content)
}

// analyze returns the facts of the file now and, when it existed at base,
// before the change. A base version that no longer parses is treated as
// absent, which holds the file to the full rules rather than none.
func (g *Structure) analyze(ctx context.Context, analyzer port.Analyzer, base string, file domain.ChangedFile) (domain.FileFacts, *domain.FileFacts, error) {
	content, err := g.source.ReadWorking(file.Path)
	if err != nil {
		return domain.FileFacts{}, nil, err
	}
	facts, err := analyzer.Analyze(ctx, file.Path, content)
	if err != nil {
		return domain.FileFacts{}, nil, err
	}
	if base == "" || file.Kind != domain.ChangeModified {
		return facts, nil, nil
	}
	old, found, err := g.source.ReadAt(ctx, base, file.Path)
	if err != nil {
		return domain.FileFacts{}, nil, err
	}
	if !found {
		return facts, nil, nil
	}
	before, err := analyzer.Analyze(ctx, file.Path, old)
	if err != nil {
		return facts, nil, nil
	}
	return facts, &before, nil
}

func (g *Structure) layerFindings(match domain.ComponentMatch, lang domain.Language, facts domain.FileFacts, base *domain.FileFacts) []domain.Finding {
	var findings []domain.Finding
	for _, convention := range match.Component.Conventions {
		layering, err := layeringFor(g.packs, convention, lang, match)
		if errors.Is(err, domain.ErrLanguageNotGoverned) {
			continue
		}
		if err != nil {
			findings = append(findings, domain.Finding{Rule: "convention-unavailable", Message: fmt.Sprintf(
				"component %s: %v; the change is blocked until the convention can be checked", match.Component.Name(), err)})
			continue
		}
		findings = append(findings, layering.CheckImports(facts, base)...)
	}
	return findings
}

// layeringFor applies a convention's pack to a component's files of lang,
// with errors worded for findings.
func layeringFor(packs port.PackSource, convention domain.Convention, lang domain.Language, match domain.ComponentMatch) (domain.Layering, error) {
	pack, err := packs.Pack(convention)
	if errors.Is(err, port.ErrPackUnavailable) {
		return domain.Layering{}, fmt.Errorf("convention %s is not available in this engine version", convention)
	}
	if err != nil {
		return domain.Layering{}, err
	}
	layering, err := pack.For(lang, match)
	if errors.Is(err, domain.ErrLanguageNotGoverned) {
		return domain.Layering{}, err
	}
	if errors.Is(err, domain.ErrLanguageNotSupported) {
		return domain.Layering{}, fmt.Errorf("convention %s has no %s rules in this engine version", convention, lang)
	}
	return layering, err
}

func label(gate string, findings []domain.Finding) []domain.Finding {
	for i := range findings {
		findings[i].Gate = gate
	}
	return findings
}

// dedupe drops repeated findings, such as one unavailable convention
// reported for every file of its component.
func dedupe(findings []domain.Finding) []domain.Finding {
	seen := make(map[domain.Finding]bool, len(findings))
	out := findings[:0]
	for _, f := range findings {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}
