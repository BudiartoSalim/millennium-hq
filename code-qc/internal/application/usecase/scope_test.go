package usecase

import (
	"strings"
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
		{"pkg/.pi", "protected-path"},
		{"pkg/.Claude", "protected-path"},
		{"pkg/sub/.pi.", "protected-path"},
		{"pkg/.pid", ""},
		{"pkg/x.claude", ""},
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

// A branch that commits `.pi` as a symlink to `conf`, with
// `conf/extensions/evil.ts`, adds an extension pi loads: `.pi` is flagged,
// even when the package may change every path.
func TestCheckScopeFlagsALinkInPlaceOfAProtectedDirectory(t *testing.T) {
	repo := &fakeRepo{changes: []domain.ChangedFile{{Path: ".pi", Kind: domain.ChangeAdded}, {Path: "conf/extensions/evil.ts", Kind: domain.ChangeAdded}}}
	uc := NewCheckScope(NewLoadConfig(enabledSourceWithDefaults(t), repo), repo)
	report, err := uc.Execute(t.Context(), ScopeInput{Base: "main", Allow: []string{"**"}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	findings := report.Findings()
	if len(findings) != 1 || findings[0].File != ".pi" || findings[0].Rule != "protected-path" || !strings.Contains(findings[0].Message, "a file or a symlink at .pi") {
		t.Fatalf("findings %+v, want one protected-path finding for .pi", findings)
	}
}
