package gate

import (
	"slices"
	"strconv"
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
	findings, err := NewHygiene(analyzers, source).Run(t.Context(), port.GateInput{Config: codeConfig(t), Changes: domain.ChangeSet{Base: base, Files: files}})
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
	want := []string{"hygiene/banned-construct", "hygiene/suppression-without-reason", "hygiene/todo-without-link", "hygiene/secret"}
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
	if got := rules(findings); len(got) != 2 || got[0] != "hygiene/scan-failed" || got[1] != "hygiene/scan-failed" {
		t.Fatalf("findings %q, want two scan-failed", got)
	}
}

// Node reads its coverage directives from raw lines, so a directive in a
// TypeScript string switches coverage off for the rest of the file, and the
// gate reports it like a written one, though no comment holds it
// (R3-SEC-2). Go code runs on no Node: the same string there is text.
func TestCodeGateReadsNodeDirectivesInTypeScriptStrings(t *testing.T) {
	const inString = "// Banner.\nexport const banner = \"/* node:coverage disable */\";\nexport const n = 1;\n"
	const inTemplate = "export const banner = `\n/* node:coverage ignore next 9 */\n`;\n"
	const goString = "package a\n\nconst banner = \"/* node:coverage disable */\"\n"
	cfg, err := domain.NewConfig(domain.ConfigInput{Version: ptr(1), Components: []domain.ComponentInput{
		{Paths: []string{"svc"}, Languages: []string{"go"}},
		{Paths: []string{"web"}, Languages: []string{"typescript"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	analyzer := fakeAnalyzer{facts: map[string]domain.FileFacts{
		factsKey([]byte(inString)):   {Comments: []domain.Comment{{Line: 1, Text: "// Banner."}}},
		factsKey([]byte(inTemplate)): {},
		factsKey([]byte(goString)):   {},
	}}
	analyzers := map[domain.Language]port.Analyzer{domain.LanguageGo: analyzer, domain.LanguageTypeScript: analyzer}
	source := &fakeSource{
		working: map[string]string{"web/a.ts": inString, "web/b.ts": inTemplate, "svc/a.go": goString},
		changed: map[string]domain.LineSet{"web/a.ts": {Ranges: []domain.LineRange{{First: 2, Last: 2}}}},
	}
	files := []domain.ChangedFile{modified("web/a.ts"), added("web/b.ts"), added("svc/a.go")}
	findings, err := NewHygiene(analyzers, source).Run(t.Context(), port.GateInput{Config: cfg, Changes: domain.ChangeSet{Base: "tree", Files: files}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var got []string
	for _, f := range findings {
		got = append(got, f.Rule+" "+f.File+":"+strconv.Itoa(f.Line))
	}
	// The doc comment above a.ts's line 2 is too short to be a reason.
	want := []string{"suppression-without-reason web/a.ts:2", "suppression-without-reason web/b.ts:2"}
	if !slices.Equal(got, want) {
		t.Fatalf("findings = %q, want %q", got, want)
	}
}

func sortedCopy(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)
	return out
}
