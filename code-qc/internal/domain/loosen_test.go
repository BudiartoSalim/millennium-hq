package domain

import (
	"slices"
	"strings"
	"testing"
)

func mustConfig(t *testing.T, in ConfigInput) Config {
	t.Helper()
	cfg, err := NewConfig(in)
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	return cfg
}

func TestLoosenings(t *testing.T) {
	recipe := &TestRecipe{Command: []string{"npm", "test"}, LCOV: "lcov.info"}
	current := mustConfig(t, ConfigInput{Version: ptr(1), PythonTypeCheck: ptr("mypy"), Components: []ComponentInput{
		{Paths: []string{"services/*/api"}, Languages: []string{"go", "shell"}, Conventions: []string{"clean-architecture"}},
		{Paths: []string{"tts"}, Languages: []string{"python"}, Conventions: []string{"clean-architecture"}},
		{Paths: []string{"web"}, Languages: []string{"typescript"}, Test: recipe},
	}})
	next := mustConfig(t, ConfigInput{Version: ptr(1), ShellLint: ptr("off"), AcceptanceTests: ptr("qa"),
		Exclude: []string{"gen"}, Disable: []DisabledCheck{{Check: "structure/function-length", Reason: "upstream style is long"}},
		Components: []ComponentInput{
			{Paths: []string{"services/*/api"}, Languages: []string{"go"}},
			{Paths: []string{"web"}, Languages: []string{"typescript"}},
		}})
	got := Loosenings(&current, next)
	want := []string{
		"language python is no longer checked",
		"language shell is no longer checked",
		"component services/*/api drops convention clean-architecture",
		"component tts is removed",
		"component web drops its test recipe",
		"gen is newly excluded from every check",
		"structure/function-length is newly disabled: upstream style is long",
		"Python type checking (mypy) is switched off",
		"shellcheck is switched off",
		"acceptance tests move from tests/acceptance to qa, so the old ones lose their protection",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("loosenings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestTighteningIsNotALoosening(t *testing.T) {
	current := mustConfig(t, ConfigInput{Version: ptr(1), Languages: []string{"go"}, ShellLint: ptr("off")})
	next := mustConfig(t, ConfigInput{Version: ptr(1), Languages: []string{"go", "python"}, Conventions: []string{"clean-architecture"}, PythonTypeCheck: ptr("mypy")})
	if got := Loosenings(&current, next); len(got) != 0 {
		t.Fatalf("tightening reported as loosening: %q", got)
	}
	if got := Loosenings(nil, next); got != nil {
		t.Fatalf("a first config loosens nothing: %q", got)
	}
}

func TestComponentPathAndLayerChanges(t *testing.T) {
	current := mustConfig(t, ConfigInput{Version: ptr(1), Components: []ComponentInput{{Paths: []string{"a", "b"}, Languages: []string{"go"}}}})
	next := mustConfig(t, ConfigInput{Version: ptr(1), Components: []ComponentInput{{Paths: []string{"a"}, Languages: []string{"go"},
		Layers: map[string][]string{"domain": {"core"}}}}})
	got := Loosenings(&current, next)
	if len(got) != 2 || !strings.Contains(got[0], "now covers [a] instead of [a b]") || !strings.Contains(got[1], "changes its layer paths") {
		t.Fatalf("loosenings = %q", got)
	}
}

func TestALanguageDroppedFromOneComponentOnly(t *testing.T) {
	current := mustConfig(t, ConfigInput{Version: ptr(1), Components: []ComponentInput{
		{Paths: []string{"a"}, Languages: []string{"go", "shell"}}, {Paths: []string{"b"}, Languages: []string{"shell"}},
	}})
	next := mustConfig(t, ConfigInput{Version: ptr(1), Components: []ComponentInput{
		{Paths: []string{"a"}, Languages: []string{"go"}}, {Paths: []string{"b"}, Languages: []string{"shell"}},
	}})
	if got := Loosenings(&current, next); !slices.Equal(got, []string{"component a no longer checks shell"}) {
		t.Fatalf("loosenings = %q", got)
	}
}
