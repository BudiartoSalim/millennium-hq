package domain

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// minReasonLetters is how much explanation a suppression or waiver needs:
// enough that "fix" or "temp" do not pass for a reason.
const minReasonLetters = 10

var (
	// suppression matches the directives that switch a checker off for a
	// line, a block or a file, across the supported languages.
	suppression = regexp.MustCompile(`eslint-disable(-next-line|-line)?|@ts-(ignore|expect-error|nocheck)|//\s*nolint|\bNOLINT\b|lint:ignore|#\s*nosec|//\s*#nosec|#\s*noqa|#\s*type:\s*ignore|#\s*pragma:\s*no\s*cover|shellcheck\s+disable=|(istanbul|c8|v8)\s+ignore|node:coverage\s+(disable|ignore\s+next)`)
	// coverageDirective matches every node:coverage comment, the ones that
	// switch coverage back on included. Like a suppression, it steers a tool,
	// so it never explains the suppression below it.
	coverageDirective = regexp.MustCompile(`node:coverage\b`)
	// nodeCoverage matches a comment that reads like one of Node's coverage
	// directives, including the one that switches coverage back on.
	nodeCoverage = regexp.MustCompile(`node:coverage\s+(enable|disable|ignore\s+next)`)
	// nodeCoverageExact is a coverage directive as Node's test runner reads
	// it: the runner looks for these exact comments anywhere on a line, so
	// it ignores one with other spacing, with text added inside, or written
	// with //.
	nodeCoverageExact = regexp.MustCompile(`/\* node:coverage (enable|disable|ignore next( \d+)?) \*/`)
	// todo matches a work-left-to-do marker word at the start of a comment,
	// or anywhere when a colon or an opening parenthesis follows it. The same
	// word inside prose, or hyphenated, is not a marker.
	todo = regexp.MustCompile(`^[\s/#*-]*(TODO|FIXME|XXX|HACK)\b[^-]|^[\s/#*-]*(TODO|FIXME|XXX|HACK)$|\b(TODO|FIXME|XXX|HACK)[:(]`)
	// issueLink matches what makes a TODO traceable: #123, PROJ-45, a URL.
	issueLink = regexp.MustCompile(`#\d+|\b[A-Z][A-Z0-9]+-\d+\b|https?://\S+`)
	// waiver matches millennium's own per-line exception:
	// "millennium:allow secret -- test fixture, not a real key".
	waiver = regexp.MustCompile(`millennium:allow\s+([a-z-]+)\s*--\s*(.*)`)
)

// SuppressionFindings reports suppression directives that carry no reason,
// and Node coverage directives that Node does not read. A reason is text
// after the directive, separated by " -- " or a further comment marker
// ("//nolint:gosec // reason"), or a comment on the line directly above
// that is not itself a suppression or coverage directive. Only comments on
// touched lines are checked.
func SuppressionFindings(file string, comments []Comment, touched LineSet) []Finding {
	byLine := commentsByLine(comments)
	var findings []Finding
	for _, c := range comments {
		if !touched.Contains(c.Line) {
			continue
		}
		if finding, ok := suppressionFinding(file, c, byLine); ok {
			findings = append(findings, finding)
		}
	}
	return findings
}

// suppressionFinding checks one comment. Node reads its coverage
// directives only as written, so text after one, a reason included, makes
// Node ignore it (ignored-directive); its reason goes on the line above.
func suppressionFinding(file string, c Comment, byLine map[int]Comment) (Finding, bool) {
	text := strings.TrimSpace(c.Text)
	node := nodeCoverage.MatchString(text)
	if node && !nodeCoverageExact.MatchString(text) {
		return Finding{Rule: "ignored-directive", File: file, Line: c.Line, Message: fmt.Sprintf(
			"Node does not read %q, so it switches nothing: Node reads only \"/* node:coverage disable */\", \"/* node:coverage enable */\" and \"/* node:coverage ignore next */\" (with an optional count), written exactly; write it so, and put the reason in a comment on the line above", text)}, true
	}
	loc := suppression.FindStringIndex(text)
	if loc == nil || hasReason(text[loc[1]:]) || explainedAbove(byLine, c.Line) {
		return Finding{}, false
	}
	advice := `add " -- <reason>" or a comment on the line above`
	if node {
		advice = "Node reads the directive only as written, so add the reason as a comment on the line above"
	}
	return Finding{Rule: "suppression-without-reason", File: file, Line: c.Line, Message: fmt.Sprintf(
		"%q switches a check off without saying why; %s", text, advice)}, true
}

