package buildtools

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"millennium/internal/domain"
)

func repoWith(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestGofmtAndVet(t *testing.T) {
	root := repoWith(t, map[string]string{
		"svc/go.mod":      "module svc\n\ngo 1.25\n",
		"svc/ok.go":       "package svc\n\nfunc OK() {}\n",
		"svc/messy.go":    "package svc\nfunc  Messy( ) {}\n",
		"svc/sub/vet.go":  "package sub\n\nimport \"fmt\"\n\nfunc V() { fmt.Printf(\"%d\", \"x\") }\n",
		"svc/sub/vet2.go": "package sub\n",
	})
	tools := New(root, "")
	unformatted, err := tools.Gofmt(t.Context(), []string{"svc/ok.go", "svc/messy.go"})
	if err != nil || !slices.Equal(unformatted, []string{"svc/messy.go"}) {
		t.Fatalf("Gofmt = %q, %v", unformatted, err)
	}
	issues, err := tools.GoVet(t.Context(), "svc")
	if err != nil {
		t.Fatalf("GoVet: %v", err)
	}
	if len(issues) != 1 || issues[0].File != "svc/sub/vet.go" || issues[0].Line != 5 || !strings.Contains(issues[0].Message, "Printf") {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestGoVetReportsCompileErrorsAsIssues(t *testing.T) {
	root := repoWith(t, map[string]string{
		"go.mod":    "module m\n\ngo 1.25\n",
		"broken.go": "package m\n\nfunc F() { undefinedThing() }\n",
	})
	issues, err := New(root, "").GoVet(t.Context(), "")
	if err != nil || len(issues) == 0 || issues[0].File != "broken.go" || issues[0].Line != 3 {
		t.Fatalf("issues = %+v, %v; want the compile error as an issue", issues, err)
	}
}

func typeScriptFixture(t *testing.T) (node, fixture string) {
	t.Helper()
	fixture = os.Getenv("MILLENNIUM_TYPESCRIPT")
	if fixture == "" {
		t.Skip("MILLENNIUM_TYPESCRIPT is not set; see AGENTS.md for the vetted TypeScript test fixture")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatalf("node is required: %v", err)
	}
	return node, fixture
}

func TestTypeCheckAndTSConfig(t *testing.T) {
	node, fixture := typeScriptFixture(t)
	root := repoWith(t, map[string]string{
		"tsconfig.base.json":    `{ "compilerOptions": { "strict": true } }`,
		"web/tsconfig.json":     `{ "extends": "../tsconfig.base.json", "compilerOptions": { "noEmit": true, "strictNullChecks": false }, "include": ["src"] }`,
		"web/src/ok.ts":         "export const n: number = 1;\n",
		"web/src/bad.ts":        "export const s: string = 1;\n",
		"web/package.json":      "{}",
		"other/tsconfig.json":   "{}",
		"other/src/x.ts":        "export {};\n",
		"lonely/tsconfig.json":  "{}",
		"lonely/package.json":   "{}",
		"lonely/src/nothing.ts": "export {};\n",
	})
	// Hoisted, as in a workspace: one typescript at the repository root.
	if err := os.Symlink(filepath.Join(fixture, "node_modules"), filepath.Join(root, "node_modules")); err != nil {
		t.Fatal(err)
	}
	tools := New(root, node)
	issues, err := tools.TypeCheck(t.Context(), "web")
	if err != nil {
		t.Fatalf("TypeCheck: %v", err)
	}
	if len(issues) != 1 || issues[0].File != "web/src/bad.ts" || issues[0].Line != 1 || issues[0].Code != "TS2322" {
		t.Fatalf("issues = %+v", issues)
	}
	config, err := tools.TSConfig(t.Context(), "web")
	if err != nil {
		t.Fatalf("TSConfig: %v", err)
	}
	problems, err := domain.TSConfigProblems(config)
	if err != nil || !slices.Equal(problems, []string{`"strictNullChecks" must not be switched off`}) {
		t.Fatalf("problems = %q, %v; want the weakened option found through extends", problems, err)
	}
}

func TestTypeCheckWithoutTypeScriptFailsClosed(t *testing.T) {
	root := repoWith(t, map[string]string{"web/tsconfig.json": "{}"})
	if _, err := New(root, "/usr/bin/node").TypeCheck(t.Context(), "web"); err == nil || !strings.Contains(err.Error(), "npm ci") {
		t.Fatalf("err = %v, want an install hint", err)
	}
	if _, err := New(root, "").TypeCheck(t.Context(), "web"); err == nil || !strings.Contains(err.Error(), "node was not found") {
		t.Fatalf("err = %v, want node missing", err)
	}
}

func TestParsers(t *testing.T) {
	tools := New("/repo", "")
	mypy := "pkg/a.py:7: error: Incompatible types in assignment  [assignment]\npkg/b.py:2:5: error: Name \"x\" is not defined  [name-defined]\nnote: something\n"
	got := tools.parse("svc", mypy, mypyIssue, func(m []string) domain.LintIssue {
		return domain.LintIssue{File: m[1], Line: atoi(m[2]), Code: m[4], Message: m[3]}
	})
	want := []domain.LintIssue{
		{File: "svc/pkg/a.py", Line: 7, Code: "assignment", Message: "Incompatible types in assignment"},
		{File: "svc/pkg/b.py", Line: 2, Code: "name-defined", Message: `Name "x" is not defined`},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("mypy issues = %+v\nwant           %+v", got, want)
	}
	if got := tools.fromDir("svc", "/repo/svc/x.go"); got != "svc/x.go" {
		t.Fatalf("absolute path mapped to %q", got)
	}
	for _, re := range []*regexp.Regexp{goIssue, tscIssue, mypyIssue} {
		if re.MatchString("random output line") {
			t.Fatalf("%v matched noise", re)
		}
	}
}
