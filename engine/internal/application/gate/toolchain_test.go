package gate

import (
	"errors"
	"strings"
	"testing"

	"millennium/internal/application/port"
	"millennium/internal/domain"
)

type fakeLocator struct {
	missing map[string]bool
}

func (f fakeLocator) Locate(name string) error {
	if f.missing[name] {
		return errors.New("executable file not found in $PATH")
	}
	return nil
}

func TestToolchainPassesWhenEveryToolIsPresent(t *testing.T) {
	g := NewToolchain(fakeLocator{})
	in := port.GateInput{Config: domain.Config{Languages: domain.Languages()}}
	findings, err := g.Run(t.Context(), in)
	if err != nil || len(findings) != 0 {
		t.Fatalf("got findings %+v, err %v; want none", findings, err)
	}
}

func TestToolchainBlocksOnEachMissingTool(t *testing.T) {
	g := NewToolchain(fakeLocator{missing: map[string]bool{"gofmt": true, "mypy": true}})
	in := port.GateInput{Config: domain.Config{
		Languages:       []domain.Language{domain.LanguageGo, domain.LanguagePython},
		PythonTypeCheck: domain.PythonTypeCheckMypy,
	}}
	findings, err := g.Run(t.Context(), in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 2 {
		t.Fatalf("want 2 findings, got %+v", findings)
	}
	for i, tool := range []string{"gofmt", "mypy"} {
		f := findings[i]
		if f.Gate != "toolchain" || f.Rule != "tool-missing" || !strings.Contains(f.Message, tool) {
			t.Errorf("finding %d = %+v, want toolchain/tool-missing naming %s", i, f, tool)
		}
	}
}
