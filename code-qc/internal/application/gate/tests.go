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
	foreign   port.ForeignCoverageSource
}

// NewTests returns the tests gate.
func NewTests(analyzers map[domain.Language]port.Analyzer, source port.SourceReader, runner port.TestRunner, finder port.FileFinder, foreign port.ForeignCoverageSource) *Tests {
	return &Tests{analyzers: analyzers, source: source, runner: runner, finder: finder, foreign: foreign}
}

// Rules lists the rules the gate reports.
func (*Tests) Rules() []port.Rule {
	return []port.Rule{
		{Name: "tests-missing", Disableable: true},
		{Name: "tests-failed", Disableable: true},
		{Name: "uncovered-change", Disableable: true},
		{Name: "line-directive", Disableable: true},
		{Name: "foreign-coverage", Disableable: true},
		{Name: "tests-unconfigured", Disableable: false},
		{Name: "tool-failed", Disableable: false},
	}
}

// Name identifies the gate in findings.
func (*Tests) Name() string { return "tests" }

// candidate is changed code that needs tests. lines is how many lines its
// content has, as git counts them. note ends each of its findings: why a
// generated-code marker in it did not exempt it, if it has one.
type candidate struct {
	file    domain.ChangedFile
	lang    domain.Language
	match   domain.ComponentMatch
	runtime domain.RuntimeCode
	lines   int
	note    string
}

// Run checks the tests of the change.
func (g *Tests) Run(ctx context.Context, in port.GateInput) ([]domain.Finding, error) {
	candidates, directives := g.testedFiles(ctx, in)
	findings := append(g.missingTests(ctx, in, candidates), directives...)
	if in.Tier != domain.TierFull {
		return label(g.Name(), findings), nil
	}
	results, runFindings := g.runSuites(ctx, in)
	findings = append(findings, runFindings...)
	uncovered, err := g.uncovered(ctx, in, candidates, results)
	if err != nil {
		return nil, err
	}
	return label(g.Name(), append(findings, uncovered...)), nil
}

// testedFiles analyses each changed file in a tested layer once. It returns
// the candidates, production logic that needs tests, and the line-directive
// findings of every such file, test files included, since a directive moves
// coverage wherever it is. A file that cannot be read or analysed is
// reported by the structure gate, so it gives neither here.
func (g *Tests) testedFiles(ctx context.Context, in port.GateInput) ([]candidate, []domain.Finding) {
	var candidates []candidate
	var findings []domain.Finding
	for _, file := range in.Changes.Files {
		c, facts, ok := g.testedFile(ctx, in.Config, file)
		if !ok {
			continue
		}
		findings = append(findings, domain.LineDirectiveFindings(file.Path, facts.LineDirectives)...)
		if !domain.IsTestPath(c.lang, file.Path) && domain.NeedsTests(facts) {
			candidates = append(candidates, c)
		}
	}
	return candidates, findings
}

// testedFile analyses a changed file in a tested layer: production code
// with its runtime code, which only this gate needs and asks for, and test
// code without.
func (g *Tests) testedFile(ctx context.Context, cfg domain.Config, file domain.ChangedFile) (candidate, domain.FileFacts, bool) {
	lang, match, ok := ownedCode(cfg, file)
	analyzer, known := g.analyzers[lang]
	if !ok || !known || cfg.Packs.TestingOf(lang, match, file.Path) != domain.LayerTested {
		return candidate{}, domain.FileFacts{}, false
	}
	content, err := g.source.ReadWorking(file.Path)
	if err != nil {
		return candidate{}, domain.FileFacts{}, false
	}
	var facts domain.FileFacts
	if domain.IsTestPath(lang, file.Path) {
		facts, err = analyzer.Analyze(ctx, file.Path, content)
	} else {
		facts, err = port.AnalyzeRuntime(ctx, analyzer, file.Path, content)
	}
	if err != nil {
		return candidate{}, domain.FileFacts{}, false
	}
	c := candidate{file: file, lang: lang, match: match, runtime: facts.Runtime, lines: domain.LineCount(content), note: domain.LayerTested.MarkerNote(facts)}
	return c, facts, true
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
// compare equal: the file counts as changed and needs its test change. So
// does a file whose runtime code is unknown now, without reading the base.
func (g *Tests) baseRuntime(ctx context.Context, base string, c candidate) domain.RuntimeCode {
	if base == "" || c.file.Kind != domain.ChangeModified || !c.runtime.Known() {
		return domain.RuntimeCode{}
	}
	content, found, err := g.source.ReadAt(ctx, base, c.file.Path)
	if err != nil || !found {
		return domain.RuntimeCode{}
	}
	facts, err := port.AnalyzeRuntime(ctx, g.analyzers[c.lang], c.file.Path, content)
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
