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
	if report.ChangedFiles != 1 || len(gate.got.Changes.Files) != 1 || gate.got.Changes.Files[0].Path != "main.go" {
		t.Fatalf("excluded file reached the gates: %+v", gate.got.Changes)
	}
}

func ptr[T any](v T) *T { return &v }
