package usecase

import (
	"errors"
	"strings"
	"testing"

	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/domain"
)

func TestAuditFailsWhenTheConfigCannotLoad(t *testing.T) {
	loadErr := errors.New("disk on fire")
	repo := &fakeRepo{}
	uc := NewRunAudit(NewLoadConfig(fakeSource{err: loadErr}, repo), repo, []port.Gate{&fakeGate{name: "g"}})
	if _, err := uc.Execute(t.Context(), AuditInput{}); !errors.Is(err, loadErr) {
		t.Fatalf("err = %v, want %v", err, loadErr)
	}
}

func TestRunGatesPassesTheRequestedTier(t *testing.T) {
	repo := &fakeRepo{}
	gate := &fakeGate{name: "g"}
	uc := NewRunGates(NewLoadConfig(enabledSource(), repo), repo, []port.Gate{gate})
	for tier, want := range map[domain.Tier]domain.Tier{domain.TierFast: domain.TierFast, "": domain.TierFull} {
		if _, err := uc.Execute(t.Context(), RunGatesInput{Tier: tier}); err != nil {
			t.Fatal(err)
		}
		if gate.got.Tier != want {
			t.Errorf("tier %q reached the gates as %q, want %q", tier, gate.got.Tier, want)
		}
	}
}

func TestExplainFileFailures(t *testing.T) {
	loadErr := errors.New("disk on fire")
	if _, err := NewExplainFile(NewLoadConfig(fakeSource{err: loadErr}, &fakeRepo{})).Execute(t.Context(), "a.go"); !errors.Is(err, loadErr) {
		t.Fatalf("config error: %v", err)
	}
	tied, err := domain.NewConfig(domain.ConfigInput{Version: ptr(1), Components: []domain.ComponentInput{
		{Paths: []string{"a/*"}, Languages: []string{"go"}},
		{Paths: []string{"*/b"}, Languages: []string{"go"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewExplainFile(NewLoadConfig(fakeSource{cfg: tied}, &fakeRepo{})).Execute(t.Context(), "a/b/c.go"); !errors.Is(err, domain.ErrAmbiguousComponent) {
		t.Fatalf("ambiguous component: %v", err)
	}
}

func TestExplainFileReportsPackProblems(t *testing.T) {
	cfg, err := domain.NewConfig(domain.ConfigInput{Version: ptr(1), Languages: []string{"go", "python"}, Conventions: []string{"clean-architecture"}})
	if err != nil {
		t.Fatal(err)
	}
	// The test pack has Go paths only, so a Python file cannot be placed.
	out, err := NewExplainFile(NewLoadConfig(fakeSource{cfg: withCleanArchitecture(cfg)}, &fakeRepo{})).Execute(t.Context(), "pkg/domain.py")
	if err != nil || len(out.Layers) != 1 || !strings.Contains(out.Layers[0].Problem, "no rules for this language") {
		t.Fatalf("unsupported language: %+v, %v", out.Layers, err)
	}
}

func TestExplainFileSkipsConventionsThatDoNotGovernTheLanguage(t *testing.T) {
	cfg, err := domain.NewConfig(domain.ConfigInput{Version: ptr(1), Languages: []string{"go", "shell"}, Conventions: []string{"clean-architecture"}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := NewExplainFile(NewLoadConfig(fakeSource{cfg: withCleanArchitecture(cfg)}, &fakeRepo{})).Execute(t.Context(), "scripts/deploy.sh")
	if err != nil || len(out.Layers) != 0 || out.Language != domain.LanguageShell {
		t.Fatalf("a shell script under clean-architecture has no layer: %+v, %v", out, err)
	}
}
