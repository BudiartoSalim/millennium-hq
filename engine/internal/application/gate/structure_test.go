package gate

import (
	"context"
	"errors"
	"strings"
	"testing"

	"millennium/internal/application/port"
	"millennium/internal/domain"
)

// fakeAnalyzer returns canned facts keyed by file content, so a test can give
// the working and base versions different facts.
type fakeAnalyzer struct {
	facts map[string]domain.FileFacts
}

func (f fakeAnalyzer) Analyze(_ context.Context, path string, content []byte) (domain.FileFacts, error) {
	facts, ok := f.facts[string(content)]
	if !ok {
		return domain.FileFacts{}, errors.New("parse " + path + ": syntax error")
	}
	facts.Path = path
	return facts, nil
}

type fakeSource struct {
	working    map[string]string
	base       map[string]string
	changed    map[string]domain.LineSet
	readAtErr  error
	changedErr error
	gotRev     string
}

func (f *fakeSource) ReadWorking(path string) ([]byte, error) {
	content, ok := f.working[path]
	if !ok {
		return nil, errors.New(path + " is not a regular file")
	}
	return []byte(content), nil
}

func (f *fakeSource) ReadAt(_ context.Context, rev, path string) ([]byte, bool, error) {
	f.gotRev = rev
	content, ok := f.base[path]
	return []byte(content), ok, f.readAtErr
}

func (f *fakeSource) ChangedLines(_ context.Context, rev, path string) (domain.LineSet, error) {
	f.gotRev = rev
	if f.changedErr != nil {
		return domain.LineSet{}, f.changedErr
	}
	if lines, ok := f.changed[path]; ok {
		return lines, nil
	}
	return domain.LineSet{}, nil
}

type fakePacks struct{}

func (fakePacks) Pack(name domain.Convention) (domain.Pack, error) {
	if name != domain.ConventionCleanArchitecture {
		return domain.Pack{}, port.ErrPackUnavailable
	}
	return domain.NewPack(domain.PackInput{
		Name:      "clean-architecture",
		Languages: []string{"go", "typescript", "python"},
		Layers:    []domain.PackLayer{{Name: "domain"}, {Name: "infrastructure", MayImport: []string{"domain"}, ThirdParty: true}},
		Paths:     map[string]map[string][]string{"go": {"domain": {"internal/domain"}, "infrastructure": {"internal/infrastructure"}}},
	})
}

func structureConfig(t *testing.T) domain.Config {
	t.Helper()
	cfg, err := domain.NewConfig(domain.ConfigInput{Version: ptr(1), Components: []domain.ComponentInput{
		{Paths: []string{"svc"}, Languages: []string{"go", "typescript"}, Conventions: []string{"clean-architecture"}},
		{Paths: []string{"legacy"}, Languages: []string{"go"}},
		{Paths: []string{"web"}, Languages: []string{"typescript"}, Conventions: []string{"react-feature-based"}},
		{Paths: []string{"x/*"}, Languages: []string{"go"}},
		{Paths: []string{"*/y"}, Languages: []string{"go"}},
	}})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	return cfg
}

func ptr[T any](v T) *T { return &v }

var (
	cleanFile = domain.FileFacts{Functions: []domain.FunctionFacts{{Key: "ok", Lines: 10}}}
	longFile  = domain.FileFacts{Functions: []domain.FunctionFacts{{Key: "long", Line: 4, Lines: 80}}}
	badImport = domain.FileFacts{Imports: []domain.Import{{Path: "github.com/x/y", Line: 3, Kind: domain.ImportExternal}}}
)

