package domain

import (
	"slices"
	"strings"
	"testing"
)

func discoverPacks(t *testing.T) []Pack {
	t.Helper()
	clean, err := NewPack(cleanArchitectureInput())
	if err != nil {
		t.Fatal(err)
	}
	react, err := NewPack(PackInput{
		Name: "react-feature-based", Languages: []string{"typescript"},
		Layers: []PackLayer{{Name: "shared"}, {Name: "features", MayImport: []string{"shared"}, Isolated: true}},
		Paths:  map[string]map[string][]string{"typescript": {"shared": {"src/components"}, "features": {"src/features/*"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return []Pack{clean, react}
}

func TestDiscover(t *testing.T) {
	files := []string{
		"services/game/api/go.mod",
		"services/game/api/main.go",
		"services/game/api/internal/domain/server.go",
		"services/game/api/internal/domain/server_test.go",
		"services/game/api/internal/application/start.go",
		"services/game/web/package.json",
		"services/game/web/src/features/lobby/Lobby.tsx",
		"services/game/web/src/components/Button.tsx",
		"services/legacy/go.mod",
		"services/legacy/handlers.go",
		"services/legacy/db.go",
		"scripts/deploy.sh",
		"README.md",
	}
	d := Discover(DiscoverInput{Files: files, React: func(root string) bool { return root == "services/game/web" }, Packs: discoverPacks(t)})
	if len(d.Modules) != 3 || d.Loose[LanguageShell] != 1 {
		t.Fatalf("discovery = %+v", d)
	}
	api := d.Modules[0]
	if api.Root != "services/game/api" || !slices.Equal(api.Markers, []string{"go.mod"}) || api.Files[LanguageGo] != 4 || api.Tests != 1 {
		t.Fatalf("api module = %+v", api)
	}
	// Tests do not count; main.go is the composition root, part of the layout.
	if len(api.Fits) != 1 || api.Fits[0].InLayer != 3 || api.Fits[0].Total != 3 || !api.Fits[0].Fits() {
		t.Fatalf("api fits = %+v", api.Fits)
	}
	got := d.Suggest()
	if len(got) != 4 || got[3].Paths[0] != "scripts" || !slices.Equal(got[3].Languages, []string{"shell"}) {
		t.Fatalf("suggestion = %+v", got)
	}
	if !slices.Equal(got[0].Conventions, []string{"clean-architecture"}) || got[0].Reason != "" {
		t.Errorf("api: %+v", got[0])
	}
	if got[1].Paths[0] != "services/game/web" || !slices.Equal(got[1].Conventions, []string{"react-feature-based"}) {
		t.Errorf("web: %+v", got[1])
	}
	if got[2].Paths[0] != "services/legacy" || !slices.Equal(got[2].Conventions, []string{}) ||
		!strings.Contains(got[2].Reason, "clean-architecture holds 0 of 2 go files") {
		t.Errorf("legacy: %+v", got[2])
	}
	if _, err := NewConfig(ConfigInput{Version: ptr(1), Components: got}); err != nil {
		t.Fatalf("the suggestion must be a valid config: %v", err)
	}
}

func TestDiscoverAtTheRoot(t *testing.T) {
	d := Discover(DiscoverInput{Files: []string{"go.mod", "tool.go", "deploy.sh"}, React: func(string) bool { return false }})
	got := d.Suggest()
	if loose := Discover(DiscoverInput{Files: []string{"deploy.sh"}, React: func(string) bool { return false }}); len(loose.Suggest()) != 0 || loose.Loose[LanguageShell] != 1 {
		t.Fatalf("a script at the root of a repository without modules is reported, not drafted: %+v", loose)
	}
	if len(got) != 1 || got[0].Paths[0] != "." || !slices.Equal(got[0].Languages, []string{"go", "shell"}) ||
		got[0].Reason != "no convention pack governs this module's languages" {
		t.Fatalf("suggestion = %+v", got)
	}
	empty := Discover(DiscoverInput{Files: []string{"docs/package.json"}, React: func(string) bool { return false }})
	if len(empty.Suggest()) != 0 {
		t.Fatal("a module without code needs no component")
	}
}

func TestDiscoverPrefersReactForAReactModule(t *testing.T) {
	files := []string{"app/package.json", "app/src/features/a/x.ts", "app/internal/domain/d.go"}
	d := Discover(DiscoverInput{Files: files, React: func(string) bool { return true }, Packs: discoverPacks(t)})
	got := d.Suggest()
	if len(got) != 1 || !slices.Equal(got[0].Conventions, []string{"react-feature-based"}) {
		t.Fatalf("a React module gets the React pack even where clean-architecture fits its Go files: %+v", got)
	}
}
