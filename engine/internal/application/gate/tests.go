package gate

import (
	"context"
	"fmt"
	"path"

	"millennium/internal/application/port"
	"millennium/internal/domain"
)

// Tests holds changed logic to its tests. In every tier, changed production
// code in a tested layer (domain and application, under clean-architecture)
// must come with a change to its tests. In the full tier the suites of the
// touched modules also run, must pass, and must execute every changed
// executable line of that code.
type Tests struct {
	analyzers map[domain.Language]port.Analyzer
	packs     port.PackSource
	source    port.SourceReader
	runner    port.TestRunner
	finder    port.FileFinder
}

// NewTests returns the tests gate.
func NewTests(analyzers map[domain.Language]port.Analyzer, packs port.PackSource, source port.SourceReader, runner port.TestRunner, finder port.FileFinder) *Tests {
	return &Tests{analyzers: analyzers, packs: packs, source: source, runner: runner, finder: finder}
}

// Rules lists the rules the gate reports.
func (*Tests) Rules() []port.Rule {
	return []port.Rule{
		{Name: "tests-missing", Disableable: true},
		{Name: "tests-failed", Disableable: true},
		{Name: "uncovered-change", Disableable: true},
		{Name: "tests-unconfigured", Disableable: false},
		{Name: "tool-failed", Disableable: false},
	}
}

// Name identifies the gate in findings.
func (*Tests) Name() string { return "tests" }

// candidate is changed code that needs tests.
type candidate struct {
	file  domain.ChangedFile
	lang  domain.Language
	match domain.ComponentMatch
}

// Run checks the tests of the change.
func (g *Tests) Run(ctx context.Context, in port.GateInput) ([]domain.Finding, error) {
	candidates := g.candidates(ctx, in)
	findings := missingTests(in, candidates)
	if in.Tier != domain.TierFull {
		return label(g.Name(), findings), nil
	}
	coverage, unusable, runFindings := g.runSuites(ctx, in)
	findings = append(findings, runFindings...)
	uncovered, err := g.uncovered(ctx, in, candidates, coverage, unusable)
	if err != nil {
		return nil, err
	}
	return label(g.Name(), append(findings, uncovered...)), nil
}

func (g *Tests) candidates(ctx context.Context, in port.GateInput) []candidate {
	var out []candidate
	for _, file := range in.Changes.Files {
		lang, match, ok := ownedCode(in.Config, file)
		if !ok || domain.IsTestPath(lang, file.Path) || !g.inTestedLayer(lang, match, file.Path) {
			continue
		}
		if g.needsTests(ctx, lang, file.Path) {
			out = append(out, candidate{file: file, lang: lang, match: match})
		}
	}
	return out
}

// ownedCode returns the language and component of a changed source file
// that a component checks.
func ownedCode(cfg domain.Config, file domain.ChangedFile) (domain.Language, domain.ComponentMatch, bool) {
	lang, ok := domain.LanguageOf(file.Path)
	if file.Kind == domain.ChangeDeleted || !ok || lang == domain.LanguageShell {
		return "", domain.ComponentMatch{}, false
	}
	match, owned, err := cfg.ComponentFor(file.Path)
	return lang, match, err == nil && owned && match.Component.Has(lang)
}

func (g *Tests) inTestedLayer(lang domain.Language, match domain.ComponentMatch, p string) bool {
	for _, convention := range match.Component.Conventions {
		if layering, err := layeringFor(g.packs, convention, lang, match); err == nil && layering.Tested(p) {
			return true
		}
	}
	return false
}

// needsTests analyses the file; one that cannot be analysed is reported by
// the structure gate, so it is not a candidate here.
func (g *Tests) needsTests(ctx context.Context, lang domain.Language, p string) bool {
	analyzer, ok := g.analyzers[lang]
	if !ok {
		return false
	}
	content, err := g.source.ReadWorking(p)
	if err != nil {
		return false
	}
	facts, err := analyzer.Analyze(ctx, p, content)
	return err == nil && domain.NeedsTests(facts)
}