// TodoFindings reports TODO-style markers without an issue reference on
// touched lines.
func TodoFindings(file string, comments []Comment, touched LineSet) []Finding {
	var findings []Finding
	for _, c := range comments {
		if !touched.Contains(c.Line) || !todo.MatchString(c.Text) || issueLink.MatchString(c.Text) {
			continue
		}
		findings = append(findings, Finding{Rule: "todo-without-link", File: file, Line: c.Line, Message: fmt.Sprintf(
			"%q has no follow-up; link an issue (#123, PROJ-45 or a URL) or do the work now", strings.TrimSpace(c.Text))})
	}
	return findings
}

// hasReason looks for an explanation after a directive: the text following
// " -- ", or following a second comment marker.
func hasReason(rest string) bool {
	for _, sep := range []string{" -- ", "--", "//", "#"} {
		if _, after, ok := strings.Cut(rest, sep); ok && letters(after) >= minReasonLetters {
			return true
		}
	}
	return false
}

func explainedAbove(byLine map[int]Comment, line int) bool {
	above, ok := byLine[line-1]
	return ok && !suppression.MatchString(above.Text) && !coverageDirective.MatchString(above.Text) && letters(above.Text) >= minReasonLetters
}

func commentsByLine(comments []Comment) map[int]Comment {
	byLine := make(map[int]Comment, len(comments))
	for _, c := range comments {
		byLine[c.Line] = c
	}
	return byLine
}

func letters(s string) int {
	count := 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			count++
		}
	}
	return count
}

// Waivers are millennium's own per-line exceptions, read from the raw text
// so they work in any file: a "millennium:allow <rule> -- <reason>" comment
// on the line itself or the line above waives that rule there.
type Waivers struct {
	byLine map[int][]string
}

// ReadWaivers collects the waivers of a file. A waiver without a reason of
// at least minReasonLetters letters waives nothing.
func ReadWaivers(content []byte) Waivers {
	w := Waivers{byLine: map[int][]string{}}
	for i, line := range strings.Split(string(content), "\n") {
		if m := waiver.FindStringSubmatch(line); m != nil && letters(m[2]) >= minReasonLetters {
			w.byLine[i+1] = append(w.byLine[i+1], m[1])
		}
	}
	return w
}

// Covers reports whether rule is waived at line.
func (w Waivers) Covers(rule string, line int) bool {
	for _, l := range []int{line, line - 1} {
		for _, waived := range w.byLine[l] {
			if waived == rule {
				return true
			}
		}
	}
	return false
}

// ConstructFindings reports risky constructs on touched lines, except where
// a waiver for "banned-construct" explains them.
func ConstructFindings(file string, constructs []Construct, touched LineSet, waivers Waivers) []Finding {
	var findings []Finding
	for _, c := range constructs {
		if !touched.Contains(c.Line) || waivers.Covers("banned-construct", c.Line) {
			continue
		}
		findings = append(findings, Finding{Rule: "banned-construct", File: file, Line: c.Line, Message: fmt.Sprintf(
			"%s (%s): %s; if it is truly needed, add \"millennium:allow banned-construct -- <reason>\" on the line above", c.Kind, c.Detail, constructAdvice(c.Kind))})
	}
	return findings
}

func constructAdvice(kind ConstructKind) string {
	switch kind {
	case ConstructDynamicCode:
		return "code built at run time can run whatever reaches it"
	case ConstructShellCommand:
		return "a shell turns interpolated values into commands; run the program directly with an argument list"
	case ConstructTLSVerifyOff:
		return "without certificate checks anyone on the network can impersonate the server"
	case ConstructUnsafeDeserialization:
		return "decoding untrusted data this way can run code; use a data-only format or a safe loader"
	case ConstructHTMLInjection:
		return "raw HTML from data is a cross-site scripting hole; render text or sanitise first"
	case ConstructExplicitAny:
		return "`any` switches type checking off; use `unknown` and narrow it"
	}
	return "risky construct"
}
