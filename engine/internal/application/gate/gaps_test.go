package gate

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"millennium/internal/application/port"
	"millennium/internal/domain"
)

// These tests drive the error and edge branches the full-tier self-audit
// found unexercised. Every one of them must block or stay silent on purpose.

var errInjected = errors.New("injected failure")

func singleConfig(t *testing.T, langs ...string) domain.Config {
	t.Helper()
	cfg, err := domain.NewConfig(domain.ConfigInput{Version: ptr(1), Languages: langs, Conventions: []string{"clean-architecture"}})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestBuildGateEdges(t *testing.T) {
	finder := fakeFinder{"": "go.mod", "web": "tsconfig.json"}
	run := func(tools *fakeBuildTools, files ...domain.ChangedFile) []string {
		findings, err := NewBuild(tools, finder).Run(t.Context(), port.GateInput{Config: singleConfig(t, "go", "typescript"), Changes: domain.ChangeSet{Files: files}})
		if err != nil {
			t.Fatal(err)
		}
		return rules(findings)
	}
	root := &fakeBuildTools{}
	if got := run(root, modified("main.go")); len(got) != 0 || !slices.Equal(root.calls, []string{"gofmt main.go", "vet "}) {
		t.Fatalf("a file at the repository root: findings %q, calls %q", got, root.calls)
	}
	if got := run(&fakeBuildTools{vetErr: errInjected}, modified("main.go")); !slices.Equal(got, []string{"build/tool-failed"}) {
		t.Fatalf("vet failure: %q", got)
	}
	if got := run(&fakeBuildTools{tsconfigErr: errInjected}, modified("web/a.ts")); !slices.Equal(got, []string{"build/tool-failed"}) {
		t.Fatalf("showConfig failure: %q", got)
	}
	if got := run(&fakeBuildTools{tsconfig: map[string]string{"web": "not json"}}, modified("web/a.ts")); !slices.Equal(got, []string{"build/tool-failed"}) {
		t.Fatalf("unreadable showConfig: %q", got)
	}
}

func TestCodeGateEdges(t *testing.T) {
	cfg, err := domain.NewConfig(domain.ConfigInput{Version: ptr(1), Components: []domain.ComponentInput{
		{Paths: []string{"svc"}, Languages: []string{"go", "python"}},
		{Paths: []string{"a/*"}, Languages: []string{"go"}},
		{Paths: []string{"*/b"}, Languages: []string{"go"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	analyzers := map[domain.Language]port.Analyzer{domain.LanguageGo: fakeAnalyzer{facts: map[string]domain.FileFacts{"x": {}}}}
	run := func(source *fakeSource, base string, files ...domain.ChangedFile) ([]domain.Finding, error) {
		return NewCode(analyzers, source).Run(t.Context(), port.GateInput{Config: cfg, Changes: domain.ChangeSet{Base: base, Files: files}})
	}
	source := &fakeSource{working: map[string]string{"a/b/c.go": "x", "svc/m.py": "x", "other/z.go": "x"}}
	findings, err := run(source, "", domain.ChangedFile{Path: "svc/gone.go", Kind: domain.ChangeDeleted},
		added("a/b/c.go"), added("svc/m.py"), added("other/z.go"))
	if err != nil || len(findings) != 0 {
		t.Fatalf("deleted, ambiguous, analyzer-less and unowned files get only the secret scan: %+v, %v", findings, err)
	}
	if _, err := run(&fakeSource{working: map[string]string{"svc/a.go": "x"}, changedErr: errInjected}, "tree", modified("svc/a.go")); !errors.Is(err, errInjected) {
		t.Fatalf("a changed-lines failure must fail the gate: %v", err)
	}
}

func TestShellGateEdges(t *testing.T) {
	cfg := shellConfig(t, "")
	run := func(source *fakeSource, tools *fakeShellTools, base string, file domain.ChangedFile) ([]domain.Finding, error) {
		return NewShell(source, tools).Run(t.Context(), port.GateInput{Config: cfg, Changes: domain.ChangeSet{Base: base, Files: []domain.ChangedFile{file}}})
	}
	findings, err := run(&fakeSource{}, &fakeShellTools{}, "", added("scripts/unreadable.sh"))
	if err != nil || len(findings) != 1 || findings[0].Rule != "analysis-failed" {
		t.Fatalf("an unreadable script: %+v, %v", findings, err)
	}
	noStrict := &fakeSource{working: map[string]string{"scripts/a.sh": "#!/bin/bash\n"}, readAtErr: errInjected}
	if _, err := run(noStrict, &fakeShellTools{}, "tree", modified("scripts/a.sh")); !errors.Is(err, errInjected) {
		t.Fatalf("a base read failure must fail the gate: %v", err)
	}
	strict := &fakeSource{working: map[string]string{"scripts/a.sh": strictScript}}
	if _, err := run(strict, &fakeShellTools{lintErr: errInjected}, "", added("scripts/a.sh")); !errors.Is(err, errInjected) {
		t.Fatalf("a shellcheck failure must fail the gate: %v", err)
	}
	strict.changedErr = errInjected
	if _, err := run(strict, &fakeShellTools{}, "tree", modified("scripts/a.sh")); !errors.Is(err, errInjected) {
		t.Fatalf("a changed-lines failure must fail the gate: %v", err)
	}
}

func TestStructureGateEdges(t *testing.T) {
	analyzers := map[domain.Language]port.Analyzer{
		domain.LanguageGo:    fakeAnalyzer{facts: map[string]domain.FileFacts{"long": longFile}},
		domain.LanguageShell: fakeAnalyzer{facts: map[string]domain.FileFacts{"#!/bin/bash\n": {}}},
	}
	cfg := singleConfig(t, "go", "shell", "python")
	run := func(source *fakeSource, packs port.PackSource, base string, files ...domain.ChangedFile) []string {
		findings, err := NewStructure(analyzers, packs, source).Run(t.Context(), port.GateInput{Config: cfg, Changes: domain.ChangeSet{Base: base, Files: files}})
		if err != nil {
			t.Fatal(err)
		}
		return rules(findings)
	}
	shebang := &fakeSource{working: map[string]string{"bin/deploy": "#!/bin/bash\n"}}
	if got := run(shebang, fakePacks{}, "", added("bin/deploy"), added("bin/missing")); len(got) != 0 {
		t.Fatalf("extensionless files: %q", got)
	}
	baseErr := &fakeSource{working: map[string]string{"internal/domain/a.go": "long"}, readAtErr: errInjected}
	if got := run(baseErr, fakePacks{}, "tree", modified("internal/domain/a.go")); !slices.Equal(got, []string{"structure/analysis-failed"}) {
		t.Fatalf("a base read failure: %q", got)
	}
	newFile := &fakeSource{working: map[string]string{"internal/domain/a.go": "long"}}
	if got := run(newFile, fakePacks{}, "tree", modified("internal/domain/a.go")); !slices.Equal(got, []string{"structure/function-length"}) {
		t.Fatalf("a modified file absent at base is new code: %q", got)
	}
	if got := run(newFile, brokenPacks{}, "", added("internal/domain/a.go")); !slices.Contains(got, "structure/convention-unavailable") {
		t.Fatalf("a broken pack source must block: %q", got)
	}
	python := &fakeSource{working: map[string]string{"pkg/domain.py": "x"}}
	pyAnalyzers := map[domain.Language]port.Analyzer{domain.LanguagePython: fakeAnalyzer{facts: map[string]domain.FileFacts{"x": {}}}}
	findings, err := NewStructure(pyAnalyzers, fakePacks{}, python).Run(t.Context(), port.GateInput{Config: cfg, Changes: domain.ChangeSet{Files: []domain.ChangedFile{added("pkg/domain.py")}}})
	if err != nil || len(findings) != 1 || !strings.Contains(findings[0].Message, "has no python rules") {
		t.Fatalf("a pack without rules for the language: %+v, %v", findings, err)
	}
}

type brokenPacks struct{}

func (brokenPacks) Pack(domain.Convention) (domain.Pack, error) {
	return domain.Pack{}, errInjected
}

func TestTestsGateEdges(t *testing.T) {
	runner := &fakeRunner{results: map[string]port.TestResult{"go svc": {Coverage: domain.Coverage{}}}}
	source := &fakeSource{working: map[string]string{"svc/internal/domain/user.go": "logic", "tts/pkg/domain.py": "logic"}}
	// An audit (no base) words the finding differently, and Python gets a
	// Python hint.
	findings := runTests(t, runner, source, domain.TierFast, "", added("svc/internal/domain/user.go"), added("tts/pkg/domain.py"),
		domain.ChangedFile{Path: "svc/internal/domain/gone.go", Kind: domain.ChangeDeleted}, added("scripts/x.sh"))
	if len(findings) != 2 || !strings.Contains(findings[0].Message, "has no tests") || !strings.Contains(findings[1].Message, "add or update test_domain.py") {
		t.Fatalf("findings = %+v", findings)
	}
	// Unreadable code and code without an analyzer are not candidates.
	unreadable := runTests(t, runner, &fakeSource{}, domain.TierFast, "", added("svc/internal/domain/user.go"), added("web/src/domain/x.tsx"))
	if len(unreadable) != 0 {
		t.Fatalf("findings = %+v", unreadable)
	}
	// A changed-lines failure in the full tier fails the gate.
	failing := &fakeSource{working: map[string]string{"svc/internal/domain/user.go": "logic"}, changedErr: errInjected}
	runnerWithFile := &fakeRunner{results: map[string]port.TestResult{"go svc": {Coverage: domain.Coverage{"svc/internal/domain/user.go": {1: 1}}}}}
	analyzers := map[domain.Language]port.Analyzer{domain.LanguageGo: fakeAnalyzer{facts: map[string]domain.FileFacts{"logic": logic}}}
	g := NewTests(analyzers, testedPacks{}, failing, runnerWithFile, fakeFinder{"svc": "go.mod"})
	in := port.GateInput{Config: testsConfig(t), Tier: domain.TierFull, Changes: domain.ChangeSet{Base: "tree", Files: []domain.ChangedFile{
		modified("svc/internal/domain/user.go"), added("svc/internal/domain/user_test.go")}}}
	if _, err := g.Run(t.Context(), in); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v", err)
	}
}

func TestSuitesAtTheRepositoryRoot(t *testing.T) {
	runner := &fakeRunner{results: map[string]port.TestResult{"go ": {Failed: true, Summary: "boom"}}}
	analyzers := map[domain.Language]port.Analyzer{domain.LanguageGo: fakeAnalyzer{facts: map[string]domain.FileFacts{"logic": logic}}}
	source := &fakeSource{working: map[string]string{"main.go": "logic"}}
	g := NewTests(analyzers, testedPacks{}, source, runner, fakeFinder{"": "go.mod"})
	findings, err := g.Run(t.Context(), port.GateInput{Config: singleConfig(t, "go"), Tier: domain.TierFull, Changes: domain.ChangeSet{Files: []domain.ChangedFile{added("main.go")}}})
	if err != nil || len(findings) != 1 || !strings.Contains(findings[0].Message, "tests of the repository root failed: boom") {
		t.Fatalf("findings = %+v, %v", findings, err)
	}
}

func TestTestsGateSkipsWhatItCannotJudge(t *testing.T) {
	// No Python analyzer: Python logic cannot be classified, so it is not a
	// candidate; the structure gate reports the missing analyzer. A deleted
	// file plans no suite.
	runner := &fakeRunner{results: map[string]port.TestResult{"python tts": {Coverage: domain.Coverage{}}}}
	source := &fakeSource{working: map[string]string{"tts/pkg/domain.py": "logic"}}
	analyzers := map[domain.Language]port.Analyzer{domain.LanguageGo: fakeAnalyzer{facts: map[string]domain.FileFacts{"logic": logic}}}
	g := NewTests(analyzers, testedPacks{}, source, runner, fakeFinder{})
	findings, err := g.Run(t.Context(), port.GateInput{Config: testsConfig(t), Tier: domain.TierFull, Changes: domain.ChangeSet{Files: []domain.ChangedFile{
		added("tts/pkg/domain.py"), {Path: "svc/internal/domain/gone.go", Kind: domain.ChangeDeleted}}}})
	if err != nil || len(findings) != 0 || !slices.Equal(runner.calls, []string{"python tts"}) {
		t.Fatalf("findings %+v, calls %q, err %v", findings, runner.calls, err)
	}
}
