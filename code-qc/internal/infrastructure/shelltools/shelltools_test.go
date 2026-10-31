package shelltools

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func requireTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"bash", "shellcheck"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("%s is required for these tests: %v", tool, err)
		}
	}
}

func scripts(t *testing.T, files map[string]string) *Tools {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return New(root)
}

func TestSyntax(t *testing.T) {
	requireTools(t)
	tools := scripts(t, map[string]string{
		"ok.sh":  "#!/bin/bash\nif true; then echo hi; fi\n",
		"bad.sh": "#!/bin/bash\nif true; then\necho hi\n",
	})
	if issues, err := tools.Syntax(t.Context(), "ok.sh"); err != nil || len(issues) != 0 {
		t.Fatalf("ok.sh: %+v, %v", issues, err)
	}
	issues, err := tools.Syntax(t.Context(), "bad.sh")
	if err != nil || len(issues) == 0 || issues[0].Code != "syntax" || issues[0].Line == 0 {
		t.Fatalf("bad.sh: %+v, %v; want a syntax issue with a line", issues, err)
	}
}

func TestLint(t *testing.T) {
	requireTools(t)
	tools := scripts(t, map[string]string{
		"lib.sh":        "greet() { echo \"$1\"; }\n",
		"unquoted.sh":   "#!/bin/bash\nf=$1\nrm $f\n",
		"clean.sh":      "#!/bin/bash\n# shellcheck source=lib.sh\nsource ./lib.sh\ngreet \"x\"\n",
		".shellcheckrc": "disable=all\n",
	})
	issues, err := tools.Lint(t.Context(), "unquoted.sh", "")
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if len(issues) != 1 || issues[0].Code != "SC2086" || issues[0].Line != 3 || !strings.Contains(issues[0].Message, "Double quote") {
		t.Fatalf("issues = %+v; want SC2086 on line 3 despite the .shellcheckrc that disables everything", issues)
	}
	if issues, err := tools.Lint(t.Context(), "clean.sh", ""); err != nil || len(issues) != 0 {
		t.Fatalf("clean.sh: %+v, %v", issues, err)
	}
	if issues, err := tools.Lint(t.Context(), "lib.sh", "bash"); err != nil || len(issues) != 0 {
		t.Fatalf("lib.sh without shebang, linted as bash: %+v, %v", issues, err)
	}
}

func TestSyntaxIssuesWithoutALineNumber(t *testing.T) {
	issues := syntaxIssues("bash: something odd\n")
	if len(issues) != 1 || issues[0].Line != 0 || issues[0].Message != "bash: something odd" {
		t.Fatalf("issues = %+v", issues)
	}
}
