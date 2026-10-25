package gate

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"millennium/internal/application/port"
	"millennium/internal/domain"
)

type fakeRunner struct {
	results map[string]port.TestResult
	err     error
	calls   []string
}

func (f *fakeRunner) result(call string) (port.TestResult, error) {
	f.calls = append(f.calls, call)
	return f.results[call], f.err
}

func (f *fakeRunner) GoTest(_ context.Context, dir string) (port.TestResult, error) {
	return f.result("go " + dir)
}

func (f *fakeRunner) PythonTest(_ context.Context, dir string) (port.TestResult, error) {
	return f.result("python " + dir)
}

func (f *fakeRunner) Command(_ context.Context, dir string, recipe domain.TestRecipe) (port.TestResult, error) {
	return f.result("command " + dir + " " + strings.Join(recipe.Command, " "))
}

// testedPacks marks domain as tested, infrastructure as not.
type testedPacks struct{}

func (testedPacks) Pack(domain.Convention) (domain.Pack, error) {
	return domain.NewPack(domain.PackInput{
		Name:      "clean-architecture",
		Languages: []string{"go", "typescript", "python"},
		Layers:    []domain.PackLayer{{Name: "domain", Tested: true}, {Name: "infrastructure", MayImport: []string{"domain"}, ThirdParty: true}},
		Paths: map[string]map[string][]string{
			"go":         {"domain": {"internal/domain"}, "infrastructure": {"internal/infrastructure"}},
			"python":     {"domain": {"*/domain.py"}},
			"typescript": {"domain": {"src/domain"}},
		},
	})
}

func testsConfig(t *testing.T) domain.Config {
	t.Helper()
	cfg, err := domain.NewConfig(domain.ConfigInput{Version: ptr(1), Components: []domain.ComponentInput{
		{Paths: []string{"svc"}, Languages: []string{"go"}, Conventions: []string{"clean-architecture"}},
		{Paths: []string{"tts"}, Languages: []string{"python"}, Conventions: []string{"clean-architecture"}},
		{Paths: []string{"web"}, Languages: []string{"typescript"}, Conventions: []string{"clean-architecture"}},
		{Paths: []string{"api"}, Languages: []string{"typescript"}, Conventions: []string{"clean-architecture"},
			Test: &domain.TestRecipe{Command: []string{"npm", "test"}, LCOV: "coverage/lcov.info"}},
	}})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	return cfg
}

var logic = domain.FileFacts{Functions: []domain.FunctionFacts{{Key: "f"}}}

func runTests(t *testing.T, runner *fakeRunner, source *fakeSource, tier domain.Tier, base string, files ...domain.ChangedFile) []domain.Finding {
	t.Helper()
	analyzers := map[domain.Language]port.Analyzer{
		domain.LanguageGo:         fakeAnalyzer{facts: map[string]domain.FileFacts{"logic": logic, "types": {}}},
		domain.LanguagePython:     fakeAnalyzer{facts: map[string]domain.FileFacts{"logic": logic}},
		domain.LanguageTypeScript: fakeAnalyzer{facts: map[string]domain.FileFacts{"logic": logic}},
	}
	g := NewTests(analyzers, testedPacks{}, source, runner, fakeFinder{"svc": "go.mod"})
	findings, err := g.Run(t.Context(), port.GateInput{Config: testsConfig(t), Changes: domain.ChangeSet{Base: base, Files: files}, Tier: tier})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return findings
}

func TestTestsRequiredInTheFastTier(t *testing.T) {
	source := &fakeSource{working: map[string]string{
		"svc/internal/domain/user.go": "logic", "svc/internal/domain/types.go": "types",
		"svc/internal/infrastructure/db.go": "logic", "tts/pkg/domain.py": "logic", "web/src/domain/cart.ts": "logic",
	}}
	findings := runTests(t, &fakeRunner{}, source, domain.TierFast, "tree",
		modified("svc/internal/domain/user.go"),                          // tested layer, no test change: blocked
		modified("svc/internal/domain/types.go"),                         // types only: exempt
		modified("svc/internal/infrastructure/db.go"),                    // untested layer: exempt
		modified("tts/pkg/domain.py"), added("tts/tests/test_domain.py"), // has its test
		modified("web/src/domain/cart.ts"), added("web/src/domain/other.test.ts"), // unrelated test
	)
	var files []string
	for _, f := range findings {
		if f.Rule != "tests-missing" {
			t.Fatalf("unexpected finding %+v", f)
		}
		files = append(files, f.File)
	}
	if want := []string{"svc/internal/domain/user.go", "web/src/domain/cart.ts"}; !slices.Equal(files, want) {
		t.Fatalf("tests-missing for %q, want %q", files, want)
	}
}

