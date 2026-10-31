package domain

import (
	"errors"
	"slices"
	"testing"
)

// monorepo builds a config in the components form. The list is never nil:
// nil means the single-project form.
func monorepo(components ...ComponentInput) ConfigInput {
	return ConfigInput{Version: ptr(1), Components: append([]ComponentInput{}, components...)}
}

func TestComponentsValidation(t *testing.T) {
	tests := []struct {
		name string
		in   ConfigInput
		want string
	}{
		{"empty list", monorepo(), "at least one component"},
		{"no paths", monorepo(ComponentInput{Languages: []string{"go"}}), "components[0].paths: at least one path"},
		{"no languages", monorepo(ComponentInput{Paths: []string{"api"}}), "components[0].languages: at least one language"},
		{"bad path", monorepo(ComponentInput{Paths: []string{"../api"}, Languages: []string{"go"}}), "components[0].paths: pattern"},
		{"recursive path", monorepo(ComponentInput{Paths: []string{"**/api"}, Languages: []string{"go"}}), `"**" is not allowed here`},
		{"react without typescript", monorepo(ComponentInput{Paths: []string{"web"}, Languages: []string{"go"}, Conventions: []string{"react-feature-based"}}),
			`components[0].conventions: "react-feature-based" needs "typescript"`},
		{"empty layer override", monorepo(ComponentInput{Paths: []string{"api"}, Languages: []string{"go"}, Layers: map[string][]string{"domain": {}}}),
			"components[0].layers.domain: at least one path"},
		{"both forms", func() ConfigInput {
			in := monorepo(ComponentInput{Paths: []string{"api"}, Languages: []string{"go"}})
			in.Languages = []string{"go"}
			return in
		}(), "belong inside each component"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { assertConfigError(t, tt.in, tt.want) })
	}
}

func TestConfigLanguagesIsTheUnionOfComponents(t *testing.T) {
	cfg, err := NewConfig(monorepo(
		ComponentInput{Paths: []string{"web"}, Languages: []string{"typescript"}},
		ComponentInput{Paths: []string{"api"}, Languages: []string{"go"}},
	))
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if !slices.Equal(cfg.Languages, []Language{LanguageGo, LanguageTypeScript}) {
		t.Fatalf("languages = %v", cfg.Languages)
	}
}

func serverScriptsConfig(t *testing.T) Config {
	t.Helper()
	cfg, err := NewConfig(monorepo(
		ComponentInput{Paths: []string{"services/*/api"}, Languages: []string{"go"}, Conventions: []string{"clean-architecture"}},
		ComponentInput{Paths: []string{"services/identity/api"}, Languages: []string{"typescript"}},
		ComponentInput{Paths: []string{"services/*"}, Languages: []string{"python"}},
		ComponentInput{Paths: []string{"*/identity/api"}, Languages: []string{"go"}, Reason: "deliberately ambiguous for the test"},
	))
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	return cfg
}

func TestComponentForPicksTheMostSpecificMatch(t *testing.T) {
	cfg := serverScriptsConfig(t)
	tests := []struct {
		file, component, root string
	}{
		{"services/game/api/internal/domain/x.go", "services/*/api", "services/game/api"},
		{"services/game/api/main.go", "services/*/api", "services/game/api"},
		{"services/kokoro/app/main.py", "services/*", "services/kokoro"},
	}
	for _, tt := range tests {
		match, ok, err := cfg.ComponentFor(tt.file)
		if err != nil || !ok || match.Component.Name() != tt.component || match.Root != tt.root {
			t.Errorf("ComponentFor(%q) = %q at %q, %v, %v; want %q at %q", tt.file, match.Component.Name(), match.Root, ok, err, tt.component, tt.root)
		}
	}
}

