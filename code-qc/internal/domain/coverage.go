package domain

import (
	"fmt"
	"maps"
	"slices"
)

// The tests gate trusts a coverage report for a file it judges only when
// the report describes that file: every line it gives exists in the file,
// and no other file's code is counted as the file's. Both are checked for
// those files only, so a report that is odd about any other file (generated
// code, an excluded file, a file outside the tested layers) blocks nothing.

// ForeignCoverage is code in one file that the coverage tool counts as
// another file's. Go's cover credits the code after a //line directive to
// the file name the directive gives, taken as a file of the same package
// directory, whatever directory the directive names.
type ForeignCoverage struct {
	// File holds the directive and the code after it, repository-relative.
	File      string
	Directive LineDirective
	// Credited is the repository-relative file the code counts for.
	Credited string
}

// ForeignCoverageFindings reports the code of other files that the coverage
// tool credits to file, a file the tests gate judges. Its coverage may come
// only from the file itself: code that ran elsewhere would pass for its
// untested lines. An excluded file's directives count like any other's:
// exclude takes a file out of the checks, not out of the coverage of the
// files beside it. The file's own directives are line-directive findings.
func ForeignCoverageFindings(file string, foreign []ForeignCoverage) []Finding {
	var findings []Finding
	for _, f := range foreign {
		if f.Credited != file || f.File == file {
			continue
		}
		findings = append(findings, Finding{Rule: "foreign-coverage", File: file, Message: fmt.Sprintf(
			"%s:%d holds %q, so the coverage tool counts the code after it as this file's, and untested lines here could pass as run; coverage of a file in a tested layer may come only from that file: remove the directive, or make it name no file of this directory",
			f.File, f.Directive.Line, f.Directive.Text)})
	}
	return findings
}

// Outside returns the first line the coverage gives that a file of
// lineCount lines, as LineCount counts them, does not have.
func (f FileCoverage) Outside(lineCount int) (int, bool) {
	for _, line := range slices.Sorted(maps.Keys(f)) {
		if line < 1 || line > lineCount {
			return line, true
		}
	}
	return 0, false
}

// CoverageBoundsFinding reports coverage of file that gives a line the file
// does not have. The report then describes other positions than the
// file's, and the lines it should have given go unreported, so it cannot be
// used: a tool failure.
func CoverageBoundsFinding(file string, coverage FileCoverage, lineCount int) (Finding, bool) {
	line, outside := coverage.Outside(lineCount)
	if !outside {
		return Finding{}, false
	}
	return Finding{Rule: "tool-failed", File: file, Message: fmt.Sprintf(
		"the coverage report gives line %d of this file, which has %d lines, so it does not describe the file as it is: a Go //line directive moves the positions the report gives, and a report built from another version of the file has other positions; remove the directive, or rebuild what the test command runs",
		line, lineCount)}, true
}
