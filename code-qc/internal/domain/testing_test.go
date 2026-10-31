package domain

import (
	"slices"
	"testing"
)

func TestCoverageUncoveredAndMerge(t *testing.T) {
	cov := Coverage{"a.go": {1: 1, 2: 0, 3: 0, 5: 0, 9: 2}}
	cov.Merge(Coverage{"a.go": {3: 4}, "b.go": {1: 0}})
	got := cov["a.go"].Uncovered(AllLines())
	if want := []LineRange{{First: 2, Last: 2}, {First: 5, Last: 5}}; !slices.Equal(got, want) {
		t.Fatalf("uncovered = %+v, want %+v (line 3 merged as covered)", got, want)
	}
	touched := LineSet{Ranges: []LineRange{{First: 4, Last: 9}}}
	if got := cov["a.go"].Uncovered(touched); !slices.Equal(got, []LineRange{{First: 5, Last: 5}}) {
		t.Fatalf("touched uncovered = %+v", got)
	}
	if got := FormatRanges([]LineRange{{First: 3, Last: 3}, {First: 7, Last: 9}}); got != "3, 7-9" {
		t.Fatalf("FormatRanges = %q", got)
	}
	if got := (FileCoverage{1: 0, 2: 0, 3: 0}).Uncovered(AllLines()); !slices.Equal(got, []LineRange{{First: 1, Last: 3}}) {
		t.Fatalf("adjacent lines must merge: %+v", got)
	}
}

func TestRelatedTests(t *testing.T) {
	tests := []struct {
		lang             Language
		production, test string
		want             bool
	}{
		{LanguageGo, "svc/domain/user.go", "svc/domain/user_test.go", true},
		{LanguageGo, "svc/domain/user.go", "svc/domain/other_test.go", true},
		{LanguageGo, "svc/domain/user.go", "svc/app/user_test.go", false},
		{LanguageTypeScript, "src/domain/user.ts", "src/domain/user.test.ts", true},
		{LanguageTypeScript, "src/domain/user.ts", "test/user.spec.tsx", true},
		{LanguageTypeScript, "src/domain/user.ts", "src/__tests__/user.ts", true},
		{LanguageTypeScript, "src/domain/user.ts", "src/domain/users.test.ts", false},
		{LanguagePython, "tts/domain.py", "tests/test_domain.py", true},
		{LanguagePython, "tts/domain.py", "tts/domain_test.py", true},
		{LanguagePython, "tts/domain.py", "tests/test_service.py", false},
		{LanguagePython, "tts/domain.py", "tts/helpers.py", false},
	}
	for _, tt := range tests {
		if got := IsRelatedTest(tt.lang, tt.production, tt.test); got != tt.want {
			t.Errorf("IsRelatedTest(%s, %q, %q) = %v, want %v", tt.lang, tt.production, tt.test, got, tt.want)
		}
	}
	if !NeedsTests(FileFacts{Functions: []FunctionFacts{{Key: "f"}}}) || NeedsTests(FileFacts{}) ||
		NeedsTests(FileFacts{Test: true, Functions: []FunctionFacts{{Key: "f"}}}) {
		t.Fatal("NeedsTests misclassified")
	}
}
