package domain

import (
	"fmt"
	"slices"
	"sort"
)

// Catalog is every convention pack a config can name: the packs the engine
// ships and the project's own.
type Catalog map[Convention]Pack

// NewCatalog combines the built-in packs with the project's. A project pack
// may not take a built-in pack's name: to vary a convention, copy it under
// a new name, so one pack is always the whole rule set.
func NewCatalog(builtin, project []Pack) (Catalog, error) {
	catalog := Catalog{}
	for _, pack := range builtin {
		catalog[pack.Name] = pack
	}
	return catalog.With(project)
}

// With returns the catalog with packs added, replacing project packs of the
// same name. A name the engine ships, or one listed twice, is an error.
func (c Catalog) With(packs []Pack) (Catalog, error) {
	out := make(Catalog, len(c)+len(packs))
	for name, pack := range c {
		out[name] = pack
	}
	var problems []string
	added := map[Convention]bool{}
	for _, pack := range packs {
		existing, exists := c[pack.Name]
		switch {
		case added[pack.Name]:
			problems = append(problems, fmt.Sprintf("convention %s is defined twice", pack.Name))
		case exists && existing.Origin == PackBuiltin:
			problems = append(problems, fmt.Sprintf("convention %s is built in; to change it, copy it under a new name with `millennium-code-qc convention new <name> --from %s`", pack.Name, pack.Name))
		default:
			out[pack.Name] = pack
		}
		added[pack.Name] = true
	}
	if len(problems) > 0 {
		return nil, &ConfigError{Problems: problems}
	}
	return out, nil
}

// Names lists the catalog's conventions in order.
func (c Catalog) Names() []Convention {
	names := make([]Convention, 0, len(c))
	for name := range c {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// Packs lists the catalog's packs in name order.
func (c Catalog) Packs() []Pack {
	packs := make([]Pack, 0, len(c))
	for _, name := range c.Names() {
		packs = append(packs, c[name])
	}
	return packs
}

// Resolve checks every convention the config names against catalog and
// returns the config with the packs it uses. A convention must exist, must
// govern at least one of its component's languages, and every layer a
// component maps must be a layer of one of its conventions.
func (c Config) Resolve(catalog Catalog) (Config, error) {
	used := Catalog{}
	var problems []string
	for _, component := range c.Components {
		for _, name := range component.Conventions {
			pack, ok := catalog[name]
			if !ok {
				problems = append(problems, fmt.Sprintf("component %s: convention %s does not exist; available: %s", component.Name(), name, quoteAll(catalog.Names())))
				continue
			}
			used[name] = pack
			if !slices.ContainsFunc(component.Languages, pack.Governs) {
				problems = append(problems, fmt.Sprintf("component %s: convention %s governs %s, and the component uses none of them", component.Name(), name, quoteAll(pack.Languages)))
			}
		}
		problems = append(problems, unknownLayers(component, catalog)...)
	}
	if len(problems) > 0 {
		return Config{}, &ConfigError{Problems: problems}
	}
	c.Packs = used
	return c, nil
}

func unknownLayers(component Component, catalog Catalog) []string {
	var problems []string
	names := make([]string, 0, len(component.Layers))
	for name := range component.Layers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !slices.ContainsFunc(component.Conventions, func(conv Convention) bool {
			pack, ok := catalog[conv]
			return ok && pack.HasLayer(name)
		}) {
			problems = append(problems, fmt.Sprintf("component %s: layers.%s is not a layer of its conventions (%s)", component.Name(), name, quoteAll(component.Conventions)))
		}
	}
	return problems
}

// LimitsFor returns the function limits of a component: its own limits,
// field by field, else the strictest among its conventions' packs, else the
// engine's defaults.
func (c Config) LimitsFor(component Component) Limits {
	limits := DefaultLimits()
	first := true
	for _, name := range component.Conventions {
		pack, ok := c.Packs[name]
		if !ok {
			continue
		}
		if first {
			limits, first = pack.Limits, false
			continue
		}
		limits = limits.Strictest(pack.Limits)
	}
	if component.Limits != nil {
		limits = limits.With(component.Limits.Values)
	}
	return limits
}

// Guides lists the packs the config uses, in name order, for the agents'
// briefing.
func (c Config) Guides() []Pack { return c.Packs.Packs() }
