package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"millennium/internal/application/usecase"
	"millennium/internal/domain"
)

type fakeDiscover struct {
	result usecase.DiscoverResult
	err    error
}

func (f *fakeDiscover) Execute(context.Context) (usecase.DiscoverResult, error) {
	return f.result, f.err
}

var discovered = usecase.DiscoverResult{
	Discovery: domain.Discovery{
		Modules: []domain.Module{
			{Root: "api", Markers: []string{"go.mod"}, Files: map[domain.Language]int{domain.LanguageGo: 3}, Tests: 1,
				Fits: []domain.PackFit{{Convention: domain.ConventionCleanArchitecture, Language: domain.LanguageGo, InLayer: 2, Total: 2}}},
			{Root: "", Markers: []string{"package.json"}, Files: map[domain.Language]int{}, React: true},
		},
		Loose: map[domain.Language]int{domain.LanguageShell: 1},
	},
	Components: []domain.ComponentInput{{Paths: []string{"api"}, Languages: []string{"go"}, Conventions: []string{"clean-architecture"}}},
}

func TestDiscoverText(t *testing.T) {
	h := &harness{discover: &fakeDiscover{result: discovered}}
	if code := h.run(t, "discover"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr.String())
	}
	assertContainsAll(t, h.stdout.String(), []string{
		"2 modules", "api (go.mod): go 3 files, 1 test files", "clean-architecture: 2 of 2 go files in its layers",
		". (package.json): no code, React", "outside any module: shell 1 files", `"conventions": [`, "millennium audit --proposal",
	})
	empty := &harness{}
	if code := empty.run(t, "discover"); code != 0 || !strings.Contains(empty.stdout.String(), "No code found") {
		t.Fatalf("exit %d: %s", code, empty.stdout.String())
	}
}

func TestDiscoverJSON(t *testing.T) {
	h := &harness{discover: &fakeDiscover{result: discovered}}
	if code := h.run(t, "discover", "--format", "json"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr.String())
	}
	var out struct {
		Modules  []moduleJSON `json:"modules"`
		Proposal draftJSON    `json:"proposal"`
	}
	if err := json.Unmarshal(h.stdout.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Modules) != 2 || out.Modules[1].Root != "." || !out.Modules[0].Fits[0].Fits || out.Proposal.Config.Components[0].Paths[0] != "api" {
		t.Fatalf("output = %s", h.stdout.String())
	}
}

func TestDiscoverFailures(t *testing.T) {
	for _, args := range [][]string{{"discover", "extra"}, {"discover", "--format", "yaml"}, {"discover", "--nope"}} {
		if code := (&harness{}).run(t, args...); code != 2 {
			t.Errorf("%q: exit %d", args, code)
		}
	}
	broken := &harness{discover: &fakeDiscover{err: errors.New("git broke")}}
	if code := broken.run(t, "discover"); code != 2 || !strings.Contains(broken.stderr.String(), "git broke") {
		t.Fatalf("exit %d: %s", code, broken.stderr.String())
	}
}

func TestAuditAgainstAProposal(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "draft.json")
	if err := os.WriteFile(file, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := domain.Config{}
	h := &harness{audit: &fakeAudit{report: passing}, applier: &fakeApplier{plan: usecase.ApplyPlan{Config: &cfg}}}
	if code := h.run(t, "audit", "--proposal", file); code != 0 || h.audit.got.Config != &cfg || h.applier.applied {
		t.Fatalf("exit %d, config %v, applied %v: %s", code, h.audit.got.Config, h.applier.applied, h.stderr.String())
	}
	approvalsOnly := &harness{applier: &fakeApplier{}}
	if code := approvalsOnly.run(t, "audit", "--proposal", file); code != 2 || !strings.Contains(approvalsOnly.stderr.String(), "no config to audit against") {
		t.Fatalf("exit %d: %s", code, approvalsOnly.stderr.String())
	}
	invalid := &harness{applier: &fakeApplier{prepareErr: errors.New("proposal: version 2")}}
	if code := invalid.run(t, "audit", "--proposal", file); code != 2 || !strings.Contains(invalid.stderr.String(), "version 2") {
		t.Fatalf("exit %d: %s", code, invalid.stderr.String())
	}
	missing := &harness{}
	if code := missing.run(t, "audit", "--proposal", filepath.Join(dir, "none.json")); code != 2 || !strings.Contains(missing.stderr.String(), "read the proposal") {
		t.Fatalf("exit %d: %s", code, missing.stderr.String())
	}
}
