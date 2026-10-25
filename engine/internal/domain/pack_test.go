package domain

import (
	"errors"
	"strings"
	"testing"
)

func cleanArchitectureInput() PackInput {
	return PackInput{
		Name:      "clean-architecture",
		Languages: []string{"go", "python"},
		Layers: []PackLayer{
			{Name: "domain"},
			{Name: "application", MayImport: []string{"domain"}},
			{Name: "infrastructure", MayImport: []string{"domain", "application"}, ThirdParty: true},
			{Name: "interface", MayImport: []string{"domain", "application"}, ThirdParty: true},
		},
		Paths: map[string]map[string][]string{"go": {
			"domain":         {"internal/domain"},
			"application":    {"internal/application"},
			"infrastructure": {"internal/infrastructure"},
			"interface":      {"internal/interface"},
			"composition":    {"main.go", "cmd"},
		}},
	}
}

func goLayering(t *testing.T, root string, layers map[string][]Pattern) Layering {
	t.Helper()
	pack, err := NewPack(cleanArchitectureInput())
	if err != nil {
		t.Fatalf("NewPack: %v", err)
	}
	match := ComponentMatch{Component: Component{Patterns: []Pattern{RootPattern()}, Layers: layers}, Root: root}
	layering, err := pack.For(LanguageGo, match)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	return layering
}

