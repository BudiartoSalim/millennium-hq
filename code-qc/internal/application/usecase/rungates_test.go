package usecase

import (
	"errors"
	"strings"
	"testing"

	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/domain"
)

func enabledSource() fakeSource {
	return fakeSource{cfg: domain.Config{Languages: []domain.Language{domain.LanguageGo}}}
}

func TestRunGatesNotEnabled(t *testing.T) {
	notFound := fakeSource{err: port.ErrConfigNotFound}
	tests := []struct {
		name        string
		requirement ConfigRequirement
		wantErr     error
	}{
		{"optional config", ConfigOptional, nil},
		{"required config", ConfigRequired, ErrConfigRequired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{}
			gate := &fakeGate{name: "g"}
			uc := NewRunGates(NewLoadConfig(notFound, repo), repo, []port.Gate{gate})
			report, err := uc.Execute(t.Context(), RunGatesInput{Config: tt.requirement})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if report.Enabled || gate.got.Config.Languages != nil {
				t.Fatalf("gates ran in a repository that is not opted in: %+v", report)
			}
		})
	}
}

func TestRunGatesPassesChangesToEveryGate(t *testing.T) {
	changes := []domain.ChangedFile{{Path: "main.go", Kind: domain.ChangeModified}}
	repo := &fakeRepo{changes: changes}
	first, second := &fakeGate{name: "first"}, &fakeGate{name: "second"}
	uc := NewRunGates(NewLoadConfig(enabledSource(), repo), repo, []port.Gate{first, second})

	report, err := uc.Execute(t.Context(), RunGatesInput{Base: "main"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if repo.gotBase != "main" {
		t.Fatalf("base = %q, want main", repo.gotBase)
	}
	if !report.Passed() || report.ChangedFiles != 1 || len(report.Results) != 2 {
		t.Fatalf("unexpected report %+v", report)
	}
	for _, g := range []*fakeGate{first, second} {
		if len(g.got.Changes.Files) != 1 || g.got.Changes.Base != "base-tree" || !g.got.Config.Has(domain.LanguageGo) {
			t.Errorf("gate %s got input %+v", g.name, g.got)
		}
	}
}

func TestRunGatesTurnsBrokenGatesIntoFindings(t *testing.T) {
	tests := []struct {
		name string
		gate *fakeGate
		want string
	}{
		{"gate error", &fakeGate{name: "err", err: errors.New("tool crashed")}, "tool crashed"},
		{"gate panic", &fakeGate{name: "panic", panicked: true}, "panicked: boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{}
			uc := NewRunGates(NewLoadConfig(enabledSource(), repo), repo, []port.Gate{tt.gate})
			report, err := uc.Execute(t.Context(), RunGatesInput{})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if report.Passed() {
				t.Fatal("a broken gate must block")
			}
			f := report.Findings()[0]
			if f.Gate != tt.gate.name || f.Rule != "gate-failed" || !strings.Contains(f.Message, tt.want) {
				t.Fatalf("finding = %+v, want gate-failed mentioning %q", f, tt.want)
			}
		})
	}
}

func TestRunGatesFailsWhenChangesCannotBeRead(t *testing.T) {
	gitErr := errors.New("not a git repository")
	repo := &fakeRepo{changesErr: gitErr}
	uc := NewRunGates(NewLoadConfig(enabledSource(), repo), repo, []port.Gate{&fakeGate{name: "g"}})
	if _, err := uc.Execute(t.Context(), RunGatesInput{}); !errors.Is(err, gitErr) {
		t.Fatalf("err = %v, want %v", err, gitErr)
	}
}

func TestRunGatesFailsOnConfigError(t *testing.T) {
	repo := &fakeRepo{tracked: true}
	uc := NewRunGates(NewLoadConfig(fakeSource{err: port.ErrConfigNotFound}, repo), repo, []port.Gate{&fakeGate{name: "g"}})
	if _, err := uc.Execute(t.Context(), RunGatesInput{}); !errors.Is(err, ErrConfigRemoved) {
		t.Fatalf("err = %v, want %v", err, ErrConfigRemoved)
	}
}

func TestRunGatesDropsExcludedFiles(t *testing.T) {
	cfg, err := domain.NewConfig(domain.ConfigInput{Version: ptr(1), Languages: []string{"go"}, Exclude: []string{"**/vendor/**"}})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	repo := &fakeRepo{changes: []domain.ChangedFile{
		{Path: "main.go", Kind: domain.ChangeModified},
		{Path: "lib/vendor/x/y.go", Kind: domain.ChangeAdded},
	}}
	gate := &fakeGate{name: "g"}
	report, err := NewRunGates(NewLoadConfig(fakeSource{cfg: cfg}, repo), repo, []port.Gate{gate}).Execute(t.Context(), RunGatesInput{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if report.Proposed || report.ChangedFiles != 1 || len(gate.got.Changes.Files) != 1 || gate.got.Changes.Files[0].Path != "main.go" {
		t.Fatalf("excluded file reached the gates: %+v", gate.got.Changes)
	}
}

func ptr[T any](v T) *T { return &v }

// A proposal replaces the repository's config, so it works in a repository
// that is not opted in yet, and its own excludes apply.
func TestRunGatesAgainstAProposedConfig(t *testing.T) {
	proposed, err := domain.NewConfig(domain.ConfigInput{Version: ptr(1), Languages: []string{"python"}, Exclude: []string{"gen/**"}})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	sources := map[string]fakeSource{"opted in": enabledSource(), "not opted in": {err: port.ErrConfigNotFound}}
	for name, source := range sources {
		t.Run(name, func(t *testing.T) {
			repo := &fakeRepo{changes: []domain.ChangedFile{
				{Path: "app.py", Kind: domain.ChangeModified},
				{Path: "gen/x.py", Kind: domain.ChangeAdded},
			}}
			gate := &fakeGate{name: "g"}
			uc := NewRunGates(NewLoadConfig(source, repo), repo, []port.Gate{gate})
			report, err := uc.Execute(t.Context(), RunGatesInput{Proposed: &proposed})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if !report.Enabled || !report.Proposed || !gate.got.Config.Has(domain.LanguagePython) || gate.got.Config.Has(domain.LanguageGo) {
				t.Fatalf("gates did not run against the proposal: %+v", gate.got.Config)
			}
			if report.ChangedFiles != 1 || gate.got.Changes.Files[0].Path != "app.py" {
				t.Fatalf("the proposal's exclude was not applied: %+v", gate.got.Changes)
			}
		})
	}
}

var (
	errTrackedCheck = errors.New("git failed")
	errUnreadable   = errors.New("config.json must be a regular file, found a symlink")
)

// A proposal stands in for the repository's config, but cannot hide that
// the config was deleted or cannot be read, nor satisfy a run that requires
// the repository's own config.
func TestRunGatesProposalRefusals(t *testing.T) {
	proposed, err := domain.NewConfig(domain.ConfigInput{Version: ptr(1), Languages: []string{"go"}})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	tests := []struct {
		name   string
		source fakeSource
		repo   *fakeRepo
		in     RunGatesInput
		want   error
	}{
		{"config deleted from the working tree", fakeSource{err: port.ErrConfigNotFound}, &fakeRepo{tracked: true},
			RunGatesInput{Proposed: &proposed}, ErrConfigRemoved},
		{"required config", enabledSource(), &fakeRepo{}, RunGatesInput{Config: ConfigRequired, Proposed: &proposed}, ErrRequiredWithProposal},
		{"tracked check failing", fakeSource{err: port.ErrConfigNotFound}, &fakeRepo{trackedErr: errTrackedCheck},
			RunGatesInput{Proposed: &proposed}, errTrackedCheck},
		{"config unreadable", fakeSource{err: errUnreadable}, &fakeRepo{}, RunGatesInput{Proposed: &proposed}, errUnreadable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gate := &fakeGate{name: "g"}
			uc := NewRunGates(NewLoadConfig(tt.source, tt.repo), tt.repo, []port.Gate{gate})
			if _, err := uc.Execute(t.Context(), tt.in); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if gate.got.Config.Languages != nil {
				t.Fatal("gates ran despite the refusal")
			}
		})
	}
}
