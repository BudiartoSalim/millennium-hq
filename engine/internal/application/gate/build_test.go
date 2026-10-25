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

type fakeBuildTools struct {
	unformatted []string
	vet         map[string][]domain.LintIssue
	tsc         map[string][]domain.LintIssue
	tsconfig    map[string]string
	mypy        []domain.LintIssue
	err         error
	calls       []string
}

func (f *fakeBuildTools) Gofmt(_ context.Context, files []string) ([]string, error) {
	f.calls = append(f.calls, "gofmt "+strings.Join(files, ","))
	return f.unformatted, f.err
}

func (f *fakeBuildTools) GoVet(_ context.Context, dir string) ([]domain.LintIssue, error) {
	f.calls = append(f.calls, "vet "+dir)
	return f.vet[dir], f.err
}

func (f *fakeBuildTools) TypeCheck(_ context.Context, dir string) ([]domain.LintIssue, error) {
	f.calls = append(f.calls, "tsc "+dir)
	return f.tsc[dir], f.err
}

func (f *fakeBuildTools) TSConfig(_ context.Context, dir string) ([]byte, error) {
	if config, ok := f.tsconfig[dir]; ok {
		return []byte(config), nil
	}
	return []byte(`{"compilerOptions":{"strict":true}}`), nil
}

func (f *fakeBuildTools) Mypy(_ context.Context, dir string, files []string) ([]domain.LintIssue, error) {
	f.calls = append(f.calls, "mypy "+dir+" "+strings.Join(files, ","))
	return f.mypy, f.err
}

// fakeFinder knows which directories hold which marker files.
type fakeFinder map[string]string

func (f fakeFinder) NearestUp(from, marker, within string) (string, bool) {
	for dir := from; domain.Within(dir, within); dir = parentOf(dir) {
		if f[dir] == marker {
			return dir, true
		}
		if dir == "" {
			break
		}
	}
	return "", false
}

func parentOf(dir string) string {
	if i := strings.LastIndex(dir, "/"); i >= 0 {
		return dir[:i]
	}
	return ""
}

func buildConfig(t *testing.T, mypy bool) domain.Config {
	t.Helper()
	in := domain.ConfigInput{Version: ptr(1), Components: []domain.ComponentInput{
		{Paths: []string{"services/*/api"}, Languages: []string{"go"}},
		{Paths: []string{"web"}, Languages: []string{"typescript"}},
		{Paths: []string{"tts"}, Languages: []string{"python"}},
	}}
	if mypy {
		in.PythonTypeCheck = ptr("mypy")
	}
	cfg, err := domain.NewConfig(in)
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	return cfg
}

func runBuild(t *testing.T, cfg domain.Config, tools *fakeBuildTools, files ...domain.ChangedFile) []domain.Finding {
	t.Helper()
	finder := fakeFinder{"services/game/api": "go.mod", "web": "tsconfig.json"}
	findings, err := NewBuild(tools, finder).Run(t.Context(), port.GateInput{Config: cfg, Changes: domain.ChangeSet{Files: files}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return findings
}

func TestBuildRunsEachToolOncePerModule(t *testing.T) {
	tools := &fakeBuildTools{
		unformatted: []string{"services/game/api/b.go"},
		vet:         map[string][]domain.LintIssue{"services/game/api": {{File: "services/game/api/c.go", Line: 4, Code: "vet", Message: "Printf arg"}}},
		tsc:         map[string][]domain.LintIssue{"web": {{File: "web/src/a.ts", Line: 1, Code: "TS2322", Message: "Type 'number'"}}},
		tsconfig:    map[string]string{"web": `{"compilerOptions":{"strict":true,"strictNullChecks":false}}`},
	}
	findings := runBuild(t, buildConfig(t, false), tools,
		modified("services/game/api/a.go"), added("services/game/api/b.go"), modified("web/src/a.ts"),
		modified("tts/x.py"), domain.ChangedFile{Path: "services/game/api/gone.go", Kind: domain.ChangeDeleted})
	if want := []string{"gofmt services/game/api/a.go,services/game/api/b.go", "vet services/game/api", "tsc web"}; !slices.Equal(tools.calls, want) {
		t.Fatalf("calls = %q, want %q (python without mypy runs nothing)", tools.calls, want)
	}
	if got := rules(findings); !slices.Equal(got, []string{"build/gofmt", "build/vet", "build/type-error", "build/tsconfig-weakened"}) {
		t.Fatalf("findings = %q", got)
	}
	if !strings.HasPrefix(findings[2].Message, "TS2322: ") {
		t.Fatalf("tsc finding message = %q", findings[2].Message)
	}
}

func TestBuildMypyAndMissingTSConfig(t *testing.T) {
	tools := &fakeBuildTools{mypy: []domain.LintIssue{{File: "tts/x.py", Line: 3, Code: "assignment", Message: "bad"}}}
	findings := runBuild(t, buildConfig(t, true), tools, modified("tts/x.py"), modified("web2/a.ts"))
	if want := []string{"mypy tts tts/x.py"}; !slices.Equal(tools.calls, want) {
		t.Fatalf("calls = %q", tools.calls)
	}
	if got := rules(findings); !slices.Equal(got, []string{"build/type-error"}) {
		t.Fatalf("findings = %q", got)
	}
	cfg, err := domain.NewConfig(domain.ConfigInput{Version: ptr(1), Languages: []string{"typescript"}})
	if err != nil {
		t.Fatal(err)
	}
	missing := runBuild(t, cfg, &fakeBuildTools{}, modified("lib/a.ts"))
	if got := rules(missing); !slices.Equal(got, []string{"build/tsconfig-missing"}) {
		t.Fatalf("a TypeScript file without tsconfig must block, got %q", got)
	}
}

func TestBuildToolFailureBlocks(t *testing.T) {
	tools := &fakeBuildTools{err: errors.New("go: not found")}
	findings := runBuild(t, buildConfig(t, false), tools, modified("services/game/api/a.go"))
	if got := rules(findings); !slices.Equal(got, []string{"build/tool-failed"}) {
		t.Fatalf("findings = %q", got)
	}
}