func missingTests(in port.GateInput, candidates []candidate) []domain.Finding {
	var findings []domain.Finding
	for _, c := range candidates {
		if hasRelatedTest(in.Changes.Files, c) {
			continue
		}
		message := fmt.Sprintf("%s changed without a change to its tests; %s", c.file.Path, testHint(c))
		if in.Changes.Base == "" {
			message = fmt.Sprintf("%s has no tests; %s", c.file.Path, testHint(c))
		}
		findings = append(findings, domain.Finding{Rule: "tests-missing", File: c.file.Path, Message: message})
	}
	return findings
}

func hasRelatedTest(files []domain.ChangedFile, c candidate) bool {
	for _, f := range files {
		if f.Kind != domain.ChangeDeleted && domain.Within(f.Path, c.match.Root) && domain.IsRelatedTest(c.lang, c.file.Path, f.Path) {
			return true
		}
	}
	return false
}

func testHint(c candidate) string {
	base := path.Base(c.file.Path)
	switch c.lang {
	case domain.LanguageGo:
		return "add or update a _test.go file in " + path.Dir(c.file.Path)
	case domain.LanguageTypeScript:
		return "add or update " + moduleName(base) + ".test.ts"
	}
	return "add or update test_" + base
}

func moduleName(base string) string { return base[:len(base)-len(path.Ext(base))] }

// runSuites runs each touched module's or component's suite once and
// merges the coverage. It also names the components whose suite gave no
// usable result (it failed, could not run, or has no recipe): that finding
// already blocks, and their coverage would only repeat it.
func (g *Tests) runSuites(ctx context.Context, in port.GateInput) (domain.Coverage, map[string]bool, []domain.Finding) {
	coverage := domain.Coverage{}
	unusable := map[string]bool{}
	var findings []domain.Finding
	for _, suite := range g.suites(in) {
		finding, result := g.runSuite(ctx, suite)
		if finding != nil {
			findings = append(findings, *finding)
			unusable[suite.component] = true
			continue
		}
		coverage.Merge(result.Coverage)
	}
	return coverage, unusable, findings
}

func (g *Tests) runSuite(ctx context.Context, suite suite) (*domain.Finding, port.TestResult) {
	if suite.unconfigured {
		return &domain.Finding{Rule: "tests-unconfigured", Message: fmt.Sprintf(
			"component %s has %s code but no test recipe; add \"test\": {\"command\": [...], \"lcov\": \"...\"} to it", suite.component, suite.lang)}, port.TestResult{}
	}
	result, err := suite.run(ctx, g.runner)
	if err != nil {
		return &domain.Finding{Rule: "tool-failed", Message: fmt.Sprintf("tests of %s: %v", suite.label(), err)}, port.TestResult{}
	}
	if result.Failed {
		return &domain.Finding{Rule: "tests-failed", Message: fmt.Sprintf("tests of %s failed: %s", suite.label(), result.Summary)}, port.TestResult{}
	}
	return nil, result
}

func (g *Tests) uncovered(ctx context.Context, in port.GateInput, candidates []candidate, coverage domain.Coverage, unusable map[string]bool) ([]domain.Finding, error) {
	var findings []domain.Finding
	for _, c := range candidates {
		if unusable[c.match.Component.Name()] {
			continue
		}
		lines, ok := coverage[c.file.Path]
		if !ok {
			findings = append(findings, domain.Finding{Rule: "uncovered-change", File: c.file.Path, Message: "no test executed this file"})
			continue
		}
		touched, err := touchedLines(ctx, g.source, in.Changes.Base, c.file)
		if err != nil {
			return nil, err
		}
		if ranges := lines.Uncovered(touched); len(ranges) > 0 {
			findings = append(findings, domain.Finding{Rule: "uncovered-change", File: c.file.Path, Line: ranges[0].First, Message: fmt.Sprintf(
				"changed lines %s never run in any test", domain.FormatRanges(ranges))})
		}
	}
	return findings, nil
}
