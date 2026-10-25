package domain

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"sort"
)

// Component is one part of a repository with its own languages and
// conventions: a service in a monorepo, or the whole repository.
type Component struct {
	Patterns    []Pattern
	Languages   []Language
	Conventions []Convention
	// Layers overrides a convention's layer paths for this component,
	// relative to the component root, keyed by layer name.
	Layers map[string][]Pattern
	// Reason explains a deliberate exception, such as a legacy component
	// with no conventions.
	Reason string
}

// ComponentInput is an unvalidated component as read from a file.
type ComponentInput struct {
	Paths       []string
	Languages   []string
	Conventions []string
	Layers      map[string][]string
	Reason      string
}

// Name identifies the component in messages by its first pattern.
func (c Component) Name() string { return c.Patterns[0].String() }

// Has reports whether the component uses lang.
func (c Component) Has(lang Language) bool { return slices.Contains(c.Languages, lang) }

// ComponentMatch is the component a file belongs to and the directory its
// pattern matched, which is the root that layer paths are relative to.
type ComponentMatch struct {
	Component Component
	Root      string
}

// ErrAmbiguousComponent means two components claim a file equally; the
// config must make one pattern more specific.
var ErrAmbiguousComponent = errors.New("ambiguous component")

type componentCandidate struct {
	index    int
	root     string
	depth    int
	literals int
}

func (c componentCandidate) beats(other componentCandidate) bool {
	if c.depth != other.depth {
		return c.depth > other.depth
	}
	return c.literals > other.literals
}

// ComponentFor finds the component that owns file. The most specific
// matching pattern wins: the deepest root, then the most literal segments.
// It returns false when no component owns the file.
func (c Config) ComponentFor(file string) (ComponentMatch, bool, error) {
	var best *componentCandidate
	rival := -1
	for i, component := range c.Components {
		for _, pattern := range component.Patterns {
			root, ok := pattern.MatchPrefix(path.Dir(file))
			if !ok {
				continue
			}
			candidate := componentCandidate{index: i, root: root, depth: pattern.Depth(), literals: pattern.Literals()}
			switch {
			case best == nil || candidate.beats(*best):
				best, rival = &candidate, -1
			case !best.beats(candidate) && candidate.index != best.index:
				rival = candidate.index
			}
		}
	}
	if best == nil {
		return ComponentMatch{}, false, nil
	}
	if rival >= 0 {
		return ComponentMatch{}, false, fmt.Errorf("%w: %s matches components %q and %q equally; make one pattern more specific",
			ErrAmbiguousComponent, file, c.Components[best.index].Name(), c.Components[rival].Name())
	}
	return ComponentMatch{Component: c.Components[best.index], Root: best.root}, true, nil
}

// components validates the component list, or turns the single-project form
// into one component at the repository root.
func (v *validator) components(in ConfigInput) []Component {
	if in.Components == nil {
		if len(in.Languages) == 0 {
			v.add("languages: at least one language is required, or list components")
		}
		return []Component{v.component("", ComponentInput{
			Paths: []string{"."}, Languages: in.Languages, Conventions: in.Conventions,
		})}
	}
	if in.Languages != nil || in.Conventions != nil {
		v.add("languages and conventions belong inside each component when components are listed")
	}
	if len(in.Components) == 0 {
		v.add("components: at least one component is required")
	}
	out := make([]Component, 0, len(in.Components))
	for i, component := range in.Components {
		out = append(out, v.component(fmt.Sprintf("components[%d].", i), component))
	}
	return out
}

func (v *validator) component(prefix string, in ComponentInput) Component {
	if len(in.Paths) == 0 {
		v.addf("%spaths: at least one path is required", prefix)
	}
	if prefix != "" && len(in.Languages) == 0 {
		v.addf("%slanguages: at least one language is required", prefix)
	}
	component := Component{
		Patterns:    v.patterns(prefix+"paths", in.Paths, PrefixPattern),
		Languages:   parseList(v, prefix+"languages", in.Languages, Languages()),
		Conventions: parseList(v, prefix+"conventions", in.Conventions, Conventions()),
		Layers:      v.layers(prefix+"layers", in.Layers),
		Reason:      in.Reason,
	}
	// Every path was invalid and already reported; a placeholder keeps Name
	// usable while the remaining problems are collected.
	if len(component.Patterns) == 0 {
		component.Patterns = []Pattern{RootPattern()}
	}
	if slices.Contains(component.Conventions, ConventionReactFeatureBased) && !component.Has(LanguageTypeScript) {
		v.addf("%sconventions: %q needs %q in languages", prefix, ConventionReactFeatureBased, LanguageTypeScript)
	}
	return component
}

func (v *validator) layers(field string, raw map[string][]string) map[string][]Pattern {
	if len(raw) == 0 {
		return nil
	}
	names := make([]string, 0, len(raw))
	for name := range raw {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make(map[string][]Pattern, len(raw))
	for _, name := range names {
		if len(raw[name]) == 0 {
			v.addf("%s.%s: at least one path is required", field, name)
		}
		out[name] = v.patterns(field+"."+name, raw[name], PrefixPattern)
	}
	return out
}
