package gate

import (
	"slices"
	"strings"
	"testing"

	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/domain"
)

// These tests cover the checks that a coverage report describes the files
// the tests gate judges (R2-CR-1, R2-MR-1, R2-SEC-1): the report's lines
// must be in the file, and no other file's code may count as the file's.
// The report is checked for those files only.

const dbFile = "svc/internal/infrastructure/db.go"

// runCoverageGate runs the tests gate in the full tier over a change to
// files, with logic in each, every line touched, and the suites' results.
func runCoverageGate(t *testing.T, results map[string]port.TestResult, working map[string]string, files ...domain.ChangedFile) []domain.Finding {
	t.Helper()
	analyzer := fakeAnalyzer{facts: map[string]domain.FileFacts{"logic": logic}}
	analyzers := map[domain.Language]port.Analyzer{domain.LanguageGo: analyzer, domain.LanguageTypeScript: analyzer}
	changed := map[string]domain.LineSet{}
	for _, f := range files {
		changed[f.Path] = domain.AllLines()
	}
	source := &fakeSource{working: working, changed: changed}
	g := NewTests(analyzers, source, &fakeRunner{results: results}, fakeFinder{"svc": "go.mod"})
	findings, err := g.Run(t.Context(), port.GateInput{Config: testsConfig(t), Changes: domain.ChangeSet{Base: "tree", Files: files}, Tier: domain.TierFull})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return findings
}

// coverageRules renders the findings other than tests-missing as
// "rule file: message".
func coverageRules(findings []domain.Finding) []string {
	var out []string
	for _, f := range findings {
		if f.Rule != "tests-missing" {
			out = append(out, f.Rule+" "+f.File+": "+f.Message)
		}
	}
	return out
}

// havePrefixes reports whether got holds as many strings as want, each
// starting with its counterpart.
func havePrefixes(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if !strings.HasPrefix(got[i], want[i]) {
			return false
		}
	}
	return true
}

// A report that gives lines or files the module does not have, for files the
// gate does not judge (goyacc's yaccpar, an excluded or unchanged file, a
// file outside the tested layers), blocks nothing; for a file it judges, it
// blocks that file only, whichever tool wrote the report.
func TestCoveragePositionsAreCheckedForJudgedFilesOnly(t *testing.T) {
	goCoverage := domain.Coverage{
		userFile: {3: 0},
		"svc/internal/infrastructure/parser/yaccpar": {1: 1, 900: 0},
		"svc/internal/domain/gen.go":                 {5000: 0},
		dbFile:                                       {5000: 0},
	}
	tests := []struct {
		name    string
		results map[string]port.TestResult
		working map[string]string
		files   []domain.ChangedFile
		want    []string
	}{
		{"lines past the end of files the gate does not judge",
			map[string]port.TestResult{"go svc": {Coverage: goCoverage}},
			map[string]string{userFile: long("logic"), dbFile: "logic"},
			[]domain.ChangedFile{modified(userFile), modified(dbFile)},
			[]string{"uncovered-change " + userFile + ": changed lines 3 never run in any test"}},
		{"a line past the end of a judged Go file",
			map[string]port.TestResult{"go svc": {Coverage: domain.Coverage{userFile: {3: 1, 20: 0, 21: 0}}}},
			map[string]string{userFile: long("logic")},
			[]domain.ChangedFile{modified(userFile)},
			[]string{"tool-failed " + userFile + ": the coverage report gives line 21 of this file, which has 20 lines"}},
		{"a line past the end of a judged file from an lcov report",
			map[string]port.TestResult{"command api npm test": {Coverage: domain.Coverage{"api/src/domain/a.ts": {1: 1, 2: 0}}}},
			map[string]string{"api/src/domain/a.ts": "logic"},
			[]domain.ChangedFile{modified("api/src/domain/a.ts")},
			[]string{"tool-failed api/src/domain/a.ts: the coverage report gives line 2 of this file, which has 1 lines"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := coverageRules(runCoverageGate(t, tt.results, tt.working, tt.files...)); !havePrefixes(got, tt.want) {
				t.Fatalf("findings = %q, want %q", got, tt.want)
			}
		})
	}
}

// Coverage of a judged file may come only from that file: a //line
// directive in a file beside it, excluded or not, lends it coverage of code
// that ran elsewhere, so the gate blocks rather than read lines as run
// (R2-SEC-1). Directives that credit any other file block nothing.
func TestForeignCoverageBlocksTheFileItIsCreditedTo(t *testing.T) {
	lend := func(file, credited string) domain.ForeignCoverage {
		return domain.ForeignCoverage{File: file, Directive: domain.LineDirective{Line: 5, Text: "//line user.go:3", File: "user.go"}, Credited: credited}
	}
	ran := domain.Coverage{userFile: {3: 1, 4: 1}}
	tests := []struct {
		name    string
		foreign []domain.ForeignCoverage
		want    []string
	}{
		{"none", nil, nil},
		{"an excluded sibling lends to the file", []domain.ForeignCoverage{lend("svc/internal/domain/user_string.go", userFile)},
			[]string{"foreign-coverage " + userFile + `: svc/internal/domain/user_string.go:5 holds "//line user.go:3"`}},
		{"a sibling lends to a file the gate does not judge", []domain.ForeignCoverage{lend("svc/internal/domain/user_string.go", "svc/internal/domain/other.go")}, nil},
		{"goyacc's driver", []domain.ForeignCoverage{lend("svc/internal/infrastructure/parser/y.go", "svc/internal/infrastructure/parser/yaccpar")}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := map[string]port.TestResult{"go svc": {Coverage: ran, Foreign: tt.foreign}}
			got := coverageRules(runCoverageGate(t, results, map[string]string{userFile: long("logic")}, modified(userFile)))
			if !havePrefixes(got, tt.want) {
				t.Fatalf("findings = %q, want %q", got, tt.want)
			}
		})
	}
}

// The foreign code of every suite counts, and a suite that gave no usable
// result reports nothing more for its files.
func TestForeignCoverageAcrossSuites(t *testing.T) {
	lent := []domain.ForeignCoverage{{File: "svc/internal/domain/z.go", Directive: domain.LineDirective{Line: 1, Text: "//line user.go:1", File: "user.go"}, Credited: userFile}}
	results := map[string]port.TestResult{
		"go svc":               {Coverage: domain.Coverage{userFile: {1: 1}}, Foreign: lent},
		"command api npm test": {Failed: true, Summary: "1 failed"},
	}
	working := map[string]string{userFile: long("logic"), "api/src/domain/a.ts": "logic"}
	got := rules(runCoverageGate(t, results, working, modified(userFile), modified("api/src/domain/a.ts")))
	slices.Sort(got)
	if want := []string{"tests/foreign-coverage", "tests/tests-failed", "tests/tests-missing", "tests/tests-missing"}; !slices.Equal(got, want) {
		t.Fatalf("rules = %q, want %q", got, want)
	}
}
