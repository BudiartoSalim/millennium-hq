package domain

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func mustPack(t *testing.T, in PackInput) Pack {
	t.Helper()
	pack, err := NewPack(in)
	if err != nil {
		t.Fatalf("NewPack: %v", err)
	}
	return pack
}

// testCatalog has a built-in clean-architecture (Go and Python) and a
// project pack "flat" (Go) with stricter limits.
func testCatalog(t *testing.T) Catalog {
	t.Helper()
	builtin := cleanArchitectureInput()
	builtin.Origin = PackBuiltin
	flat := PackInput{
		Name: "flat", Origin: PackProject, Languages: []string{"go"},
		Layers: []PackLayer{{Name: "core", Tested: true}, {Name: "edge", MayImport: []string{"core"}, ThirdParty: true}},
		Paths:  map[string]map[string][]string{"go": {"core": {"core"}, "edge": {"edge"}}},
		Limits: LimitsInput{Nesting: ptr(2), Cognitive: ptr(20)},
		Guide:  "Keep the core pure.",
	}
	catalog, err := NewCatalog([]Pack{mustPack(t, builtin)}, []Pack{mustPack(t, flat)})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	return catalog
}

func TestCatalog(t *testing.T) {
	catalog := testCatalog(t)
	if got := catalog.Names(); !slices.Equal(got, []Convention{"clean-architecture", "flat"}) {
		t.Fatalf("names = %v", got)
	}
	if packs := catalog.Packs(); len(packs) != 2 || packs[1].Origin != PackProject {
		t.Fatalf("packs = %+v", packs)
	}
	replaced := catalog["flat"]
	replaced.Guide = "A new guide."
	next, err := catalog.With([]Pack{replaced})
	if err != nil || next["flat"].Guide != "A new guide." || catalog["flat"].Guide != "Keep the core pure." {
		t.Fatalf("a project pack is replaced in a copy: %v, %+v", err, next["flat"])
	}
}

func TestCatalogRejectsShadowingAndDuplicates(t *testing.T) {
	catalog := testCatalog(t)
	shadow := catalog["clean-architecture"]
	shadow.Origin = PackProject
	_, err := catalog.With([]Pack{shadow})
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) || !strings.Contains(err.Error(), "convention clean-architecture is built in; to change it, copy it under a new name") {
		t.Fatalf("err = %v, want a built-in name to be refused", err)
	}
	flat := catalog["flat"]
	if _, err := catalog.With([]Pack{flat, flat}); err == nil || !strings.Contains(err.Error(), "convention flat is defined twice") {
		t.Fatalf("err = %v, want a duplicate to be refused", err)
	}
}

func TestResolve(t *testing.T) {
	catalog := testCatalog(t)
	cfg := mustConfig(t, ConfigInput{Version: ptr(1), Components: []ComponentInput{
		{Paths: []string{"api"}, Languages: []string{"go"}, Conventions: []string{"flat"}, Layers: map[string][]string{"core": {"internal/core"}, "composition": {"main.go"}}},
		{Paths: []string{"tool"}, Languages: []string{"go"}},
	}})
	resolved, err := cfg.Resolve(catalog)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := resolved.Packs.Names(); !slices.Equal(got, []Convention{"flat"}) {
		t.Fatalf("packs = %v, want only the used one", got)
	}
	if guides := resolved.Guides(); len(guides) != 1 || guides[0].Guide != "Keep the core pure." {
		t.Fatalf("guides = %+v", guides)
	}
}

