package gate

import (
	"errors"
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
	return runForeignGate(t, results, &fakeForeign{}, working, files...)
}

// runForeignGate is runCoverageGate with what the Go sources of each
// directory lend. Content "no runtime" is logic without runtime lines.
func runForeignGate(t *testing.T, results map[string]port.TestResult, foreign *fakeForeign, working map[string]string, files ...domain.ChangedFile) []domain.Finding {
	t.Helper()
	analyzer := &countingAnalyzer{facts: map[string]domain.FileFacts{"logic": logic, "no runtime": runtimeLogic(t, "a")}}
	analyzers := map[domain.Language]port.Analyzer{domain.LanguageGo: analyzer, domain.LanguageTypeScript: analyzer}
	changed := map[string]domain.LineSet{}
	for _, f := range files {
		changed[f.Path] = domain.AllLines()
	}
	source := &fakeSource{working: working, changed: changed}
	g := NewTests(analyzers, source, &fakeRunner{results: results}, fakeFinder{"svc": "go.mod"}, foreign)
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

// The out-of-range message names the causes of the file's language: no
// //line directive for TypeScript (QT3-4).
func TestCoverageBoundsMessageFitsTheLanguage(t *testing.T) {
	results := map[string]port.TestResult{"command api npm test": {Coverage: domain.Coverage{"api/src/domain/a.ts": {7: 0}}}}
	findings := runCoverageGate(t, results, map[string]string{"api/src/domain/a.ts": "logic"}, modified("api/src/domain/a.ts"))
	i := slices.IndexFunc(findings, func(f domain.Finding) bool { return f.Rule == "tool-failed" })
	if i < 0 || strings.Contains(findings[i].Message, "//line") || !strings.Contains(findings[i].Message, "source map") {
		t.Fatalf("findings = %+v, want a tool-failed naming source maps and no //line directive", findings)
	}
}

const (
	stringer  = "svc/internal/domain/user_string.go"
	domainDir = "svc/internal/domain"
)

// lend is a //line directive in file that credits the code after it to
// credited.
func lend(file, credited string) domain.ForeignCoverage {
	return domain.ForeignCoverage{File: file, Directive: domain.LineDirective{Line: 5, Text: "//line user.go:3", File: "user.go"}, Credited: credited}
}

// Coverage of a judged file may come only from that file: a //line
// directive in a file beside it, excluded or not, lends it coverage of code
// that ran elsewhere, so the gate blocks rather than read lines as run
// (R2-SEC-1). Directives that credit any other file block nothing.
func TestForeignCoverageBlocksTheFileItIsCreditedTo(t *testing.T) {
	ran := domain.Coverage{userFile: {3: 1, 4: 1}}
	tests := []struct {
		name    string
		foreign []domain.ForeignCoverage
		want    []string
	}{
		{"none", nil, nil},
		{"an excluded sibling lends to the file", []domain.ForeignCoverage{lend(stringer, userFile)},
			[]string{"foreign-coverage " + userFile + `: svc/internal/domain/user_string.go:5 holds "//line user.go:3"`}},
		{"a sibling lends to a file the gate does not judge", []domain.ForeignCoverage{lend(stringer, "svc/internal/domain/other.go")}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			foreign := &fakeForeign{lent: map[string][]domain.ForeignCoverage{domainDir: tt.foreign}}
			got := coverageRules(runForeignGate(t, map[string]port.TestResult{"go svc": {Coverage: ran}}, foreign, map[string]string{userFile: long("logic")}, modified(userFile)))
			if !havePrefixes(got, tt.want) {
				t.Fatalf("findings = %q, want %q", got, tt.want)
			}
		})
	}
}

// A Go component may run its tests with its own recipe, around the same
// cover tool, so a //line directive beside a judged file lends it coverage
// just the same: the gate asks the Go sources whatever ran the suite
// (R3-SEC-4).
func TestForeignCoverageWhateverRanTheSuite(t *testing.T) {
	const real = "gor/internal/domain/real.go"
	results := map[string]port.TestResult{"command gor make cover": {Coverage: domain.Coverage{real: {1: 1}}}}
	foreign := &fakeForeign{lent: map[string][]domain.ForeignCoverage{"gor/internal/domain": {lend("gor/internal/domain/real_string.go", real)}}}
	got := coverageRules(runForeignGate(t, results, foreign, map[string]string{real: "logic"}, modified(real)))
	if want := []string{"foreign-coverage " + real + ": gor/internal/domain/real_string.go:5"}; !havePrefixes(got, want) {
		t.Fatalf("findings = %q, want %q", got, want)
	}
}

// The gate asks only for the directories of Go candidates whose suite
// passed and whose change touched runtime code, each once (R3-PERF-1): a
// failed suite already blocks, other languages' tools credit no code to
// other files, and a change without runtime code needs no coverage.
func TestForeignCoverageIsAskedOnlyWhereItCounts(t *testing.T) {
	results := map[string]port.TestResult{
		"go svc":                 {Coverage: domain.Coverage{userFile: {1: 1}, "svc/internal/domain/other.go": {1: 1}}},
		"command gor make cover": {Failed: true, Summary: "1 failed"},
		"command api npm test":   {Coverage: domain.Coverage{"api/src/domain/a.ts": {1: 1}}},
	}
	working := map[string]string{
		userFile: "logic", "svc/internal/domain/other.go": "logic", "svc/internal/domain/sub/doc.go": "no runtime",
		"gor/internal/domain/real.go": "logic", "api/src/domain/a.ts": "logic", dbFile: "logic",
	}
	foreign := &fakeForeign{}
	runForeignGate(t, results, foreign, working, modified(userFile), modified("svc/internal/domain/other.go"), modified("svc/internal/domain/sub/doc.go"),
		modified("gor/internal/domain/real.go"), modified("api/src/domain/a.ts"), modified(dbFile), modified("svc/internal/domain/user_test.go"))
	if want := []string{domainDir}; !slices.Equal(foreign.asked, want) {
		t.Fatalf("directories asked = %q, want %q", foreign.asked, want)
	}
}

// A Go source that cannot be read (a FIFO, a file over the size limit)
// leaves unknown what it lends, which blocks the judged files of its
// directory, and only them (R3-SEC-5): files elsewhere in the module are
// checked as usual, and no module-wide tool-failed hides them.
func TestUnreadableForeignCoverageBlocksItsDirectoryOnly(t *testing.T) {
	const elsewhere = "svc/internal/domain/sub/x.go"
	results := map[string]port.TestResult{"go svc": {Coverage: domain.Coverage{userFile: {1: 1}, "svc/internal/domain/other.go": {1: 1}, elsewhere: {1: 0}}}}
	foreign := &fakeForeign{errs: map[string]error{domainDir: errors.New("read the line directives of svc/internal/domain/big.go: too large")}}
	working := map[string]string{userFile: "logic", "svc/internal/domain/other.go": "logic", elsewhere: "logic"}
	got := coverageRules(runForeignGate(t, results, foreign, working, modified(userFile), modified("svc/internal/domain/other.go"), modified(elsewhere)))
	slices.Sort(got)
	want := []string{
		"tool-failed svc/internal/domain/other.go: whether a //line directive in a Go file beside this one lends it coverage",
		"tool-failed " + userFile + ": whether a //line directive in a Go file beside this one lends it coverage",
		"uncovered-change " + elsewhere + ": changed lines 1 never run in any test",
	}
	slices.Sort(want)
	if !havePrefixes(got, want) || !strings.Contains(got[0], "big.go: too large") {
		t.Fatalf("findings = %q, want %q naming the cause", got, want)
	}
}
