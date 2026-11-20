package domain

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

// nodeSource is TypeScript source of lines, with the comments an analyzer
// would report for it: here, a line that starts with a comment marker, or
// the end of a line from " // " on.
func nodeSource(lines ...string) ([]byte, []Comment) {
	var comments []Comment
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		_, trailing, found := strings.Cut(trimmed, " // ")
		switch {
		case strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*"):
			comments = append(comments, Comment{Line: i + 1, Text: trimmed})
		case found:
			comments = append(comments, Comment{Line: i + 1, Text: "// " + trailing})
		}
	}
	return []byte(strings.Join(lines, "\n") + "\n"), comments
}

// Node's test runner reads its coverage directives only as these exact
// texts: "/* node:coverage disable */", "enable", "ignore next" and
// "ignore next <n>". Any other text, a reason included, makes it ignore the
// directive (checked with node v24), so a reason goes on the line above,
// and a directive Node would ignore is reported (SR-16, QT2-8). Node finds
// them on raw lines, strings and template literals included, so they are
// directives there too (R3-SEC-2).
func TestNodeCoverageFindings(t *testing.T) {
	const reason = "// the platform branch runs only on Windows"
	tests := []struct {
		name  string
		lines []string
		want  []string
	}{
		{"disable bare", []string{"/* node:coverage disable */"}, []string{"suppression-without-reason 1"}},
		{"ignore next bare", []string{"/* node:coverage ignore next */"}, []string{"suppression-without-reason 1"}},
		{"ignore next with a count, bare", []string{"/* node:coverage ignore next 3 */"}, []string{"suppression-without-reason 1"}},
		{"disable with a reason above", []string{reason, "/* node:coverage disable */"}, nil},
		{"ignore next with a count and a reason above", []string{reason, "/* node:coverage ignore next 3 */"}, nil},
		{"enable is no suppression", []string{"/* node:coverage enable */"}, nil},
		{"directive above is not a reason", []string{"/* node:coverage enable */", "/* node:coverage ignore next */"}, []string{"suppression-without-reason 2"}},
		{"a reason two lines up", []string{reason, "", "/* node:coverage disable */"}, []string{"suppression-without-reason 3"}},
		{"code above is not a reason", []string{"const windowsOnlyBranch = 1;", "/* node:coverage disable */"}, []string{"suppression-without-reason 2"}},
		{"reason after the directive", []string{"/* node:coverage ignore next -- the platform branch runs only on Windows */"}, []string{"ignored-directive 1"}},
		{"reason after the directive, reason above too", []string{reason, "/* node:coverage disable -- the platform branch runs only on Windows */"}, []string{"ignored-directive 2"}},
		{"enable with text", []string{"/* node:coverage enable -- back on */"}, []string{"ignored-directive 1"}},
		{"line comment form", []string{reason, "// node:coverage ignore next 3"}, []string{"ignored-directive 2"}},
		{"other spacing", []string{reason, "/*node:coverage disable*/"}, []string{"ignored-directive 2"}},
		{"prose that names the directives", []string{"// node:coverage is Node's own coverage"}, nil},
		{"exact form inside a line comment", []string{"// see /* node:coverage disable */"}, []string{"suppression-without-reason 1"}},
		{"exact form inside a line comment, reason above", []string{reason, "// see /* node:coverage disable */"}, nil},
		{"one directive Node reads, one it ignores", []string{reason, "/* node:coverage enable */ /*node:coverage disable*/"}, []string{"ignored-directive 2"}},
		{"both kinds on one line", []string{"/* node:coverage disable */ // node:coverage enable"}, []string{"ignored-directive 1", "suppression-without-reason 1"}},
		// QA's repros: the directive in a string or a template literal
		// switches coverage off for the rest of the file.
		{"disable in a string", []string{`export const tag = "/* node:coverage disable */";`}, []string{"suppression-without-reason 1"}},
		{"ignore next in a string", []string{`export const banner = "/* node:coverage ignore next 9 */";`}, []string{"suppression-without-reason 1"}},
		{"disable in a template literal", []string{"export const banner = `", "/* node:coverage disable */", "`;"}, []string{"suppression-without-reason 2"}},
		{"disable in a string, reason above", []string{reason, `export const tag = "/* node:coverage disable */";`}, nil},
		{"near miss in a string", []string{`const s = "node:coverage disable";`}, []string{"ignored-directive 1"}},
		{"enable in a string is no suppression", []string{`const s = "/* node:coverage enable */";`}, nil},
		// A line that holds a directive steers Node, whatever its comment.
		{"a directive in a string above is not a reason", []string{`const s = "/* node:coverage enable */"; ` + reason, "/* node:coverage disable */"}, []string{"suppression-without-reason 2"}},
		{"a CRLF line", []string{"/* node:coverage disable */\r"}, []string{"suppression-without-reason 1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, comments := nodeSource(tt.lines...)
			got := NodeCoverageFindings("a.ts", LanguageTypeScript, content, comments, AllLines())
			if rules := rulesAndLines(got); !slices.Equal(rules, tt.want) {
				t.Fatalf("findings = %+v, want %q", got, tt.want)
			}
			if i := slices.IndexFunc(got, recommendsMoreThanTheLineAbove); i >= 0 {
				t.Fatalf("finding %+v must name a.ts and recommend only a reason on the line above", got[i])
			}
		})
	}
}

// rulesAndLines renders findings as "rule line".
func rulesAndLines(findings []Finding) []string {
	var out []string
	for _, f := range findings {
		out = append(out, f.Rule+" "+strconv.Itoa(f.Line))
	}
	return out
}

func recommendsMoreThanTheLineAbove(f Finding) bool {
	return f.File != "a.ts" || strings.Contains(f.Message, "<reason>\"") || !strings.Contains(f.Message, "on the line above")
}

// Only touched lines are checked, and only TypeScript runs on Node: a Go
// or Python string that names a directive steers no coverage tool.
func TestNodeCoverageFindingsScope(t *testing.T) {
	content, comments := nodeSource("const a = 1;", `const tag = "/* node:coverage disable */";`, "/* node:coverage ignore next */")
	touched := LineSet{Ranges: []LineRange{{First: 3, Last: 3}}}
	if got := NodeCoverageFindings("a.ts", LanguageTypeScript, content, comments, touched); len(got) != 1 || got[0].Line != 3 {
		t.Fatalf("findings = %+v, want only the touched line 3", got)
	}
	for _, lang := range []Language{LanguageGo, LanguagePython, LanguageShell} {
		if got := NodeCoverageFindings("a", lang, content, comments, AllLines()); len(got) != 0 {
			t.Fatalf("%s: findings = %+v, want none", lang, got)
		}
	}
}

// Node's directives are not the comment check's business: on raw lines
// they are reported once, by NodeCoverageFindings. A directive is still no
// reason for the suppression below it.
func TestSuppressionFindingsLeaveNodeDirectives(t *testing.T) {
	if got := SuppressionFindings("a.ts", comments("/* node:coverage disable */", "// node:coverage ignore next"), AllLines()); len(got) != 0 {
		t.Fatalf("findings = %+v, want none", got)
	}
	if got := SuppressionFindings("a.ts", comments("/* node:coverage enable -- the generated part ends here */", "// @ts-ignore"), AllLines()); len(got) != 1 || got[0].Line != 2 {
		t.Fatalf("findings = %+v, want the @ts-ignore below the directive", got)
	}
}
