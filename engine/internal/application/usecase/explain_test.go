package usecase

import (
	"errors"
	"testing"

	"millennium/internal/application/port"
	"millennium/internal/domain"
)

type fakePacks struct{}

func (fakePacks) Pack(name domain.Convention) (domain.Pack, error) {
	if name != domain.ConventionCleanArchitecture {
		return domain.Pack{}, port.ErrPackUnavailable
	}
	return domain.NewPack(domain.PackInput{
		Name:   "clean-architecture",
		Layers: []domain.PackLayer{{Name: "domain"}},
		Paths:  map[string]map[string][]string{"go": {"domain": {"internal/domain"}}},
	})
}

func explain(t *testing.T, path string) Explanation {
	t.Helper()
	cfg, err := domain.NewConfig(domain.ConfigInput{Version: ptr(1), Exclude: []string{"dist"}, Components: []domain.ComponentInput{
		{Paths: []string{"services/*/api"}, Languages: []string{"go"}, Conventions: []string{"clean-architecture"}},
		{Paths: []string{"web"}, Languages: []string{"typescript"}, Conventions: []string{"react-feature-based"}},
	}})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	out, err := NewExplainFile(NewLoadConfig(fakeSource{cfg: cfg}, &fakeRepo{}), fakePacks{}).Execute(t.Context(), path)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return out
}

func TestExplainFile(t *testing.T) {
	out := explain(t, "services/game/api/internal/domain/user.go")
	if out.Language != domain.LanguageGo || out.Component == nil || out.Component.Root != "services/game/api" {
		t.Fatalf("explanation = %+v", out)
	}
	if len(out.Layers) != 1 || out.Layers[0].Layer != "domain" || out.Layers[0].Problem != "" {
		t.Fatalf("layers = %+v", out.Layers)
	}
	if out.Limits != domain.DefaultLimits() {
		t.Fatalf("limits = %+v", out.Limits)
	}
}

func TestExplainFileProblemsAndMisses(t *testing.T) {
	web := explain(t, "web/src/App.tsx")
	if len(web.Layers) != 1 || web.Layers[0].Problem != "not available in this engine version" {
		t.Fatalf("web layers = %+v", web.Layers)
	}
	outside := explain(t, "scripts/install.sh")
	if outside.Component != nil || outside.Language != "" {
		t.Fatalf("a file outside every component: %+v", outside)
	}
	if !explain(t, "dist/app.js").Excluded {
		t.Fatal("dist/app.js should be reported as excluded")
	}
}

func TestExplainFileNeedsAConfigAndAValidPath(t *testing.T) {
	uc := NewExplainFile(NewLoadConfig(fakeSource{err: port.ErrConfigNotFound}, &fakeRepo{}), fakePacks{})
	if _, err := uc.Execute(t.Context(), "a.go"); !errors.Is(err, ErrConfigRequired) {
		t.Fatalf("err = %v, want ErrConfigRequired", err)
	}
	if _, err := uc.Execute(t.Context(), "../a.go"); err == nil {
		t.Fatal("a path outside the repository was accepted")
	}
}