func TestResolveRejects(t *testing.T) {
	tests := []struct {
		name      string
		component ComponentInput
		want      string
	}{
		{"unknown convention", ComponentInput{Paths: []string{"api"}, Languages: []string{"go"}, Conventions: []string{"hexagonal"}},
			`component api: convention hexagonal does not exist; available: "clean-architecture", "flat"`},
		{"no shared language", ComponentInput{Paths: []string{"web"}, Languages: []string{"typescript"}, Conventions: []string{"flat"}},
			`component web: convention flat governs "go", and the component uses none of them`},
		{"unknown layer", ComponentInput{Paths: []string{"api"}, Languages: []string{"go"}, Conventions: []string{"flat"}, Layers: map[string][]string{"domain": {"x"}}},
			`component api: layers.domain is not a layer of its conventions ("flat")`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := mustConfig(t, ConfigInput{Version: ptr(1), Components: []ComponentInput{tt.component}})
			_, err := cfg.Resolve(testCatalog(t))
			var cfgErr *ConfigError
			if !errors.As(err, &cfgErr) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestLimitsFor(t *testing.T) {
	cfg := mustConfig(t, ConfigInput{Version: ptr(1), Components: []ComponentInput{
		{Paths: []string{"plain"}, Languages: []string{"go"}},
		{Paths: []string{"layered"}, Languages: []string{"go"}, Conventions: []string{"clean-architecture"}},
		{Paths: []string{"both"}, Languages: []string{"go"}, Conventions: []string{"clean-architecture", "flat"}},
		{Paths: []string{"own"}, Languages: []string{"go"}, Conventions: []string{"flat"},
			Limits: &LimitOverride{Values: LimitsInput{FunctionLines: ptr(80)}, Reason: "generated parsers are long"}},
	}})
	resolved, err := cfg.Resolve(testCatalog(t))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := []Limits{
		DefaultLimits(),
		DefaultLimits(),
		{FunctionLines: 50, Nesting: 2, Cognitive: 15},
		{FunctionLines: 80, Nesting: 2, Cognitive: 20},
	}
	for i, c := range resolved.Components {
		if got := resolved.LimitsFor(c); got != want[i] {
			t.Errorf("%s: limits = %+v, want %+v", c.Name(), got, want[i])
		}
	}
	if got := cfg.LimitsFor(cfg.Components[2]); got != DefaultLimits() {
		t.Errorf("an unresolved config has no packs, so the defaults apply: %+v", got)
	}
}

func TestLimitsRaised(t *testing.T) {
	got := DefaultLimits().Raised(Limits{FunctionLines: 40, Nesting: 4, Cognitive: 15})
	if !slices.Equal(got, []string{"the nesting depth limit rises from 3 to 4"}) {
		t.Fatalf("raised = %q", got)
	}
}

func resolvedConfig(t *testing.T, catalog Catalog, in ConfigInput) Config {
	t.Helper()
	cfg, err := mustConfig(t, in).Resolve(catalog)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return cfg
}

func TestPackAndLimitLoosenings(t *testing.T) {
	catalog := testCatalog(t)
	in := ConfigInput{Version: ptr(1), Components: []ComponentInput{
		{Paths: []string{"api"}, Languages: []string{"go"}, Conventions: []string{"flat"}},
	}}
	current := resolvedConfig(t, catalog, in)
	weaker := catalog["flat"]
	weaker.Layers = []PackLayer{{Name: "core", MayImport: []string{"edge"}, ThirdParty: true}}
	weaker.Paths = map[Language]map[string][]Pattern{}
	weaker.Limits = Limits{FunctionLines: 50, Nesting: 2, Cognitive: 25}
	next := resolvedConfig(t, Catalog{"flat": weaker}, in)
	want := []string{
		"component api: the cognitive complexity limit rises from 20 to 25",
		"convention flat: layer core may now import edge",
		"convention flat: layer core may now import third-party code",
		"convention flat: layer core no longer needs tests",
		"convention flat removes layer edge",
		"convention flat changes its layer paths",
	}
	if got := Loosenings(&current, next); !slices.Equal(got, want) {
		t.Fatalf("loosenings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestPackLosingALanguageOrIsolation(t *testing.T) {
	isolated := mustPack(t, PackInput{
		Name: "features", Origin: PackProject, Languages: []string{"go", "typescript"},
		Layers: []PackLayer{{Name: "feature", Isolated: true}},
		Paths:  map[string]map[string][]string{"go": {"feature": {"features/*"}}},
		Guide:  "One feature per folder.",
	})
	loose := isolated
	loose.Languages = []Language{LanguageGo}
	loose.Layers = []PackLayer{{Name: "feature"}}
	got := packLoosenings(isolated, loose)
	want := []string{"convention features no longer governs typescript", "convention features: layer feature is no longer isolated"}
	if !slices.Equal(got, want) {
		t.Fatalf("loosenings = %q", got)
	}
	if got := packLoosenings(isolated, isolated); len(got) != 0 {
		t.Fatalf("an unchanged pack loosens nothing: %q", got)
	}
}
