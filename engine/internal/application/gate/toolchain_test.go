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

func TestToolchainAcceptsAnyAlternativeName(t *testing.T) {
	python := port.GateInput{Config: domain.Config{Languages: []domain.Language{domain.LanguagePython}}}
	tests := []struct {
		name         string
		missing      []string
		wantFindings int
	}{
		{"python3 present", nil, 0},
		{"only python present, as on Windows", []string{"python3", "py"}, 0},
		{"only the py launcher present", []string{"python3", "python"}, 0},
		{"none present", []string{"python3", "python", "py"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			missing := map[string]bool{}
			for _, name := range tt.missing {
				missing[name] = true
			}
			findings, err := NewToolchain(fakeLocator{missing: missing}).Run(t.Context(), python)
			if err != nil || len(findings) != tt.wantFindings {
				t.Fatalf("findings %+v, err %v; want %d findings", findings, err, tt.wantFindings)
			}
			if tt.wantFindings == 1 && !strings.Contains(findings[0].Message, "python3, python or py") {
				t.Fatalf("message %q does not list the alternatives", findings[0].Message)
			}
		})
	}
}
