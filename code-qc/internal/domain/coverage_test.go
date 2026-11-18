package domain

import (
	"strings"
	"testing"
)

// Coverage of a file may come only from that file: a //line directive in a
// file beside it, excluded or not, that names it lends it the coverage of
// code that ran elsewhere (R2-SEC-1).
func TestForeignCoverageFindings(t *testing.T) {
	const real = "svc/internal/domain/real.go"
	lend := func(file, credited string) ForeignCoverage {
		return ForeignCoverage{File: file, Directive: LineDirective{Line: 3, Text: "//line real.go:5", File: "real.go"}, Credited: credited}
	}
	tests := []struct {
		name    string
		foreign []ForeignCoverage
		want    []string
	}{
		{"none", nil, nil},
		{"a sibling lends to the file", []ForeignCoverage{lend("svc/internal/domain/real_string.go", real)}, []string{"svc/internal/domain/real_string.go:3"}},
		{"a sibling lends to another file", []ForeignCoverage{lend("svc/internal/domain/real_string.go", "svc/internal/domain/other.go")}, nil},
		// The file's own directive is a line-directive finding already.
		{"the file names itself", []ForeignCoverage{lend(real, real)}, nil},
		{"two lenders", []ForeignCoverage{lend("svc/internal/domain/a.go", real), lend("svc/internal/domain/b.go", real)}, []string{"svc/internal/domain/a.go:3", "svc/internal/domain/b.go:3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := ForeignCoverageFindings(real, tt.foreign)
			if len(findings) != len(tt.want) {
				t.Fatalf("findings = %+v, want %d naming %q", findings, len(tt.want), tt.want)
			}
			for i, f := range findings {
				checkForeignFinding(t, f, real, tt.want[i])
			}
		})
	}
}

func checkForeignFinding(t *testing.T, f Finding, file, lender string) {
	t.Helper()
	if f.Rule != "foreign-coverage" || f.File != file || !strings.Contains(f.Message, lender) || !strings.Contains(f.Message, "//line real.go:5") {
		t.Fatalf("finding = %+v, want foreign-coverage on %s naming %s and the directive", f, file, lender)
	}
}

// Coverage that gives a line the file does not have describes other
// positions than the file's (R2-MR-1). Lines are counted as git counts them.
func TestCoverageBoundsFinding(t *testing.T) {
	content := []byte("package a\n\nfunc F() {}") // 3 lines, the last without a line feed
	tests := []struct {
		name     string
		coverage FileCoverage
		want     string
	}{
		{"every line inside", FileCoverage{1: 1, 3: 0}, ""},
		{"no lines", FileCoverage{}, ""},
		{"lines past the end", FileCoverage{2: 1, 5000: 0, 4: 0}, "gives line 4 of this file, which has 3 lines"},
		{"line zero", FileCoverage{0: 1, 2: 1}, "gives line 0 of this file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			finding, found := CoverageBoundsFinding("a.go", tt.coverage, LineCount(content))
			if found != (tt.want != "") {
				t.Fatalf("found = %v (%+v), want %v", found, finding, tt.want != "")
			}
			if found && (finding.Rule != "tool-failed" || finding.File != "a.go" || !strings.Contains(finding.Message, tt.want)) {
				t.Fatalf("finding = %+v, want tool-failed on a.go saying %q", finding, tt.want)
			}
		})
	}
}
