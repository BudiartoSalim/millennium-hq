package usecase

import (
	"errors"
	"testing"

	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/domain"
)

// fakePacks is a pack catalog: a built-in clean-architecture with Go paths
// only, and a react-feature-based suggested for React modules.
type fakePacks struct{ err error }

func (f fakePacks) Packs() (domain.Catalog, error) {
	if f.err != nil {
		return nil, f.err
	}
	return testPacks(), nil
}

func testPacks() domain.Catalog {
	clean, err := domain.NewPack(domain.PackInput{
		Name: "clean-architecture", Origin: domain.PackBuiltin, Languages: []string{"go", "python"},
		Layers: []domain.PackLayer{{Name: "domain"}},
		Paths:  map[string]map[string][]string{"go": {"domain": {"internal/domain"}}},
		Guide:  "Business rules live in domain.",
	})
	if err != nil {
		panic(err)
	}
	react, err := domain.NewPack(domain.PackInput{
		Name: "react-feature-based", Origin: domain.PackBuiltin, Languages: []string{"typescript"},
		Layers:  []domain.PackLayer{{Name: "features", Isolated: true}},
		Paths:   map[string]map[string][]string{"typescript": {"features": {"src/features/*"}}},
		Suggest: []string{"react"}, Guide: "Features never import each other.",
	})
	if err != nil {
		panic(err)
	}
	return domain.Catalog{clean.Name: clean, react.Name: react}
}

// withCleanArchitecture gives cfg the clean-architecture pack only, so any
// other convention it names has no pack.
func withCleanArchitecture(cfg domain.Config) domain.Config {
	cfg.Packs = domain.Catalog{"clean-architecture": testPacks()["clean-architecture"]}
	return cfg
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
	out, err := NewExplainFile(NewLoadConfig(fakeSource{cfg: withCleanArchitecture(cfg)}, &fakeRepo{})).Execute(t.Context(), path)
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
	if len(web.Layers) != 1 || web.Layers[0].Problem != "no pack for this convention is available" {
		t.Fatalf("web layers = %+v", web.Layers)
	}
	outside := explain(t, "docs/notes.txt")
	if outside.Component != nil || outside.Language != "" {
		t.Fatalf("a file outside every component: %+v", outside)
	}
	if !explain(t, "dist/app.js").Excluded {
		t.Fatal("dist/app.js should be reported as excluded")
	}
}

func TestExplainFileNeedsAConfigAndAValidPath(t *testing.T) {
	uc := NewExplainFile(NewLoadConfig(fakeSource{err: port.ErrConfigNotFound}, &fakeRepo{}))
	if _, err := uc.Execute(t.Context(), "a.go"); !errors.Is(err, ErrConfigRequired) {
		t.Fatalf("err = %v, want ErrConfigRequired", err)
	}
	if _, err := uc.Execute(t.Context(), "../a.go"); err == nil {
		t.Fatal("a path outside the repository was accepted")
	}
}
