package domain

import (
	"strings"
	"testing"
)

func TestTestingOf(t *testing.T) {
	input := cleanArchitectureInput()
	input.Layers[0].Tested = true // domain
	tested, err := NewPack(input)
	if err != nil {
		t.Fatalf("NewPack: %v", err)
	}
	untested, err := NewPack(cleanArchitectureInput())
	if err != nil {
		t.Fatalf("NewPack: %v", err)
	}
	untested.Name = "plain"
	catalog := Catalog{"clean-architecture": tested, "plain": untested}
	component := func(conventions ...Convention) ComponentMatch {
		return ComponentMatch{Component: Component{Patterns: []Pattern{RootPattern()}, Conventions: conventions}, Root: "svc"}
	}
	tests := []struct {
		name  string
		lang  Language
		match ComponentMatch
		path  string
		want  LayerTesting
	}{
		{"tested layer", LanguageGo, component("clean-architecture"), "svc/internal/domain/a.go", LayerTested},
		{"untested layer", LanguageGo, component("clean-architecture"), "svc/internal/infrastructure/a.go", LayerUntested},
		{"in no layer", LanguageGo, component("clean-architecture"), "svc/tools/a.go", LayerUntested},
		{"no convention", LanguageGo, component(), "svc/internal/domain/a.go", LayerUntested},
		{"convention without a pack", LanguageGo, component("missing"), "svc/internal/domain/a.go", LayerUntested},
		{"pack without paths for the language", LanguagePython, component("clean-architecture"), "svc/internal/domain/a.py", LayerUntested},
		{"a later convention tests it", LanguageGo, component("missing", "plain", "clean-architecture"), "svc/internal/domain/a.go", LayerTested},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := catalog.TestingOf(tt.lang, tt.match, tt.path); got != tt.want {
				t.Fatalf("TestingOf(%s) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// TestGeneratedMarkerExemptsOnlyOutsideTestedLayers: the marker is file
// content an agent controls, so in a tested layer it exempts nothing and the
// finding says why (SR-17, AC-14).
func TestGeneratedMarkerExemptsOnlyOutsideTestedLayers(t *testing.T) {
	marked, unmarked := FileFacts{Generated: true}, FileFacts{}
	tests := []struct {
		name    string
		testing LayerTesting
		file    FileFacts
		exempt  bool
		note    bool
	}{
		{"marked outside tested layers", LayerUntested, marked, true, false},
		{"marked in a tested layer", LayerTested, marked, false, true},
		{"marked in a layer of unknown testing", "", marked, false, true},
		{"unmarked outside tested layers", LayerUntested, unmarked, false, false},
		{"unmarked in a tested layer", LayerTested, unmarked, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.testing.MarkerExempts(tt.file); got != tt.exempt {
				t.Fatalf("MarkerExempts = %v, want %v", got, tt.exempt)
			}
			note := tt.testing.MarkerNote(tt.file)
			if (note != "") != tt.note {
				t.Fatalf("MarkerNote = %q, want a note: %v", note, tt.note)
			}
			if tt.note && (!strings.HasPrefix(note, "; the generated-code marker does not exempt files in a tested layer") || !strings.Contains(note, `"exclude" in the config`)) {
				t.Fatalf("MarkerNote = %q, want it to name the rule and point to exclude", note)
			}
		})
	}
}

// TestNeedsTestsIgnoresTheGeneratedMarker: in a tested layer, where
// NeedsTests is asked, a marked file with logic needs tests like any other
// (AC-11, SR-17).
func TestNeedsTestsIgnoresTheGeneratedMarker(t *testing.T) {
	if !NeedsTests(FileFacts{Generated: true, Functions: []FunctionFacts{{Key: "f"}}}) {
		t.Fatal("a generated-code marker took logic out of the tests gate")
	}
}
