package gate

import (
	"context"
	"fmt"
	"path"

	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/domain"
)

// suiteResults is what the suites of a change gave: their coverage merged,
// and the components whose suite gave no usable result (it failed, could
// not run, or has no recipe): that finding already blocks, and their
// coverage would only repeat it.
type suiteResults struct {
	coverage domain.Coverage
	unusable map[string]bool
}

// lentCoverage is what the Go sources of one directory lend the files
// beside them: the code cover credits to another file, or why that is
// unknown.
type lentCoverage struct {
	foreign []domain.ForeignCoverage
	err     error
}

// runSuites runs each touched module's or component's suite once.
func (g *Tests) runSuites(ctx context.Context, in port.GateInput) (suiteResults, []domain.Finding) {
	results := suiteResults{coverage: domain.Coverage{}, unusable: map[string]bool{}}
	var findings []domain.Finding
	for _, suite := range g.suites(in) {
		finding, result := g.runSuite(ctx, suite)
		if finding != nil {
			findings = append(findings, *finding)
			results.unusable[suite.component] = true
			continue
		}
		results.coverage.Merge(result.Coverage)
	}
	return results, findings
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

// uncovered reports the touched runtime lines of the candidates that no
// test ran. A change that touched no runtime line has nothing that could
// run, even in a file no test loads; with unknown runtime code every
// touched line counts.
func (g *Tests) uncovered(ctx context.Context, in port.GateInput, candidates []candidate, results suiteResults) ([]domain.Finding, error) {
	lent := map[string]lentCoverage{}
	var findings []domain.Finding
	for _, c := range candidates {
		if results.unusable[c.match.Component.Name()] {
			continue
		}
		touched, err := touchedLines(ctx, g.source, in.Changes.Base, c.file)
		if err != nil {
			return nil, err
		}
		if !c.runtime.HoldsAny(touched) {
			continue
		}
		for _, finding := range uncoveredChange(c, results.coverage, g.lentTo(c, lent), touched) {
			finding.Message += c.note
			findings = append(findings, finding)
		}
	}
	return findings, nil
}

// lentTo returns what the Go sources beside candidate c lend it, reading
// each directory once into memo. Whatever ran the suite, a Go file's
// coverage comes from cover, which credits code after a //line directive
// to the file it names; other languages' tools have no such thing.
func (g *Tests) lentTo(c candidate, memo map[string]lentCoverage) lentCoverage {
	if c.lang != domain.LanguageGo {
		return lentCoverage{}
	}
	dir := path.Dir(c.file.Path)
	if lent, read := memo[dir]; read {
		return lent
	}
	foreign, err := g.foreign.GoForeignCoverage(dir)
	memo[dir] = lentCoverage{foreign: foreign, err: err}
	return memo[dir]
}

// uncoveredChange reports the touched runtime lines of candidate c that no
// test ran. Its coverage is used only when it describes the candidate: no
// other file's code counts as its, and every line it gives is in the file.
// When what the files beside it lend is unknown, that blocks the candidate
// alone.
func uncoveredChange(c candidate, coverage domain.Coverage, lent lentCoverage, touched domain.LineSet) []domain.Finding {
	if lent.err != nil {
		return []domain.Finding{{Rule: "tool-failed", File: c.file.Path, Message: fmt.Sprintf(
			"whether a //line directive in a Go file beside this one lends it coverage of code that ran elsewhere cannot be checked: %v", lent.err)}}
	}
	if foreign := domain.ForeignCoverageFindings(c.file.Path, lent.foreign); len(foreign) > 0 {
		return foreign
	}
	lines, ok := coverage[c.file.Path]
	if !ok {
		return []domain.Finding{{Rule: "uncovered-change", File: c.file.Path, Message: "no test executed this file"}}
	}
	if finding, outside := domain.CoverageBoundsFinding(c.file.Path, c.lang, lines, c.lines); outside {
		return []domain.Finding{finding}
	}
	ranges := lines.Uncovered(touched, c.runtime)
	if len(ranges) == 0 {
		return nil
	}
	return []domain.Finding{{Rule: "uncovered-change", File: c.file.Path, Line: ranges[0].First, Message: fmt.Sprintf(
		"changed lines %s never run in any test", domain.FormatRanges(ranges))}}
}
