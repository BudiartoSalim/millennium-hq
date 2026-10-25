package domain

import (
	"slices"
	"strings"
	"testing"
)

func TestRequiredTools(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want []string
	}{
		{"go", Config{Languages: []Language{LanguageGo}}, []string{"go", "gofmt"}},
		{"typescript", Config{Languages: []Language{LanguageTypeScript}}, []string{"node"}},
		{"python without type checking", Config{Languages: []Language{LanguagePython}, PythonTypeCheck: PythonTypeCheckOff}, []string{"python3|python|py"}},
		{"python with mypy", Config{Languages: []Language{LanguagePython}, PythonTypeCheck: PythonTypeCheckMypy}, []string{"python3|python|py", "mypy"}},
		{"all languages", Config{Languages: Languages()}, []string{"go", "gofmt", "node", "python3|python|py"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, req := range RequiredTools(tt.cfg) {
				if req.Reason == "" {
					t.Errorf("%v has no reason", req.Names)
				}
				got = append(got, strings.Join(req.Names, "|"))
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToolRequirementDescribe(t *testing.T) {
	tests := []struct {
		names []string
		want  string
	}{
		{[]string{"go"}, "go"},
		{[]string{"python3", "python"}, "python3 or python"},
		{[]string{"python3", "python", "py"}, "python3, python or py"},
	}
	for _, tt := range tests {
		if got := (ToolRequirement{Names: tt.names}).Describe(); got != tt.want {
			t.Errorf("Describe(%q) = %q, want %q", tt.names, got, tt.want)
		}
	}
}
