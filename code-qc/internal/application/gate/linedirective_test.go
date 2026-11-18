package gate

import (
	"slices"
	"testing"

	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/domain"
)

// A //line directive in a tested layer moves coverage off the lines it
// covers, so the tests gate blocks it there, touched or not, in every tier
// (SEC-4). Outside tested layers it is generated code's business.
func TestTestsBlocksLineDirectivesInTestedLayers(t *testing.T) {
	directive := []domain.LineDirective{{Line: 3, Text: "//line user.go:5000", File: "user.go"}}
	withDirective := logic
	withDirective.LineDirectives = directive
	analyzer := fakeAnalyzer{facts: map[string]domain.FileFacts{
		"directive":      withDirective,
		"type directive": {LineDirectives: directive},
		"test directive": {Test: true, LineDirectives: directive},
		"clean":          logic,
	}}
	tests := []struct {
		name, path, content string
		want                []string
	}{
		{"production file", userFile, "directive", []string{userFile}},
		{"file without functions", "svc/internal/domain/types.go", "type directive", []string{"svc/internal/domain/types.go"}},
		{"test file", "svc/internal/domain/user_test.go", "test directive", []string{"svc/internal/domain/user_test.go"}},
		{"untested layer", "svc/internal/infrastructure/db.go", "directive", nil},
		{"no directive", userFile, "clean", nil},
		{"unreadable file", "svc/internal/domain/gone.go", "", nil},
		{"file that does not parse", userFile, "does not parse", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			working := map[string]string{tt.path: tt.content}
			if tt.content == "" {
				working = nil
			}
			checkDirectives(t, analyzer, working, tt.path, tt.want)
		})
	}
}

// checkDirectives runs the tests gate in each tier on a change to path,
// alongside a changed test that keeps tests-missing out of the way, and
// compares the files with line-directive findings.
func checkDirectives(t *testing.T, analyzer port.Analyzer, working map[string]string, path string, want []string) {
	t.Helper()
	for _, tier := range domain.Tiers() {
		source := &fakeSource{working: working, changed: map[string]domain.LineSet{}}
		g := NewTests(map[domain.Language]port.Analyzer{domain.LanguageGo: analyzer}, source, &fakeRunner{}, fakeFinder{"svc": "go.mod"}, &fakeForeign{})
		files := []domain.ChangedFile{modified(path), modified("svc/internal/domain/other_test.go")}
		findings, err := g.Run(t.Context(), port.GateInput{Config: testsConfig(t), Changes: domain.ChangeSet{Base: "base-rev", Files: files}, Tier: tier})
		if err != nil {
			t.Fatalf("%s: Run: %v", tier, err)
		}
		if got := directiveFiles(findings); !slices.Equal(got, want) {
			t.Fatalf("%s: line-directive findings for %q, want %q; all findings %+v", tier, got, want, findings)
		}
	}
}

func directiveFiles(findings []domain.Finding) []string {
	var files []string
	for _, f := range findings {
		if f.Gate == "tests" && f.Rule == "line-directive" {
			files = append(files, f.File)
		}
	}
	return files
}
