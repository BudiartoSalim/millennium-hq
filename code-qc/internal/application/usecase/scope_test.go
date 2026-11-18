package usecase

import (
	"testing"

	"millennium-code-qc/internal/domain"
)

// scope matches the protected paths as the guard does: anywhere in a
// changed path, in any case, and as Win32 reads a name, so a branch cannot
// carry a protected file past the merge under another spelling or in a
// subdirectory, even inside the package's paths.
func TestCheckScopeMatchesProtectedPathsAsTheGuardDoes(t *testing.T) {
	tests := []struct {
		path string
		want string // the finding's rule, "" for none
	}{
		{".MILLENNIUM/config.json", "protected-path"},
		{".Claude/Settings.json", "protected-path"},
		{"sub/.millennium/config.json", "protected-path"},
		{"pkg/.pi/extensions/a.ts", "protected-path"},
		{"pkg/.PI/Settings.json", "protected-path"},
		{"pkg/.claude/settings.local.json.", "protected-path"},
		{"pkg/.claude/settings.json::$DATA", "protected-path"},
		{"pkg/.millenniumx/config.json", ""},
		{"pkg/.claude/settings.json.bak", ""},
		{"pkg/.pi/worktrees/a.ts", ""},
		{"other/a.go", "out-of-scope"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			repo := &fakeRepo{changes: []domain.ChangedFile{{Path: tt.path, Kind: domain.ChangeAdded}}}
			uc := NewCheckScope(NewLoadConfig(enabledSourceWithDefaults(t), repo), repo)
			report, err := uc.Execute(t.Context(), ScopeInput{Base: "main", Allow: []string{"pkg/**", "sub/**", ".MILLENNIUM/**", ".Claude/**"}})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			var got string
			for _, f := range report.Findings() {
				got = f.Rule
			}
			if got != tt.want {
				t.Fatalf("finding %q, want %q (%+v)", got, tt.want, report.Findings())
			}
		})
	}
}
