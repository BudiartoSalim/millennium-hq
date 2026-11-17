package gate

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/domain"
)

// These tests cover the runtime-code facts in the tests gate: a change that
// touches only comments, whitespace or erased types needs no test change
// (tests-missing), and a changed line without runtime code need not run
// (uncovered-change). Unknown facts fail closed in both.

const userFile = "svc/internal/domain/user.go"

// countingAnalyzer maps content to facts like fakeAnalyzer, and counts the
// analyses it was asked for. Content it does not know fails; content named
// "partly <known content>" fails too, but returns that content's facts
// alongside the error.
type countingAnalyzer struct {
	facts map[string]domain.FileFacts
	calls int
}

func (a *countingAnalyzer) Analyze(_ context.Context, path string, content []byte) (domain.FileFacts, error) {
	a.calls++
	failure := errors.New("parse " + path + ": syntax error")
	if partial, ok := strings.CutPrefix(string(content), "partly "); ok {
		return a.facts[partial], failure
	}
	facts, ok := a.facts[string(content)]
	if !ok {
		return domain.FileFacts{}, failure
	}
	facts.Path = path
	return facts, nil
}

// recordingSource is a fakeSource that records every revision ReadAt is
// asked for.
type recordingSource struct {
	*fakeSource
	revs []string
}

func (s *recordingSource) ReadAt(ctx context.Context, rev, path string) ([]byte, bool, error) {
	s.revs = append(s.revs, rev)
	return s.fakeSource.ReadAt(ctx, rev, path)
}

// runtimeLogic is logic whose runtime code is digest on lines.
func runtimeLogic(t *testing.T, digest string, lines ...int) domain.FileFacts {
	t.Helper()
	runtime, err := domain.NewRuntimeCode(lines, strings.Repeat(digest, 64/len(digest)), 20)
	if err != nil {
		t.Fatal(err)
	}
	facts := logic
	facts.Runtime = runtime
	return facts
}

func runtimeFacts(t *testing.T) map[string]domain.FileFacts {
	t.Helper()
	return map[string]domain.FileFacts{
		"v1":           runtimeLogic(t, "a", 3, 4, 5),
		"v1, new doc":  runtimeLogic(t, "a", 4, 5, 6),
		"v2":           runtimeLogic(t, "b", 3, 4, 5),
		"unknown":      logic,
		"also unknown": logic,
	}
}

func runRuntimeGate(t *testing.T, source port.SourceReader, analyzer *countingAnalyzer, runner *fakeRunner, tier domain.Tier, base string, files ...domain.ChangedFile) []domain.Finding {
	t.Helper()
	g := NewTests(map[domain.Language]port.Analyzer{domain.LanguageGo: analyzer}, source, runner, fakeFinder{"svc": "go.mod"})
	findings, err := g.Run(t.Context(), port.GateInput{Config: testsConfig(t), Changes: domain.ChangeSet{Base: base, Files: files}, Tier: tier})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return findings
}

func TestTestsMissingComparesRuntimeWithBase(t *testing.T) {
	tests := []struct {
		name          string
		base          map[string]string
		readAtErr     error
		working       string
		wantMissing   bool
		wantAnalyses  int
		wantReadAtRev []string
	}{
		{"comment-only change", map[string]string{userFile: "v1"}, nil, "v1, new doc", false, 2, []string{"base-rev"}},
		{"runtime change", map[string]string{userFile: "v1"}, nil, "v2", true, 2, []string{"base-rev"}},
		{"unknown working facts", map[string]string{userFile: "unknown"}, nil, "also unknown", true, 2, []string{"base-rev"}},
		{"unknown base facts", map[string]string{userFile: "unknown"}, nil, "v1", true, 2, []string{"base-rev"}},
		{"identical unknown content", map[string]string{userFile: "unknown"}, nil, "unknown", true, 2, []string{"base-rev"}},
		{"base not found", nil, nil, "v1", true, 1, []string{"base-rev"}},
		{"base read fails", map[string]string{userFile: "v1"}, errInjected, "v1", true, 1, []string{"base-rev"}},
		{"base analysis fails", map[string]string{userFile: "does not parse"}, nil, "v1", true, 2, []string{"base-rev"}},
		{"base analysis fails with facts", map[string]string{userFile: "partly v1"}, nil, "v1, new doc", true, 2, []string{"base-rev"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := &recordingSource{fakeSource: &fakeSource{working: map[string]string{userFile: tt.working}, base: tt.base, readAtErr: tt.readAtErr}}
			analyzer := &countingAnalyzer{facts: runtimeFacts(t)}
			findings := runRuntimeGate(t, source, analyzer, &fakeRunner{}, domain.TierFast, "base-rev", modified(userFile))
			if missing := slices.Equal(rules(findings), []string{"tests/tests-missing"}); missing != tt.wantMissing || len(findings) > 1 {
				t.Fatalf("findings = %+v, want tests-missing %v", findings, tt.wantMissing)
			}
			if analyzer.calls != tt.wantAnalyses || !slices.Equal(source.revs, tt.wantReadAtRev) {
				t.Fatalf("analyses %d, ReadAt revisions %q; want %d and %q", analyzer.calls, source.revs, tt.wantAnalyses, tt.wantReadAtRev)
			}
		})
	}
}

