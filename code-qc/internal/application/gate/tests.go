package gate

import (
	"context"
	"fmt"
	"path"

	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/domain"
)

// Tests holds changed logic to its tests. In every tier, changed production
// code in a tested layer (one its convention pack marks as tested, such as
// domain and application under clean-architecture) must come with a change to its tests. In the full tier the suites of the
// touched modules also run, must pass, and must execute every changed
// executable line of that code. Only runtime code counts: a modified file
// whose runtime tokens match its base needs no test change, and a changed
// line without runtime code need not run.
type Tests struct {
	analyzers map[domain.Language]port.Analyzer
	source    port.SourceReader
	runner    port.TestRunner
	finder    port.FileFinder
}

// NewTests returns the tests gate.
func NewTests(analyzers map[domain.Language]port.Analyzer, source port.SourceReader, runner port.TestRunner, finder port.FileFinder) *Tests {
	return &Tests{analyzers: analyzers, source: source, runner: runner, finder: finder}
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

// candidate is changed code that needs tests. note ends each of its
// findings: why a generated-code marker in it did not exempt it, if it has
// one.
type candidate struct {
	file    domain.ChangedFile
	lang    domain.Language
	match   domain.ComponentMatch
	runtime domain.RuntimeCode
	note    string
}

// Run checks the tests of the change.
func (g *Tests) Run(ctx context.Context, in port.GateInput) ([]domain.Finding, error) {
	candidates := g.candidates(ctx, in)
	findings := g.missingTests(ctx, in, candidates)
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
		if !ok || domain.IsTestPath(lang, file.Path) || in.Config.Packs.TestingOf(lang, match, file.Path) != domain.LayerTested {
			continue
		}
		if facts, ok := g.logic(ctx, lang, file.Path); ok {
			out = append(out, candidate{file: file, lang: lang, match: match, runtime: facts.Runtime, note: domain.LayerTested.MarkerNote(facts)})
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

// logic analyses the file and reports whether it is production logic that
// needs tests. A file that cannot be analysed is reported by the structure
// gate, so it is not a candidate here.
func (g *Tests) logic(ctx context.Context, lang domain.Language, p string) (domain.FileFacts, bool) {
	analyzer, ok := g.analyzers[lang]
	if !ok {
		return domain.FileFacts{}, false
	}
	content, err := g.source.ReadWorking(p)
	if err != nil {
		return domain.FileFacts{}, false
	}
	facts, err := analyzer.Analyze(ctx, p, content)
	return facts, err == nil && domain.NeedsTests(facts)
}

func (g *Tests) missingTests(ctx context.Context, in port.GateInput, candidates []candidate) []domain.Finding {
	var findings []domain.Finding
	for _, c := range candidates {
		if hasRelatedTest(in.Changes.Files, c) || domain.SameRuntime(g.baseRuntime(ctx, in.Changes.Base, c), c.runtime) {
			continue
		}
		message := fmt.Sprintf("%s changed without a change to its tests; %s", c.file.Path, testHint(c))
		if in.Changes.Base == "" {
			message = fmt.Sprintf("%s has no tests; %s", c.file.Path, testHint(c))
		}
		findings = append(findings, domain.Finding{Rule: "tests-missing", File: c.file.Path, Message: message + c.note})
	}
	return findings
}

// baseRuntime returns the runtime code of a modified file as it was at base,
// for a change that only touched comments, whitespace or erased types. An
// added file and an audit have no base to compare with. A base that is not
// found, cannot be read or fails analysis gives unknown facts, which never
// compare equal: the file counts as changed and needs its test change.
func (g *Tests) baseRuntime(ctx context.Context, base string, c candidate) domain.RuntimeCode {
	if base == "" || c.file.Kind != domain.ChangeModified {
		return domain.RuntimeCode{}
	}
	content, found, err := g.source.ReadAt(ctx, base, c.file.Path)
	if err != nil || !found {
		return domain.RuntimeCode{}
	}
	facts, err := g.analyzers[c.lang].Analyze(ctx, c.file.Path, content)
	if err != nil {
		return domain.RuntimeCode{}
	}
	return facts.Runtime
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
			findings = append(findings, domain.Finding{Rule: "uncovered-change", File: c.file.Path, Message: "no test executed this file" + c.note})
			continue
		}
		touched, err := touchedLines(ctx, g.source, in.Changes.Base, c.file)
		if err != nil {
			return nil, err
		}
		if ranges := lines.Uncovered(touched, c.runtime); len(ranges) > 0 {
			findings = append(findings, domain.Finding{Rule: "uncovered-change", File: c.file.Path, Line: ranges[0].First, Message: fmt.Sprintf(
				"changed lines %s never run in any test%s", domain.FormatRanges(ranges), c.note)})
		}
	}
	return findings, nil
}
