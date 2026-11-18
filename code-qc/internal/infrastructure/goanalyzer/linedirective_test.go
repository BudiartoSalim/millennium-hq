package goanalyzer

import (
	"slices"
	"testing"

	"millennium-code-qc/internal/domain"
)

// The file name of a directive is read as go/scanner reads it, since the
// coverage tool credits the code after it to that name (R2-SEC-1).
func TestDirectiveFile(t *testing.T) {
	tests := []struct{ directive, want string }{
		{"//line real.go:5", "real.go"},
		{"//line real.go:5:1", "real.go"},
		{"/*line real.go:5*/", "real.go"},
		{"/*line real.go:5:3*/", "real.go"},
		{"//line ../q/other.go:3", "../q/other.go"},
		{`//line C:\src\real.go:5`, `C:\src\real.go`},
		{"//line yaccpar:1", "yaccpar"},
		{"//line :5", ""},
		{"//line :5:1", ""},
		{"//line 5:3", "5"},
		{"//line real.go", ""},
		{"//line real.go:x", ""},
		{"//line real.go:5*/", ""},
	}
	for _, tt := range tests {
		if got := directiveFile(tt.directive); got != tt.want {
			t.Errorf("directiveFile(%q) = %q, want %q", tt.directive, got, tt.want)
		}
	}
}

func TestLineDirectivesOfSourceThatDoesNotParse(t *testing.T) {
	src := "package a\n\nfunc {\n//line real.go:5\n\"unterminated\n/*line b.go:1*/"
	want := []domain.LineDirective{
		{Line: 4, Text: "//line real.go:5", File: "real.go"},
		{Line: 6, Text: "/*line b.go:1*/", File: "b.go"},
	}
	if got := LineDirectives([]byte(src)); !slices.Equal(got, want) {
		t.Fatalf("LineDirectives = %+v, want %+v", got, want)
	}
	if got := LineDirectives(nil); len(got) != 0 {
		t.Fatalf("empty source: %+v", got)
	}
}