func TestTestsMissingNeverComparesAddedFilesOrAudits(t *testing.T) {
	tests := []struct {
		name string
		base string
		file domain.ChangedFile
	}{
		{"added file", "base-rev", added(userFile)},
		{"audit", "", modified(userFile)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The base holds the same runtime code; it must not be read.
			source := &recordingSource{fakeSource: &fakeSource{working: map[string]string{userFile: "v1, new doc"}, base: map[string]string{userFile: "v1"}}}
			findings := runRuntimeGate(t, source, &countingAnalyzer{facts: runtimeFacts(t)}, &fakeRunner{}, domain.TierFast, tt.base, tt.file)
			if !slices.Equal(rules(findings), []string{"tests/tests-missing"}) || len(source.revs) != 0 {
				t.Fatalf("findings = %+v, ReadAt revisions %q; want tests-missing and no base read", findings, source.revs)
			}
		})
	}
}

func TestTestsSkipsTheBaseWhenATestChanged(t *testing.T) {
	source := &recordingSource{fakeSource: &fakeSource{working: map[string]string{userFile: "v2"}, base: map[string]string{userFile: "v1"}}}
	findings := runRuntimeGate(t, source, &countingAnalyzer{facts: runtimeFacts(t)}, &fakeRunner{}, domain.TierFast, "base-rev",
		modified(userFile), added("svc/internal/domain/user_test.go"))
	if len(findings) != 0 || len(source.revs) != 0 {
		t.Fatalf("findings = %+v, ReadAt revisions %q; a changed test needs no comparison", findings, source.revs)
	}
}

func TestTestsAnalysesEachCandidateAtMostTwice(t *testing.T) {
	files := []string{userFile, "svc/internal/domain/order.go", "svc/internal/domain/cart.go"}
	working, base := map[string]string{}, map[string]string{}
	var changes []domain.ChangedFile
	for i, f := range files {
		working[f] = []string{"v1, new doc", "v2", "unknown"}[i]
		base[f] = "v1"
		changes = append(changes, modified(f))
	}
	runner := &fakeRunner{results: map[string]port.TestResult{"go svc": {Coverage: domain.Coverage{
		files[0]: {4: 1}, files[1]: {4: 1}, files[2]: {4: 1},
	}}}}
	source := &recordingSource{fakeSource: &fakeSource{working: working, base: base, changed: map[string]domain.LineSet{}}}
	analyzer := &countingAnalyzer{facts: runtimeFacts(t)}
	findings := runRuntimeGate(t, source, analyzer, runner, domain.TierFull, "base-rev", changes...)
	if analyzer.calls > 2*len(files) {
		t.Fatalf("%d analyses for %d candidates, want at most %d", analyzer.calls, len(files), 2*len(files))
	}
	var missing []string
	for _, f := range findings {
		missing = append(missing, f.File)
	}
	if want := []string{files[1], files[2]}; !slices.Equal(missing, want) {
		t.Fatalf("tests-missing for %q, want %q", missing, want)
	}
}

// coverageFindings renders the findings other than tests-missing.
func coverageFindings(findings []domain.Finding) []string {
	var out []string
	for _, f := range findings {
		if f.Rule != "tests-missing" {
			out = append(out, f.Rule+": "+f.Message)
		}
	}
	return out
}

func TestUncoveredChangeCountsOnlyRuntimeLines(t *testing.T) {
	// Lines 1 and 2 are a doc comment and an erased declaration that a
	// source-mapped lcov reports with zero hits; 3-5 hold runtime code.
	sourceMapped := domain.FileCoverage{1: 0, 2: 0, 3: 0, 4: 1, 5: 0}
	touched := domain.LineSet{Ranges: []domain.LineRange{{First: 1, Last: 4}}}
	tests := []struct {
		name, base, working string
		coverage            domain.FileCoverage
		want                []string
	}{
		{"known runtime lines", "v2", "v1", sourceMapped, []string{"uncovered-change: changed lines 3 never run in any test"}},
		{"unknown runtime lines", "v1", "unknown", sourceMapped, []string{"uncovered-change: changed lines 1-3 never run in any test"}},
		// Equal digests take the file out of tests-missing only.
		{"runtime unchanged", "v1", "v1, new doc", domain.FileCoverage{4: 0, 5: 1}, []string{"uncovered-change: changed lines 4 never run in any test"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{results: map[string]port.TestResult{"go svc": {Coverage: domain.Coverage{userFile: tt.coverage}}}}
			source := &recordingSource{fakeSource: &fakeSource{
				working: map[string]string{userFile: tt.working}, base: map[string]string{userFile: tt.base},
				changed: map[string]domain.LineSet{userFile: touched},
			}}
			findings := runRuntimeGate(t, source, &countingAnalyzer{facts: runtimeFacts(t)}, runner, domain.TierFull, "base-rev", modified(userFile))
			if got := coverageFindings(findings); !slices.Equal(got, tt.want) {
				t.Fatalf("findings = %q, want %q", got, tt.want)
			}
		})
	}
}
