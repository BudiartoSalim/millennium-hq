package domain

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// CompositionRoot is the reserved layer name for the code that wires
// everything together (main.go). It may import every layer, and no layer may
// import it.
const CompositionRoot = "composition"

// PackLayer is one layer of a convention and what it may depend on.
type PackLayer struct {
	Name      string
	MayImport []string
	// ThirdParty allows imports of external dependencies.
	ThirdParty bool
}

// Pack is a validated convention pack: layers, their dependency rules, and
// where each layer lives per language. Build one with NewPack.
type Pack struct {
	Name   Convention
	Layers []PackLayer
	// Paths maps a language to layer name to path patterns relative to the
	// component root.
	Paths map[Language]map[string][]Pattern
}

// PackInput is an unvalidated pack as read from its definition file.
type PackInput struct {
	Name   string
	Layers []PackLayer
	Paths  map[string]map[string][]string
}

// ErrLanguageNotSupported means a pack has no layer paths for a language yet.
var ErrLanguageNotSupported = errors.New("the convention has no rules for this language yet")

// NewPack validates in. Packs ship inside the engine, so an invalid one is an
// engine bug, but it is reported like any config problem.
func NewPack(in PackInput) (Pack, error) {
	v := &validator{}
	name := parseChoice(v, "name", &in.Name, Conventions(), "")
	known := v.packLayers(in.Layers)
	paths := make(map[Language]map[string][]Pattern, len(in.Paths))
	for rawLang, layers := range in.Paths {
		lang := parseChoice(v, "paths", &rawLang, Languages(), "")
		paths[lang] = v.layerPaths("paths."+rawLang, layers, known)
	}
	if len(v.problems) > 0 {
		return Pack{}, fmt.Errorf("pack %q: %w", in.Name, &ConfigError{Problems: v.problems})
	}
	return Pack{Name: name, Layers: in.Layers, Paths: paths}, nil
}

func (v *validator) packLayers(layers []PackLayer) []string {
	var names []string
	for _, layer := range layers {
		switch {
		case layer.Name == "" || layer.Name == CompositionRoot:
			v.addf("layers: %q is not a usable layer name", layer.Name)
		case slices.Contains(names, layer.Name):
			v.addf("layers: %q is defined twice", layer.Name)
		default:
			names = append(names, layer.Name)
		}
	}
	for _, layer := range layers {
		for _, dep := range layer.MayImport {
			if !slices.Contains(names, dep) {
				v.addf("layers.%s.mayImport: %q is not a layer of this pack", layer.Name, dep)
			}
		}
	}
	return names
}

func (v *validator) layerPaths(field string, raw map[string][]string, known []string) map[string][]Pattern {
	out := make(map[string][]Pattern, len(raw))
	for name, patterns := range raw {
		if name != CompositionRoot && !slices.Contains(known, name) {
			v.addf("%s: %q is not a layer of this pack", field, name)
		}
		out[name] = v.patterns(field+"."+name, patterns, PrefixPattern)
	}
	return out
}

// Layering is a pack applied to one component and language: it knows which
// layer each file is in and which imports are allowed.
type Layering struct {
	pack  Pack
	root  string
	paths map[string][]Pattern
}

// For applies the pack to a component's files of lang, with the component's
// layer overrides on top of the pack's paths.
func (p Pack) For(lang Language, match ComponentMatch) (Layering, error) {
	base, ok := p.Paths[lang]
	if !ok {
		return Layering{}, fmt.Errorf("%s for %s: %w", p.Name, lang, ErrLanguageNotSupported)
	}
	paths := make(map[string][]Pattern, len(base))
	for name, patterns := range base {
		paths[name] = patterns
	}
	for name, patterns := range match.Component.Layers {
		if name != CompositionRoot && p.layer(name) == nil {
			return Layering{}, fmt.Errorf("component %s overrides layer %q, which %s does not define", match.Component.Name(), name, p.Name)
		}
		paths[name] = patterns
	}
	return Layering{pack: p, root: match.Root, paths: paths}, nil
}

func (p Pack) layer(name string) *PackLayer {
	for i := range p.Layers {
		if p.Layers[i].Name == name {
			return &p.Layers[i]
		}
	}
	return nil
}

// LayerOf names the layer repoPath belongs to, CompositionRoot, or "" when it
// is in none or outside the component.
func (l Layering) LayerOf(repoPath string) string {
	if !Within(repoPath, l.root) {
		return ""
	}
	rel := Relative(repoPath, l.root)
	for _, name := range l.order() {
		for _, pattern := range l.paths[name] {
			if _, ok := pattern.MatchPrefix(rel); ok {
				return name
			}
		}
	}
	return ""
}

// order is the pack's layer order with the composition root last, so the
// result never depends on map iteration.
func (l Layering) order() []string {
	names := make([]string, 0, len(l.pack.Layers)+1)
	for _, layer := range l.pack.Layers {
		names = append(names, layer.Name)
	}
	return append(names, CompositionRoot)
}

// CheckImports reports the imports of file that break the layer rules. When
// base, the file before the change, is given, only imports it did not have
// are checked, so existing code is held to the rules once it is touched.
func (l Layering) CheckImports(file FileFacts, base *FileFacts) []Finding {
	from := l.LayerOf(file.Path)
	if from == "" || from == CompositionRoot {
		return nil
	}
	rule := l.pack.layer(from)
	existing := importSet(base)
	var findings []Finding
	for _, imp := range file.Imports {
		if existing[imp.Path] {
			continue
		}
		if msg := l.violation(*rule, imp); msg != "" {
			findings = append(findings, Finding{Rule: "layer-import", File: file.Path, Line: imp.Line, Message: msg})
		}
	}
	return findings
}

func (l Layering) violation(rule PackLayer, imp Import) string {
	if imp.Kind == ImportStandard {
		return ""
	}
	target := ""
	if imp.Kind == ImportProject {
		target = l.LayerOf(imp.Target)
	}
	if imp.Kind == ImportExternal || !Within(imp.Target, l.root) {
		if rule.ThirdParty {
			return ""
		}
		return fmt.Sprintf("%s code must not import %q: it is outside this component, and %s allows no third-party or outside imports", rule.Name, imp.Path, rule.Name)
	}
	switch {
	case target == rule.Name || slices.Contains(rule.MayImport, target):
		return ""
	case target == "":
		return fmt.Sprintf("%s code must not import %q: it is in no layer of %s", rule.Name, imp.Path, l.pack.Name)
	case target == CompositionRoot:
		return fmt.Sprintf("%s code must not import the composition root %q", rule.Name, imp.Path)
	}
	return fmt.Sprintf("%s code must not import %s code (%q); %s may import %s", rule.Name, target, imp.Path, rule.Name, allowedList(rule))
}

func allowedList(rule PackLayer) string {
	if len(rule.MayImport) == 0 {
		return "no other layer"
	}
	return strings.Join(rule.MayImport, ", ")
}

func importSet(file *FileFacts) map[string]bool {
	if file == nil {
		return nil
	}
	set := make(map[string]bool, len(file.Imports))
	for _, imp := range file.Imports {
		set[imp.Path] = true
	}
	return set
}
