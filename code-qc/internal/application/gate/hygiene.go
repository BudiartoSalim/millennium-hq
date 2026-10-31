package gate

import (
	"context"

	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/domain"
)

// Hygiene checks what any reviewer would flag at a glance, on the lines a
// change touched: credentials in any text file, and, in analysed code, risky
// constructs, suppressions without a reason, and TODOs without a follow-up.
type Hygiene struct {
	analyzers map[domain.Language]port.Analyzer
	source    port.SourceReader
}

// NewHygiene returns the hygiene gate.
func NewHygiene(analyzers map[domain.Language]port.Analyzer, source port.SourceReader) *Hygiene {
	return &Hygiene{analyzers: analyzers, source: source}
}

// Rules lists the rules the gate reports.
func (*Hygiene) Rules() []port.Rule {
	return []port.Rule{
		{Name: "secret", Disableable: true},
		{Name: "banned-construct", Disableable: true},
		{Name: "suppression-without-reason", Disableable: true},
		{Name: "todo-without-link", Disableable: true},
		{Name: "scan-failed", Disableable: false},
	}
}

// Name identifies the gate in findings.
func (*Hygiene) Name() string { return "hygiene" }

// Run scans every added or modified file.
func (g *Hygiene) Run(ctx context.Context, in port.GateInput) ([]domain.Finding, error) {
	var findings []domain.Finding
	for _, file := range in.Changes.Files {
		if file.Kind == domain.ChangeDeleted {
			continue
		}
		fileFindings, err := g.checkFile(ctx, in, file)
		if err != nil {
			return nil, err
		}
		findings = append(findings, fileFindings...)
	}
	return label(g.Name(), findings), nil
}

func (g *Hygiene) checkFile(ctx context.Context, in port.GateInput, file domain.ChangedFile) ([]domain.Finding, error) {
	content, err := g.source.ReadWorking(file.Path)
	if err != nil {
		// A file the scan cannot read could hold anything.
		return []domain.Finding{{Rule: "scan-failed", File: file.Path, Message: err.Error()}}, nil
	}
	if domain.IsBinary(content) {
		return nil, nil
	}
	touched, err := touchedLines(ctx, g.source, in.Changes.Base, file)
	if err != nil {
		return nil, err
	}
	waivers := domain.ReadWaivers(content)
	findings := domain.SecretFindings(file.Path, content, touched, waivers)
	facts, ok, err := g.facts(ctx, in.Config, file.Path, content)
	if err != nil {
		return append(findings, domain.Finding{Rule: "scan-failed", File: file.Path, Message: err.Error()}), nil
	}
	if !ok {
		return findings, nil
	}
	findings = append(findings, domain.ConstructFindings(file.Path, facts.Constructs, touched, waivers)...)
	findings = append(findings, domain.SuppressionFindings(file.Path, facts.Comments, touched)...)
	return append(findings, domain.TodoFindings(file.Path, facts.Comments, touched)...), nil
}

// facts analyses the file when a component that owns it lists its language;
// other files get only the secret scan.
func (g *Hygiene) facts(ctx context.Context, cfg domain.Config, p string, content []byte) (domain.FileFacts, bool, error) {
	lang, ok := domain.DetectLanguage(p, content)
	if !ok {
		return domain.FileFacts{}, false, nil
	}
	match, owned, err := cfg.ComponentFor(p)
	if err != nil || !owned || !match.Component.Has(lang) {
		// An ambiguous component is reported by the structure gate.
		return domain.FileFacts{}, false, nil
	}
	analyzer, ok := g.analyzers[lang]
	if !ok {
		return domain.FileFacts{}, false, nil
	}
	facts, err := analyzer.Analyze(ctx, p, content)
	return facts, err == nil, err
}

// touchedLines is every line of an added file or of an audit, and the
// changed lines of a modified file.
func touchedLines(ctx context.Context, source port.SourceReader, base string, file domain.ChangedFile) (domain.LineSet, error) {
	if base == "" || file.Kind != domain.ChangeModified {
		return domain.AllLines(), nil
	}
	return source.ChangedLines(ctx, base, file.Path)
}
