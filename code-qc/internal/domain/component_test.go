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
		{"duplicate convention", monorepo(ComponentInput{Paths: []string{"web"}, Languages: []string{"go"}, Conventions: []string{"layers", "layers"}}),
			`components[0].conventions: "layers" is listed more than once`},
		{"limits without reason", monorepo(ComponentInput{Paths: []string{"api"}, Languages: []string{"go"}, Limits: &LimitOverride{Values: LimitsInput{Cognitive: ptr(20)}}}),
			"components[0].limits.reason: say why"},
		{"empty limits", monorepo(ComponentInput{Paths: []string{"api"}, Languages: []string{"go"}, Limits: &LimitOverride{Reason: "generated parsers are long"}}),
			"components[0].limits: set at least one"},
		{"limit out of range", monorepo(ComponentInput{Paths: []string{"api"}, Languages: []string{"go"}, Limits: &LimitOverride{Values: LimitsInput{FunctionLines: ptr(5000)}, Reason: "generated parsers are long"}}),
			"components[0].limits.functionLines: 5000 must be between 1 and 1000"},
		{"top-level limits with components", func() ConfigInput {
			in := monorepo(ComponentInput{Paths: []string{"api"}, Languages: []string{"go"}})
			in.Limits = &LimitOverride{Values: LimitsInput{Nesting: ptr(4)}, Reason: "generated parsers are long"}
			return in
		}(), "languages, conventions, test and limits belong inside each component"},
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

// Reaches decides what qa-tester may read, so it matches without regard to
// case: on a case-insensitive file system each spelling is the same file.
func TestReaches(t *testing.T) {
	cfg := serverScriptsConfig(t)
	tied, err := NewConfig(monorepo(
		ComponentInput{Paths: []string{"services/*/api"}, Languages: []string{"go"}},
		ComponentInput{Paths: []string{"*/game/api"}, Languages: []string{"go"}},
	))
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	single, err := NewConfig(minimalInput())
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	tests := []struct {
		name string
		cfg  Config
		dir  string
		want bool
	}{
		{"inside a component", cfg, "services/game/api/internal", true},
		{"inside a component, upper case", cfg, "SERVICES/GAME/API/internal", true},
		{"a component root, mixed case", cfg, "Services/Identity/Api", true},
		{"above a component, mixed case", cfg, "Services", true},
		{"the repository root", cfg, "", true},
		{"claimed by two components", tied, "services/game/api", true},
		{"outside every component", cfg, "scripts/install", false},
		{"a directory a pattern can match", cfg, "scripts", true},
		{"outside, upper case", cfg, "SCRIPTS/x", false},
		{"anywhere in a single project", single, "Docs/x", true},
	}
	for _, tt := range tests {
		if got := tt.cfg.Reaches(tt.dir); got != tt.want {
			t.Errorf("%s: Reaches(%q) = %v, want %v", tt.name, tt.dir, got, tt.want)
		}
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
	ok.Disable = []DisableInput{{Check: "structure/function-length", Reason: "upstream style keeps long handlers"}}
	cfg, err := NewConfig(ok)
	if err != nil || len(cfg.Disabled) != 1 {
		t.Fatalf("NewConfig: %+v, %v", cfg.Disabled, err)
	}
	tests := []struct {
		name  string
		check DisableInput
		want  string
	}{
		{"whole gate", DisableInput{Check: "structure", Reason: "a long enough reason"}, "must name one rule as gate/rule"},
		{"no reason", DisableInput{Check: "hygiene/secret", Reason: "temp"}, "needs a reason"},
		{"bad name", DisableInput{Check: "Code/Secret", Reason: "a long enough reason"}, "must name one rule"},
		{"empty paths", DisableInput{Check: "shell/strict-mode", Reason: "a long enough reason", Paths: []string{}}, "empty paths list"},
		{"bad path", DisableInput{Check: "shell/strict-mode", Reason: "a long enough reason", Paths: []string{"../x"}}, "disable: shell/strict-mode paths"},
		{"root path", DisableInput{Check: "shell/strict-mode", Reason: "a long enough reason", Paths: []string{"."}}, "leave paths out to disable it everywhere"},
		{"duplicate path", DisableInput{Check: "shell/strict-mode", Reason: "a long enough reason", Paths: []string{"tests/**", "a", "tests/**"}}, "lists a path more than once"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := minimalInput()
			in.Disable = []DisableInput{tt.check}
			assertConfigError(t, in, tt.want)
		})
	}
	twice := minimalInput()
	twice.Disable = []DisableInput{ok.Disable[0], ok.Disable[0]}
	assertConfigError(t, twice, "listed more than once")
}
