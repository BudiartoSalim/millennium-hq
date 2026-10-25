package helperanalyzer

import (
	"os"
	"os/exec"
	"path/filepath"
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

func run(t *testing.T, a *Analyzer, path, src string) domain.FileFacts {
	t.Helper()
	facts, err := a.Analyze(t.Context(), path, []byte(src))
	if err != nil {
		t.Fatalf("Analyze(%s): %v", path, err)
	}
	return facts
}

func functionByKey(t *testing.T, facts domain.FileFacts, key string) domain.FunctionFacts {
	t.Helper()
	for _, fn := range facts.Functions {
		if fn.Key == key {
			return fn
		}
	}
	t.Fatalf("no function %q in %+v", key, facts.Functions)
	return domain.FunctionFacts{}
}

func constructKinds(facts domain.FileFacts) []string {
	var kinds []string
	for _, c := range facts.Constructs {
		kinds = append(kinds, string(c.Kind)+":"+c.Detail)
	}
	return kinds
}

func pythonCommand(t *testing.T) []string {
	t.Helper()
	path, err := exec.LookPath("python3")
	if err != nil {
		t.Fatalf("python3 is required for these tests: %v", err)
	}
	return []string{path}
}

// typeScriptFixture returns a directory holding node_modules/typescript (a
// classic-API TypeScript, 6 or older), from MILLENNIUM_TYPESCRIPT. The
// package is not a dependency of this repository; scripts/verify.sh refuses
// to report success without it, so a skip here never passes as tested.
func typeScriptFixture(t *testing.T) (node, fixture string) {
	t.Helper()
	fixture = os.Getenv("MILLENNIUM_TYPESCRIPT")
	if fixture == "" {
		t.Skip("MILLENNIUM_TYPESCRIPT is not set; see AGENTS.md for the vetted TypeScript test fixture")
	}
	if _, err := os.Stat(filepath.Join(fixture, "node_modules", "typescript", "package.json")); err != nil {
		t.Fatalf("MILLENNIUM_TYPESCRIPT=%s has no node_modules/typescript: %v", fixture, err)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatalf("node is required for these tests: %v", err)
	}
	return node, fixture
}

// linkTypeScript gives a test project the fixture's typescript package, so
// the helper resolves it the way it resolves a real project's.
func linkTypeScript(t *testing.T, project, fixture string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(project, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(fixture, "node_modules", "typescript"), filepath.Join(project, "node_modules", "typescript")); err != nil {
		t.Fatal(err)
	}
}