func TestComponentForReportsTiesAndMisses(t *testing.T) {
	cfg := serverScriptsConfig(t)
	// services/identity/api and */identity/api are both depth 3 with 2 vs 3
	// literals, so the literal pattern wins there; scripts/ matches nothing.
	match, ok, err := cfg.ComponentFor("services/identity/api/src/main.ts")
	if err != nil || !ok || match.Component.Name() != "services/identity/api" {
		t.Fatalf("identity: %q, %v, %v", match.Component.Name(), ok, err)
	}
	if _, ok, err := cfg.ComponentFor("scripts/install.sh"); ok || err != nil {
		t.Fatalf("scripts/install.sh: ok %v, err %v; want no component", ok, err)
	}

	tied, err := NewConfig(monorepo(
		ComponentInput{Paths: []string{"services/*/api"}, Languages: []string{"go"}},
		ComponentInput{Paths: []string{"*/game/api"}, Languages: []string{"go"}},
	))
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if _, _, err := tied.ComponentFor("services/game/api/main.go"); !errors.Is(err, ErrAmbiguousComponent) {
		t.Fatalf("err = %v, want ErrAmbiguousComponent", err)
	}
}

func TestConfigExcluded(t *testing.T) {
	in := minimalInput()
	in.Exclude = []string{"**/node_modules/**", "dist"}
	cfg, err := NewConfig(in)
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	for p, want := range map[string]bool{"web/node_modules/a/b.js": true, "dist/app.js": true, "src/dist.go": false} {
		if got := cfg.Excluded(p); got != want {
			t.Errorf("Excluded(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestTestRecipes(t *testing.T) {
	recipe := &TestRecipe{Command: []string{"npm", "test"}, LCOV: "coverage/lcov.info"}
	cfg, err := NewConfig(monorepo(ComponentInput{Paths: []string{"web"}, Languages: []string{"typescript"}, Test: recipe}))
	if err != nil || cfg.Components[0].Test == nil || cfg.Components[0].Test.LCOV != "coverage/lcov.info" {
		t.Fatalf("component recipe: %+v, %v", cfg.Components, err)
	}
	single := minimalInput()
	single.Test = recipe
	if cfg, err := NewConfig(single); err != nil || cfg.Components[0].Test == nil {
		t.Fatalf("single-project recipe: %+v, %v", cfg.Components, err)
	}
	bad := []*TestRecipe{{LCOV: "lcov.info"}, {Command: []string{"npm", ""}, LCOV: "lcov.info"}, {Command: []string{"npm"}, LCOV: "../lcov.info"}}
	for _, r := range bad {
		assertConfigError(t, monorepo(ComponentInput{Paths: []string{"web"}, Languages: []string{"typescript"}, Test: r}), "components[0].test")
	}
	both := monorepo(ComponentInput{Paths: []string{"web"}, Languages: []string{"typescript"}})
	both.Test = recipe
	assertConfigError(t, both, "belong inside each component")
}

func TestDisableValidation(t *testing.T) {
	ok := minimalInput()
	ok.Disable = []DisabledCheck{{Check: "structure/function-length", Reason: "upstream style keeps long handlers"}}
	cfg, err := NewConfig(ok)
	if err != nil || len(cfg.Disabled) != 1 {
		t.Fatalf("NewConfig: %+v, %v", cfg.Disabled, err)
	}
	tests := []struct {
		name  string
		check DisabledCheck
		want  string
	}{
		{"whole gate", DisabledCheck{Check: "structure", Reason: "a long enough reason"}, "must name one rule as gate/rule"},
		{"no reason", DisabledCheck{Check: "hygiene/secret", Reason: "temp"}, "needs a reason"},
		{"bad name", DisabledCheck{Check: "Code/Secret", Reason: "a long enough reason"}, "must name one rule"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := minimalInput()
			in.Disable = []DisabledCheck{tt.check}
			assertConfigError(t, in, tt.want)
		})
	}
	twice := minimalInput()
	twice.Disable = []DisabledCheck{ok.Disable[0], ok.Disable[0]}
	assertConfigError(t, twice, "listed more than once")
}
