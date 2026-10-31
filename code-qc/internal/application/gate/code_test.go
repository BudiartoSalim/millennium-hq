package gate

import (
	"slices"
	"strings"
	"testing"

	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/domain"
)

func codeConfig(t *testing.T) domain.Config {
	t.Helper()
	cfg, err := domain.NewConfig(domain.ConfigInput{Version: ptr(1), Components: []domain.ComponentInput{
		{Paths: []string{"svc"}, Languages: []string{"go"}},
	}})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	return cfg
}

func runCode(t *testing.T, source *fakeSource, facts map[string]domain.FileFacts, base string, files ...domain.ChangedFile) []domain.Finding {
	t.Helper()
	analyzers := map[domain.Language]port.Analyzer{domain.LanguageGo: fakeAnalyzer{facts: facts}}
	findings, err := NewCode(analyzers, source).Run(t.Context(), port.GateInput{Config: codeConfig(t), Changes: domain.ChangeSet{Base: base, Files: files}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return findings
}

func TestCodeGateFindsEachKind(t *testing.T) {
	risky := domain.FileFacts{
		Constructs: []domain.Construct{{Kind: domain.ConstructTLSVerifyOff, Line: 2, Detail: "InsecureSkipVerify: true"}},
		Comments: []domain.Comment{
			{Line: 3, Text: "//nolint:gosec"},
			{Line: 4, Text: "// TODO: later"},
		},
	}
	source := &fakeSource{working: map[string]string{
		"svc/a.go":      "risky",
		"config/.env":   "API_TOKEN=\"ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789\"\n", // millennium:allow secret -- fake credential, test input for the secret scanner
		"svc/logo.png":  "\x89PNG\x00\x00",
		"docs/notes.md": "TODO: unlinked, but not code\n",
	}}
	findings := runCode(t, source, map[string]domain.FileFacts{"risky": risky}, "",
		added("svc/a.go"), added("config/.env"), added("svc/logo.png"), added("docs/notes.md"))
	got := rules(findings)
	want := []string{"code/banned-construct", "code/suppression-without-reason", "code/todo-without-link", "code/secret"}
	if strings.Join(sortedCopy(got), ",") != strings.Join(sortedCopy(want), ",") {
		t.Fatalf("findings %q, want %q", got, want)
	}
}

func TestCodeGateChecksOnlyTouchedLinesOfModifiedFiles(t *testing.T) {
	facts := domain.FileFacts{Comments: []domain.Comment{{Line: 1, Text: "// TODO: old"}, {Line: 5, Text: "// TODO: new"}}}
	source := &fakeSource{
		working: map[string]string{"svc/a.go": "todos"},
		changed: map[string]domain.LineSet{"svc/a.go": {Ranges: []domain.LineRange{{First: 5, Last: 5}}}},
	}
	findings := runCode(t, source, map[string]domain.FileFacts{"todos": facts}, "tree1", modified("svc/a.go"))
	if len(findings) != 1 || findings[0].Line != 5 {
		t.Fatalf("want only the TODO on the touched line, got %+v", findings)
	}
}

func TestCodeGateBlocksWhatItCannotScan(t *testing.T) {
	findings := runCode(t, &fakeSource{working: map[string]string{"svc/b.go": "garbage"}}, nil, "", added("svc/missing.go"), added("svc/b.go"))
	if got := rules(findings); len(got) != 2 || got[0] != "code/scan-failed" || got[1] != "code/scan-failed" {
		t.Fatalf("findings %q, want two scan-failed", got)
	}
}

func sortedCopy(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)
	return out
}
