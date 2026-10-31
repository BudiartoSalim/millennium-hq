package usecase

import (
	"errors"
	"slices"
	"testing"

	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/domain"
)

func TestAuditChecksEveryFileAsNewCode(t *testing.T) {
	cfg, err := domain.NewConfig(domain.ConfigInput{Version: ptr(1), Languages: []string{"go"}, Exclude: []string{"gen"}})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	repo := &fakeRepo{files: []string{"a.go", "svc/b.go", "svc/c/d.go", "svcx/e.go", "gen/f.go"}}
	tests := []struct {
		path string
		want []string
	}{
		{"", []string{"a.go", "svc/b.go", "svc/c/d.go", "svcx/e.go"}},
		{"svc", []string{"svc/b.go", "svc/c/d.go"}},
		{"svc/b.go", []string{"svc/b.go"}},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			gate := &fakeGate{name: "g"}
			uc := NewRunAudit(NewLoadConfig(fakeSource{cfg: cfg}, repo), repo, []port.Gate{gate})
			report, err := uc.Execute(t.Context(), AuditInput{Path: tt.path})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if gate.got.Changes.Base != "" {
				t.Errorf("an audit has no base, got %q", gate.got.Changes.Base)
			}
			got := auditedPaths(t, gate.got.Changes.Files)
			if !slices.Equal(got, tt.want) || report.ChangedFiles != len(tt.want) {
				t.Fatalf("audited %q, want %q", got, tt.want)
			}
		})
	}
}

// auditedPaths returns the paths an audit handed to the gates and checks that
// each is marked as new code.
func auditedPaths(t *testing.T, files []domain.ChangedFile) []string {
	t.Helper()
	paths := make([]string, 0, len(files))
	for _, f := range files {
		if f.Kind != domain.ChangeAdded {
			t.Errorf("%s has kind %s; an audit treats every file as new", f.Path, f.Kind)
		}
		paths = append(paths, f.Path)
	}
	return paths
}

func TestAuditFailures(t *testing.T) {
	listErr := errors.New("git failed")
	tests := []struct {
		name    string
		source  fakeSource
		repo    *fakeRepo
		path    string
		wantErr error
	}{
		{"needs a config", fakeSource{err: port.ErrConfigNotFound}, &fakeRepo{}, "", ErrConfigRequired},
		{"listing fails", enabledSource(), &fakeRepo{filesErr: listErr}, "", listErr},
		{"path outside the repository", enabledSource(), &fakeRepo{}, "../x", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := NewRunAudit(NewLoadConfig(tt.source, tt.repo), tt.repo, []port.Gate{&fakeGate{name: "g"}})
			_, err := uc.Execute(t.Context(), AuditInput{Path: tt.path})
			if err == nil || (tt.wantErr != nil && !errors.Is(err, tt.wantErr)) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestAuditAgainstAProposedConfig(t *testing.T) {
	proposed, err := domain.NewConfig(domain.ConfigInput{Version: ptr(1), Languages: []string{"go"}, Exclude: []string{"gen"}})
	if err != nil {
		t.Fatal(err)
	}
	repo := &fakeRepo{files: []string{"a.go", "gen/f.go"}}
	gate := &fakeGate{name: "g"}
	// No config in the repository: the proposal is what the audit uses.
	uc := NewRunAudit(NewLoadConfig(fakeSource{err: port.ErrConfigNotFound}, repo), repo, []port.Gate{gate})
	if _, err := uc.Execute(t.Context(), AuditInput{Config: &proposed}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := auditedPaths(t, gate.got.Changes.Files); !slices.Equal(got, []string{"a.go"}) {
		t.Fatalf("audited %q; the proposal's exclude must apply", got)
	}
}
