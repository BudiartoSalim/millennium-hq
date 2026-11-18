package domain

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
)

// Tier says how thorough a run is. The fast tier runs on every agent turn
// (hooks); the full tier also runs the test suites and measures coverage,
// and is what the tech lead runs before calling work done.
type Tier string

// Tiers.
const (
	TierFast Tier = "fast"
	TierFull Tier = "full"
)

// Tiers lists every tier.
func Tiers() []Tier { return []Tier{TierFast, TierFull} }

// TestRecipe is how a component runs its tests when the built-in runner for
// its language does not fit, or for a language without one (TypeScript): an
// argument list run from the component root without a shell, and the lcov
// coverage report it writes, relative to the component root.
type TestRecipe struct {
	Command []string
	LCOV    string
}

// FileCoverage maps each executable line of a file to how often it ran.
type FileCoverage map[int]int

// Coverage is the line coverage of a test run, by repository-relative file.
type Coverage map[string]FileCoverage

// Merge adds other into c, keeping the higher count per line.
func (c Coverage) Merge(other Coverage) {
	for file, lines := range other {
		if c[file] == nil {
			c[file] = FileCoverage{}
		}
		for line, hits := range lines {
			c[file][line] = max(c[file][line], hits)
		}
	}
}

// Uncovered returns the touched executable lines that never ran, as ranges
// for a readable message. Lines that are not executable are not counted, and
// neither are lines without runtime code: a coverage tool working through a
// source map can give a comment or an erased type declaration zero hits.
func (f FileCoverage) Uncovered(touched LineSet, runtime RuntimeCode) []LineRange {
	var lines []int
	for line, hits := range f {
		if hits == 0 && touched.Contains(line) && runtime.Holds(line) {
			lines = append(lines, line)
		}
	}
	slices.Sort(lines)
	var ranges []LineRange
	for _, line := range lines {
		if n := len(ranges); n > 0 && ranges[n-1].Last == line-1 {
			ranges[n-1].Last = line
			continue
		}
		ranges = append(ranges, LineRange{First: line, Last: line})
	}
	return ranges
}

// FormatRanges renders line ranges as "3, 7-9".
func FormatRanges(ranges []LineRange) string {
	parts := make([]string, len(ranges))
	for i, r := range ranges {
		parts[i] = fmt.Sprint(r.First)
		if r.Last != r.First {
			parts[i] += fmt.Sprintf("-%d", r.Last)
		}
	}
	return strings.Join(parts, ", ")
}

var tsTestFile = regexp.MustCompile(`\.(test|spec)\.[cm]?tsx?$`)

// IsTestPath reports whether p is test code of lang, by name.
func IsTestPath(lang Language, p string) bool {
	name := path.Base(p)
	switch lang {
	case LanguageGo:
		return strings.HasSuffix(name, "_test.go")
	case LanguageTypeScript:
		return tsTestFile.MatchString(name) || strings.Contains("/"+p, "/__tests__/")
	case LanguagePython:
		return strings.HasPrefix(name, "test_") || strings.HasSuffix(name, "_test.py")
	}
	return false
}

// IsRelatedTest reports whether test is a test of production: in Go, any
// test file of the same package (directory); in TypeScript and Python, a
// test file named after the module (user.ts and user.test.ts, user.py and
// test_user.py).
func IsRelatedTest(lang Language, production, test string) bool {
	if !IsTestPath(lang, test) {
		return false
	}
	switch lang {
	case LanguageGo:
		return path.Dir(test) == path.Dir(production)
	case LanguageTypeScript:
		return moduleStem(production) == moduleStem(tsTestFile.ReplaceAllString(path.Base(test), ""))
	}
	// Python: IsTestPath has already rejected every other language.
	name := strings.TrimSuffix(path.Base(test), ".py")
	stem := strings.TrimSuffix(strings.TrimPrefix(name, "test_"), "_test")
	return stem == moduleStem(production)
}

func moduleStem(p string) string {
	name := path.Base(p)
	if i := strings.Index(name, "."); i >= 0 {
		return name[:i]
	}
	return name
}

// LineDirectiveFindings reports every line directive of a file in a tested
// layer, touched or not: a directive above unchanged lines still moves the
// coverage of the changed lines below it, to lines that do not exist or to
// lines that ran, so untested code would pass as tested. Only the config's
// exclude, which declares generated code, takes a file out of this check.
func LineDirectiveFindings(file string, constructs []Construct) []Finding {
	var findings []Finding
	for _, c := range constructs {
		if c.Kind != ConstructLineDirective {
			continue
		}
		findings = append(findings, Finding{Rule: "line-directive", File: file, Line: c.Line, Message: fmt.Sprintf(
			"%s in a tested layer: %s; remove it, or, if the file is generated, declare it with \"exclude\" in the config", c.Detail, constructAdvice(c.Kind))})
	}
	return findings
}

// NeedsTests reports whether analysed code of a tested layer is production
// logic: not test code, and with at least one function. Files that only
// declare types or interfaces need no tests of their own. A generated-code
// marker does not count: in a tested layer only the config's exclude
// declares generated code (see LayerTesting).
func NeedsTests(facts FileFacts) bool {
	return !facts.Test && len(facts.Functions) > 0
}