func TestNewPackRejects(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*PackInput)
		want   string
	}{
		{"unknown convention", func(in *PackInput) { in.Name = "mvc" }, `name: "mvc"`},
		{"reserved layer name", func(in *PackInput) { in.Layers = append(in.Layers, PackLayer{Name: "composition"}) }, `"composition" is not a usable layer name`},
		{"duplicate layer", func(in *PackInput) { in.Layers = append(in.Layers, PackLayer{Name: "domain"}) }, `"domain" is defined twice`},
		{"unknown dependency", func(in *PackInput) { in.Layers[0].MayImport = []string{"web"} }, `layers.domain.mayImport: "web"`},
		{"path for unknown layer", func(in *PackInput) { in.Paths["go"]["web"] = []string{"web"} }, `"web" is not a layer`},
		{"unknown language", func(in *PackInput) { in.Paths["rust"] = map[string][]string{} }, `paths: "rust"`},
		{"governs nothing", func(in *PackInput) { in.Languages = nil }, "at least one language"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := cleanArchitectureInput()
			tt.modify(&in)
			if _, err := NewPack(in); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestPackForUnsupportedLanguageAndBadOverride(t *testing.T) {
	pack, err := NewPack(cleanArchitectureInput())
	if err != nil {
		t.Fatalf("NewPack: %v", err)
	}
	if _, err := pack.For(LanguagePython, ComponentMatch{Component: Component{Patterns: []Pattern{RootPattern()}}}); !errors.Is(err, ErrLanguageNotSupported) {
		t.Fatalf("err = %v, want ErrLanguageNotSupported", err)
	}
	if _, err := pack.For(LanguageShell, ComponentMatch{Component: Component{Patterns: []Pattern{RootPattern()}}}); !errors.Is(err, ErrLanguageNotGoverned) {
		t.Fatalf("err = %v, want ErrLanguageNotGoverned for a language outside the pack", err)
	}
	override := ComponentMatch{Component: Component{Patterns: []Pattern{RootPattern()}, Layers: map[string][]Pattern{"web": nil}}}
	if _, err := pack.For(LanguageGo, override); err == nil || !strings.Contains(err.Error(), `overrides layer "web"`) {
		t.Fatalf("err = %v, want an unknown-layer error", err)
	}
}

func TestLayerOf(t *testing.T) {
	l := goLayering(t, "services/game/api", nil)
	tests := map[string]string{
		"services/game/api/internal/domain/user.go":             "domain",
		"services/game/api/internal/application/usecase/x.go":   "application",
		"services/game/api/internal/interface/web":              "interface",
		"services/game/api/main.go":                             CompositionRoot,
		"services/game/api/cmd/tool/main.go":                    CompositionRoot,
		"services/game/api/internal/platform/log.go":            "",
		"services/other/api/internal/domain/user.go":            "",
		"services/game/api/internal/domainx/user.go":            "",
		"services/game/api/internal/infrastructure/valkey/c.go": "infrastructure",
	}
	for p, want := range tests {
		if got := l.LayerOf(p); got != want {
			t.Errorf("LayerOf(%q) = %q, want %q", p, got, want)
		}
	}
}

func TestLayerOfWithOverride(t *testing.T) {
	domain := mustPattern(t, "*/domain.py", PrefixPattern)
	l := goLayering(t, "tts", map[string][]Pattern{"domain": {domain}})
	if got := l.LayerOf("tts/kokoro/domain.py"); got != "domain" {
		t.Fatalf("override not applied: %q", got)
	}
	if got := l.LayerOf("tts/internal/domain/x.go"); got != "" {
		t.Fatalf("override must replace the pack's paths, got %q", got)
	}
}

func projectImport(path, target string) Import {
	return Import{Path: path, Line: 3, Kind: ImportProject, Target: target}
}

func TestCheckImports(t *testing.T) {
	l := goLayering(t, "", nil)
	tests := []struct {
		name string
		file string
		imp  Import
		want string
	}{
		{"standard library", "internal/domain/a.go", Import{Path: "fmt", Kind: ImportStandard}, ""},
		{"domain to third party", "internal/domain/a.go", Import{Path: "github.com/x/y", Kind: ImportExternal}, "allows no third-party"},
		{"infrastructure to third party", "internal/infrastructure/db/a.go", Import{Path: "github.com/x/y", Kind: ImportExternal}, ""},
		{"same layer", "internal/application/usecase/a.go", projectImport("m/internal/application/port", "internal/application/port"), ""},
		{"inward", "internal/interface/cli/a.go", projectImport("m/internal/application/usecase", "internal/application/usecase"), ""},
		{"outward", "internal/application/usecase/a.go", projectImport("m/internal/infrastructure/db", "internal/infrastructure/db"), "application code must not import infrastructure code"},
		{"domain to application", "internal/domain/a.go", projectImport("m/internal/application/port", "internal/application/port"), "domain may import no other layer"},
		{"sideways", "internal/interface/cli/a.go", projectImport("m/internal/infrastructure/db", "internal/infrastructure/db"), "interface may import domain, application"},
		{"unlayered target", "internal/domain/a.go", projectImport("m/internal/platform", "internal/platform"), "in no layer"},
		{"composition may import anything", "main.go", projectImport("m/internal/infrastructure/db", "internal/infrastructure/db"), ""},
		{"unlayered file is unrestricted", "internal/platform/a.go", projectImport("m/internal/infrastructure/db", "internal/infrastructure/db"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := l.CheckImports(FileFacts{Path: tt.file, Imports: []Import{tt.imp}}, nil)
			switch {
			case tt.want == "" && len(findings) > 0:
				t.Fatalf("want no findings, got %+v", findings)
			case tt.want != "" && (len(findings) != 1 || !strings.Contains(findings[0].Message, tt.want)):
				t.Fatalf("want one finding containing %q, got %+v", tt.want, findings)
			case tt.want != "" && (findings[0].Rule != "layer-import" || findings[0].Line != tt.imp.Line):
				t.Fatalf("finding = %+v", findings[0])
			}
		})
	}
}

func TestCheckImportsOutsideComponentCountsAsExternal(t *testing.T) {
	l := goLayering(t, "services/game/api", nil)
	shared := projectImport("m/shared/auth", "shared/auth")
	if f := l.CheckImports(FileFacts{Path: "services/game/api/internal/domain/a.go", Imports: []Import{shared}}, nil); len(f) != 1 {
		t.Fatalf("domain importing code outside its component must be blocked, got %+v", f)
	}
	if f := l.CheckImports(FileFacts{Path: "services/game/api/internal/infrastructure/a.go", Imports: []Import{shared}}, nil); len(f) != 0 {
		t.Fatalf("infrastructure may import outside code, got %+v", f)
	}
}

func TestCheckImportsOnlyChecksNewImportsOfExistingFiles(t *testing.T) {
	l := goLayering(t, "", nil)
	legacy := projectImport("m/internal/infrastructure/db", "internal/infrastructure/db")
	added := projectImport("m/internal/infrastructure/cache", "internal/infrastructure/cache")
	base := &FileFacts{Path: "internal/application/a.go", Imports: []Import{legacy}}
	file := FileFacts{Path: "internal/application/a.go", Imports: []Import{legacy, added}}
	findings := l.CheckImports(file, base)
	if len(findings) != 1 || !strings.Contains(findings[0].Message, "cache") {
		t.Fatalf("want only the added import flagged, got %+v", findings)
	}
	if all := l.CheckImports(file, nil); len(all) != 2 {
		t.Fatalf("without a base every import is checked, got %+v", all)
	}
}
