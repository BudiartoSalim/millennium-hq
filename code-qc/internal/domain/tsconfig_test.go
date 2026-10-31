package domain

import (
	"slices"
	"testing"
)

func TestTSConfigProblems(t *testing.T) {
	tests := []struct {
		name, config string
		want         []string
	}{
		{"strict", `{"compilerOptions":{"strict":true,"target":"es2022"}}`, nil},
		{"not strict", `{"compilerOptions":{"target":"es2022"}}`, []string{`"strict" must be true`}},
		{"strict but weakened", `{"compilerOptions":{"strict":true,"strictNullChecks":false,"noImplicitAny":false}}`,
			[]string{`"noImplicitAny" must not be switched off`, `"strictNullChecks" must not be switched off`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TSConfigProblems([]byte(tt.config))
			if err != nil || !slices.Equal(got, tt.want) {
				t.Fatalf("problems = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
	if _, err := TSConfigProblems([]byte("not json")); err == nil {
		t.Fatal("unreadable output must be an error")
	}
}
