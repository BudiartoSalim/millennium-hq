package domain

import (
	"strings"
	"testing"
)

// These tests drive the edge branches the full-tier self-audit found
// unexercised.

func TestShellDialectEdges(t *testing.T) {
	for _, head := range []string{"#!\n", "#!/usr/bin/env -S\n", "#!/usr/bin/env\n"} {
		if _, ok := ShellDialect([]byte(head)); ok {
			t.Errorf("ShellDialect(%q) found a shell", head)
		}
	}
}

func TestConstructAdviceCoversEveryKind(t *testing.T) {
	for _, kind := range ConstructKinds() {
		if advice := constructAdvice(kind); advice == "risky construct" || advice == "" {
			t.Errorf("%s has no specific advice", kind)
		}
	}
	if constructAdvice("unknown-kind") != "risky construct" {
		t.Fatal("an unknown kind gets the generic advice")
	}
}

func TestCheckImportsOfTheCompositionRoot(t *testing.T) {
	l := goLayering(t, "", nil)
	imp := projectImport("m/cmd/tool", "cmd/tool")
	findings := l.CheckImports(FileFacts{Path: "internal/domain/a.go", Imports: []Import{imp}}, nil)
	if len(findings) != 1 || !strings.Contains(findings[0].Message, "must not import the composition root") {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestPatternsOnTheEmptyPath(t *testing.T) {
	if root, ok := RootPattern().MatchPrefix(""); !ok || root != "" {
		t.Fatalf("the root pattern must match the root itself: %q, %v", root, ok)
	}
	if _, ok := mustPattern(t, "services", PrefixPattern).MatchPrefix(""); ok {
		t.Fatal("a non-root pattern must not match the empty path")
	}
}

func TestTestingHelpersForLanguagesWithoutTestFiles(t *testing.T) {
	if IsTestPath(LanguageShell, "tests/x.sh") || IsRelatedTest(LanguageShell, "a.sh", "tests/test_a.sh") {
		t.Fatal("shell has no test-file convention of its own")
	}
	if IsRelatedTest(Language("cobol"), "a", "a_test") {
		t.Fatal("an unknown language has no related tests")
	}
}

func TestRequiredToolsOfAnUnknownLanguage(t *testing.T) {
	if tools := RequiredTools(Config{Languages: []Language{"cobol"}}); len(tools) != 0 {
		t.Fatalf("tools = %+v", tools)
	}
}