func runStructure(t *testing.T, source *fakeSource, base string, files ...domain.ChangedFile) []domain.Finding {
	t.Helper()
	analyzers := map[domain.Language]port.Analyzer{domain.LanguageGo: fakeAnalyzer{facts: map[string]domain.FileFacts{
		"clean": cleanFile, "long": longFile, "badimport": badImport,
	}}}
	g := NewStructure(analyzers, fakePacks{}, source)
	in := port.GateInput{Config: structureConfig(t), Changes: domain.ChangeSet{Base: base, Files: files}}
	findings, err := g.Run(t.Context(), in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return findings
}

func modified(path string) domain.ChangedFile {
	return domain.ChangedFile{Path: path, Kind: domain.ChangeModified}
}

func TestStructureFindings(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		content string
		want    string
	}{
		{"clean file", "svc/internal/domain/a.go", "clean", ""},
		{"limit exceeded", "svc/internal/domain/a.go", "long", "function-length"},
		{"layer rule broken", "svc/internal/domain/a.go", "badimport", "layer-import"},
		{"no convention still gets limits", "legacy/a.go", "long", "function-length"},
		{"no convention means no layer rules", "legacy/a.go", "badimport", ""},
		{"outside every component", "scripts/a.go", "long", ""},
		{"language not in the component", "web/a.go", "long", ""},
		{"not a source file", "svc/README.md", "long", ""},
		{"unparsable file", "svc/internal/domain/a.go", "garbage", "analysis-failed"},
		{"ambiguous component", "x/y/a.go", "clean", "component-ambiguous"},
		{"analyzer missing for a language", "svc/src/a.ts", "clean", "analysis-unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := &fakeSource{working: map[string]string{tt.path: tt.content}}
			findings := runStructure(t, source, "", modified(tt.path))
			switch {
			case tt.want == "" && len(findings) > 0:
				t.Fatalf("want no findings, got %+v", findings)
			case tt.want != "" && (len(findings) != 1 || findings[0].Rule != tt.want || findings[0].Gate != "structure"):
				t.Fatalf("want one structure/%s finding, got %+v", tt.want, findings)
			}
		})
	}
}

func TestStructureUnavailableConventionBlocksOncePerComponent(t *testing.T) {
	cfg, err := domain.NewConfig(domain.ConfigInput{Version: ptr(1), Components: []domain.ComponentInput{
		{Paths: []string{"web"}, Languages: []string{"go", "typescript"}, Conventions: []string{"react-feature-based"}},
	}})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	source := &fakeSource{working: map[string]string{"web/a.go": "clean", "web/b.go": "clean"}}
	analyzers := map[domain.Language]port.Analyzer{domain.LanguageGo: fakeAnalyzer{facts: map[string]domain.FileFacts{"clean": cleanFile}}}
	in := port.GateInput{Config: cfg, Changes: domain.ChangeSet{Files: []domain.ChangedFile{modified("web/a.go"), modified("web/b.go")}}}
	findings, err := NewStructure(analyzers, fakePacks{}, source).Run(t.Context(), in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 1 || findings[0].Rule != "convention-unavailable" ||
		!strings.Contains(findings[0].Message, "convention react-feature-based is not available") {
		t.Fatalf("want one convention-unavailable finding, got %+v", findings)
	}
}

func TestStructureComparesModifiedFilesWithTheirBase(t *testing.T) {
	source := &fakeSource{
		working: map[string]string{"svc/internal/domain/a.go": "long"},
		base:    map[string]string{"svc/internal/domain/a.go": "long"},
	}
	if findings := runStructure(t, source, "tree123", modified("svc/internal/domain/a.go")); len(findings) != 0 {
		t.Fatalf("an unchanged over-limit function may stay, got %+v", findings)
	}
	if source.gotRev != "tree123" {
		t.Fatalf("base read at %q, want tree123", source.gotRev)
	}
	added := domain.ChangedFile{Path: "svc/internal/domain/a.go", Kind: domain.ChangeAdded}
	if findings := runStructure(t, source, "tree123", added); len(findings) != 1 {
		t.Fatalf("an added file is new code, got %+v", findings)
	}
	if findings := runStructure(t, source, "", modified("svc/internal/domain/a.go")); len(findings) != 1 {
		t.Fatalf("without a base (audit) the full rules apply, got %+v", findings)
	}
	source.base["svc/internal/domain/a.go"] = "garbage"
	if findings := runStructure(t, source, "tree123", modified("svc/internal/domain/a.go")); len(findings) != 1 {
		t.Fatalf("an unparsable base counts as absent, got %+v", findings)
	}
}

func TestStructureSkipsDeletedFilesAndReportsUnreadableOnes(t *testing.T) {
	deleted := domain.ChangedFile{Path: "svc/internal/domain/gone.go", Kind: domain.ChangeDeleted}
	if findings := runStructure(t, &fakeSource{}, "", deleted); len(findings) != 0 {
		t.Fatalf("deleted files are not analysed, got %+v", findings)
	}
	findings := runStructure(t, &fakeSource{}, "", modified("svc/internal/domain/link.go"))
	if len(findings) != 1 || findings[0].Rule != "analysis-failed" || !strings.Contains(findings[0].Message, "not a regular file") {
		t.Fatalf("an unreadable file must block, got %+v", findings)
	}
}
