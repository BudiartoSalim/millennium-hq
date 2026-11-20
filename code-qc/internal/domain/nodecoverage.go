package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// Node's test runner reads its coverage directives from the raw lines of the
// code it runs, not from its comments: it matches every line against
// /\/\* node:coverage (enable|disable) \*\// and
// /\/\* node:coverage ignore next (\d+ )?\*\//. A directive inside a string
// or a template literal switches coverage like a written one, and the lines
// it hides vanish from the report, so this check reads raw lines too.

var (
	// nodeCoverage matches text that reads like one of Node's coverage
	// directives, the one that switches coverage back on included.
	nodeCoverage = regexp.MustCompile(`node:coverage\s+(enable|disable|ignore\s+next)`)
	// nodeCoverageExact is a coverage directive as Node reads it, anywhere
	// on a line: other spacing, text added inside, or the // form make Node
	// ignore it.
	nodeCoverageExact = regexp.MustCompile(`/\* node:coverage (enable|disable|ignore next( \d+)?) \*/`)
)

// NodeCoverageFindings checks the touched lines of a file of lang for
// Node's coverage directives: one that switches coverage off without a
// reason (suppression-without-reason), and text that reads like a
// directive but that Node does not read (ignored-directive). Of the
// languages the engine checks, only TypeScript runs on Node. Every line
// counts, as Node reads it, so a directive in a string is reported like a
// written one. Text after a directive makes Node ignore it, so its reason
// is a comment on the line directly above, on a line that does not mention
// node:coverage itself.
func NodeCoverageFindings(file string, lang Language, content []byte, comments []Comment, touched LineSet) []Finding {
	if lang != LanguageTypeScript {
		return nil
	}
	lines := strings.Split(string(content), "\n")
	byLine := commentsByLine(comments)
	var findings []Finding
	for i, line := range lines {
		if number := i + 1; touched.Contains(number) && coverageDirective.MatchString(line) {
			findings = append(findings, nodeLineFindings(file, number, lines, byLine)...)
		}
	}
	return findings
}

// nodeLineFindings checks line number of lines. One line can hold both a
// directive Node reads and one it ignores.
func nodeLineFindings(file string, number int, lines []string, byLine map[int]Comment) []Finding {
	line := lines[number-1]
	var findings []Finding
	if near := nodeCoverage.FindString(nodeCoverageExact.ReplaceAllString(line, "")); near != "" {
		findings = append(findings, Finding{Rule: "ignored-directive", File: file, Line: number, Message: fmt.Sprintf(
			"Node does not read %q as a directive, so it switches nothing: Node reads only \"/* node:coverage disable */\", \"/* node:coverage enable */\" and \"/* node:coverage ignore next */\" (with an optional count), written exactly; write it so, and put the reason in a comment on the line above", strings.TrimSpace(line))})
	}
	if directive := switchesCoverageOff(line); directive != "" && !nodeExplainedAbove(lines, byLine, number) {
		findings = append(findings, Finding{Rule: "suppression-without-reason", File: file, Line: number, Message: fmt.Sprintf(
			"%q switches Node's coverage off without saying why; Node reads the directive anywhere on a line, in a string too, and only as written, so add the reason as a comment on the line above", directive)})
	}
	return findings
}

// switchesCoverageOff returns the first directive on line that Node reads
// and that switches coverage off, or "".
func switchesCoverageOff(line string) string {
	for _, m := range nodeCoverageExact.FindAllStringSubmatch(line, -1) {
		if m[1] != "enable" {
			return m[0]
		}
	}
	return ""
}

// nodeExplainedAbove reports whether a comment on the line above line
// number explains it. A line that mentions node:coverage anywhere, in a
// string too, steers Node itself and explains nothing.
func nodeExplainedAbove(lines []string, byLine map[int]Comment, number int) bool {
	return number > 1 && !coverageDirective.MatchString(lines[number-2]) && explainedAbove(byLine, number)
}
