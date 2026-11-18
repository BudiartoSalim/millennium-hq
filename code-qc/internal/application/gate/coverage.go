package gate

import (
	"context"
	"fmt"

	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/domain"
)

// suiteResults is what the suites of a change gave: their coverage merged,
// the code each counts as another file's, and the components whose suite
// gave no usable result (it failed, could not run, or has no recipe): that
// finding already blocks, and their coverage would only repeat it.
type suiteResults struct {
	coverage domain.Coverage
	foreign  []domain.ForeignCoverage
	unusable map[string]bool
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
		results.foreign = append(results.foreign, result.Foreign...)
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

func (g *Tests) uncovered(ctx context.Context, in port.GateInput, candidates []candidate, results suiteResults) ([]domain.Finding, error) {
	var findings []domain.Finding
	for _, c := range candidates {
		if results.unusable[c.match.Component.Name()] {
			continue
		}
		touched, err := touchedLines(ctx, g.source, in.Changes.Base, c.file)
		if err != nil {
			return nil, err
		}
		for _, finding := range uncoveredChange(c, results, touched) {
			finding.Message += c.note
			findings = append(findings, finding)
		}
	}
	return findings, nil
}

// uncoveredChange reports the touched runtime lines of a candidate that no
// test ran. A change that touched no runtime line has nothing that could
// run, even in a file no test loads; with unknown runtime code every
// touched line counts. The coverage of the candidate is used only when it
// describes the candidate: no other file's code counts as its, and every
// line it gives is in the file.
func uncoveredChange(c candidate, results suiteResults, touched domain.LineSet) []domain.Finding {
	if !c.runtime.HoldsAny(touched) {
		return nil
	}
	if foreign := domain.ForeignCoverageFindings(c.file.Path, results.foreign); len(foreign) > 0 {
		return foreign
	}
	lines, ok := results.coverage[c.file.Path]
	if !ok {
		return []domain.Finding{{Rule: "uncovered-change", File: c.file.Path, Message: "no test executed this file"}}
	}
	if finding, outside := domain.CoverageBoundsFinding(c.file.Path, lines, c.lines); outside {
		return []domain.Finding{finding}
	}
	ranges := lines.Uncovered(touched, c.runtime)
	if len(ranges) == 0 {
		return nil
	}
	return []domain.Finding{{Rule: "uncovered-change", File: c.file.Path, Line: ranges[0].First, Message: fmt.Sprintf(
		"changed lines %s never run in any test", domain.FormatRanges(ranges))}}
}
