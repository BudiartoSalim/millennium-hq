package domain

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func minimalInput() ConfigInput {
	return ConfigInput{Version: ptr(1), Languages: []string{"go"}}
}

func TestNewConfigAppliesDefaults(t *testing.T) {
	cfg, err := NewConfig(minimalInput())
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	want := Config{
		Languages:        []Language{LanguageGo},
		Conventions:      []Convention{},
		Testing:          DefaultTesting,
		DependencyPolicy: DefaultDependencyPolicy,
		CI:               DefaultCI,
		PythonTypeCheck:  DefaultPythonTypeCheck,
		AcceptanceTests:  DefaultAcceptanceTests,
	}
	if !equalConfig(cfg, want) {
		t.Fatalf("got %+v, want %+v", cfg, want)
	}
}

func TestNewConfigAcceptsFullConfig(t *testing.T) {
	in := ConfigInput{
		Version:          ptr(1),
		Languages:        []string{"go", "typescript", "python"},
		Conventions:      []string{"clean-architecture", "react-feature-based"},
		Testing:          ptr("tests-required"),
		DependencyPolicy: ptr("strict"),
		CI:               ptr("none"),
		PythonTypeCheck:  ptr("mypy"),
		AcceptanceTests:  ptr("qa/acceptance-tests"),
		Run:              &RunRecipe{Start: "make run", HealthURL: "http://127.0.0.1:8080/health"},
	}
	cfg, err := NewConfig(in)
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if cfg.CI != CINone || cfg.PythonTypeCheck != PythonTypeCheckMypy || cfg.AcceptanceTests != "qa/acceptance-tests" {
		t.Fatalf("explicit values not kept: %+v", cfg)
	}
	if cfg.Run == nil || cfg.Run.HealthURL != "http://127.0.0.1:8080/health" {
		t.Fatalf("run recipe not kept: %+v", cfg.Run)
	}
}

func TestNewConfigRejects(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*ConfigInput)
		want   string
	}{
		{"missing version", func(in *ConfigInput) { in.Version = nil }, "version: required"},
		{"future version", func(in *ConfigInput) { in.Version = ptr(2) }, "version: 2 is not supported"},
		{"no languages", func(in *ConfigInput) { in.Languages = nil }, "at least one language"},
		{"unknown language", func(in *ConfigInput) { in.Languages = []string{"cobol"} }, `languages: "cobol" is not one of`},
		{"duplicate language", func(in *ConfigInput) { in.Languages = []string{"go", "go"} }, `"go" is listed more than once`},
		{"unknown convention", func(in *ConfigInput) { in.Conventions = []string{"mvc"} }, `conventions: "mvc"`},
		{"explicit empty testing", func(in *ConfigInput) { in.Testing = ptr("") }, `testing: "" is not one of`},
		{"unknown ci", func(in *ConfigInput) { in.CI = ptr("jenkins") }, `ci: "jenkins"`},
		{"unknown dependency policy", func(in *ConfigInput) { in.DependencyPolicy = ptr("lax") }, `dependencyPolicy: "lax"`},
		{"react without typescript", func(in *ConfigInput) { in.Conventions = []string{"react-feature-based"} }, `needs "typescript"`},
		{"mypy without python", func(in *ConfigInput) { in.PythonTypeCheck = ptr("mypy") }, `needs "python"`},
		{"run without start", func(in *ConfigInput) {
			in.Run = &RunRecipe{HealthURL: "http://localhost/health"}
		}, "run.start: required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := minimalInput()
			tt.modify(&in)
			assertConfigError(t, in, tt.want)
		})
	}
}

func TestNewConfigRejectsUnsafeAcceptancePaths(t *testing.T) {
	for _, p := range []string{"", "../outside", "/abs/path", "a/./b", "a/../b", `a\b`, "a//b", "a/", ".", "with space"} {
		t.Run(p, func(t *testing.T) {
			in := minimalInput()
			in.AcceptanceTests = ptr(p)
			assertConfigError(t, in, "paths.acceptanceTests")
		})
	}
}

func TestNewConfigHealthURL(t *testing.T) {
	tests := []struct {
		url string
		ok  bool
	}{
		{"http://127.0.0.1:8080/health", true},
		{"https://localhost/health", true},
		{"http://[::1]:9000/", true},
		{"http://127.1.2.3/", true},
		{"http://example.com/health", false},
		{"http://10.0.0.5/health", false},
		{"ftp://127.0.0.1/", false},
		{"http://user:pass@127.0.0.1/", false},
		{"localhost:8080", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			in := minimalInput()
			in.Run = &RunRecipe{Start: "run", HealthURL: tt.url}
			_, err := NewConfig(in)
			if tt.ok && err != nil {
				t.Fatalf("want accepted, got %v", err)
			}
			if !tt.ok {
				assertConfigError(t, in, "run.healthUrl")
			}
		})
	}
}

func TestNewConfigReportsEveryProblem(t *testing.T) {
	in := ConfigInput{Languages: []string{"cobol"}, CI: ptr("jenkins")}
	_, err := NewConfig(in)
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("want *ConfigError, got %v", err)
	}
	if len(cfgErr.Problems) != 3 {
		t.Fatalf("want 3 problems (version, language, ci), got %q", cfgErr.Problems)
	}
}

func assertConfigError(t *testing.T, in ConfigInput, want string) {
	t.Helper()
	_, err := NewConfig(in)
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("want *ConfigError containing %q, got %v", want, err)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not mention %q", err, want)
	}
}

func equalConfig(a, b Config) bool {
	return slices.Equal(a.Languages, b.Languages) &&
		slices.Equal(a.Conventions, b.Conventions) &&
		a.Testing == b.Testing &&
		a.DependencyPolicy == b.DependencyPolicy &&
		a.CI == b.CI &&
		a.PythonTypeCheck == b.PythonTypeCheck &&
		a.AcceptanceTests == b.AcceptanceTests &&
		(a.Run == nil) == (b.Run == nil)
}