func TestTestsFullTierRunsSuitesAndChecksCoverage(t *testing.T) {
	runner := &fakeRunner{results: map[string]port.TestResult{
		"go svc":               {Coverage: domain.Coverage{"svc/internal/domain/user.go": {3: 1, 4: 0, 9: 0}}},
		"python tts":           {Failed: true, Summary: "1 of 2 tests failed: test_x"},
		"command api npm test": {Coverage: domain.Coverage{}},
	}}
	source := &fakeSource{
		working: map[string]string{"svc/internal/domain/user.go": "logic", "tts/pkg/domain.py": "logic", "api/src/domain/a.ts": "logic", "web/src/domain/b.ts": "logic"},
		changed: map[string]domain.LineSet{"svc/internal/domain/user.go": {Ranges: []domain.LineRange{{First: 3, Last: 5}}}},
	}
	findings := runTests(t, runner, source, domain.TierFull, "tree",
		modified("svc/internal/domain/user.go"), added("svc/internal/domain/user_test.go"),
		modified("tts/pkg/domain.py"), added("tts/tests/test_domain.py"),
		modified("api/src/domain/a.ts"), added("api/src/domain/a.test.ts"),
		modified("web/src/domain/b.ts"), added("web/src/domain/b.test.ts"))
	if want := []string{"go svc", "python tts", "command api npm test"}; !slices.Equal(runner.calls, want) {
		t.Fatalf("suite runs = %q, want %q", runner.calls, want)
	}
	got := map[string]string{}
	for _, f := range findings {
		got[f.Rule+" "+f.File] = f.Message
	}
	if msg := got["uncovered-change svc/internal/domain/user.go"]; msg != "changed lines 4 never run in any test" {
		t.Errorf("go coverage: %q (line 9 is not touched, 5 is not executable)", msg)
	}
	if msg := got["tests-failed "]; !strings.Contains(msg, "tests of tts failed: 1 of 2") {
		t.Errorf("python failure: %q", msg)
	}
	if _, ok := got["uncovered-change api/src/domain/a.ts"]; !ok {
		t.Errorf("a file no test executed must be reported: %v", got)
	}
	if msg := got["tests-unconfigured "]; !strings.Contains(msg, "component web has typescript code but no test recipe") {
		t.Errorf("typescript without recipe: %q", msg)
	}
	// tts failed and web has no recipe: those findings block, and their
	// files' coverage is not reported on top.
	if len(findings) != 4 {
		t.Errorf("findings = %+v", findings)
	}
}

func TestTestsFastTierRunsNoSuites(t *testing.T) {
	runner := &fakeRunner{}
	source := &fakeSource{working: map[string]string{"svc/internal/domain/user.go": "logic"}}
	runTests(t, runner, source, domain.TierFast, "", added("svc/internal/domain/user.go"), added("svc/internal/domain/user_test.go"))
	if len(runner.calls) != 0 {
		t.Fatalf("the fast tier ran suites: %q", runner.calls)
	}
}

func TestTestsRunnerErrorBlocks(t *testing.T) {
	runner := &fakeRunner{err: errors.New("go: command not found")}
	source := &fakeSource{working: map[string]string{"svc/internal/infrastructure/db.go": "logic"}}
	findings := runTests(t, runner, source, domain.TierFull, "", added("svc/internal/infrastructure/db.go"))
	if got := rules(findings); !slices.Equal(got, []string{"tests/tool-failed"}) {
		t.Fatalf("findings = %q", got)
	}
}
