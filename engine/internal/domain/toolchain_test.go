package domain

import (
	"slices"
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
		{"python without type checking", Config{Languages: []Language{LanguagePython}, PythonTypeCheck: PythonTypeCheckOff}, []string{"python3"}},
		{"python with mypy", Config{Languages: []Language{LanguagePython}, PythonTypeCheck: PythonTypeCheckMypy}, []string{"python3", "mypy"}},
		{"all languages", Config{Languages: Languages()}, []string{"go", "gofmt", "node", "python3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, req := range RequiredTools(tt.cfg) {
				if req.Reason == "" {
					t.Errorf("%s has no reason", req.Name)
				}
				got = append(got, req.Name)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
