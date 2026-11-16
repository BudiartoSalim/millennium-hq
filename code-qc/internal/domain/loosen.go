package domain

import (
	"fmt"
	"slices"
)

// Loosenings lists every way next checks less than current: a language,
// component, convention or test recipe gone, more paths excluded, a check
// disabled, an optional checker switched off, component or acceptance-test
// paths moved, a limit raised, a convention pack's rules weakened. `config
// apply` shows these before it asks for confirmation, so a weaker rule set
// is never applied by accident. A first config loosens nothing. Both
// configs must be resolved, so their packs can be compared.
func Loosenings(current *Config, next Config) []string {
	if current == nil {
		return nil
	}
	var out []string
	for _, lang := range current.Languages {
		if !next.Has(lang) {
			out = append(out, fmt.Sprintf("language %s is no longer checked", lang))
		}
	}
	out = append(out, componentLoosenings(*current, next)...)
	for _, p := range next.Exclude {
		if !slices.ContainsFunc(current.Exclude, func(c Pattern) bool { return c.String() == p.String() }) {
			out = append(out, fmt.Sprintf("%s is newly excluded from every check", p))
		}
	}
	out = append(out, disableLoosenings(current.Disabled, next.Disabled)...)
	out = append(out, settingLoosenings(*current, next)...)
	for _, name := range current.Packs.Names() {
		if pack, ok := next.Packs[name]; ok {
			out = append(out, packLoosenings(current.Packs[name], pack)...)
		}
	}
	return out
}

func componentLoosenings(current Config, next Config) []string {
	var out []string
	for _, c := range current.Components {
		i := slices.IndexFunc(next.Components, func(n Component) bool { return n.Name() == c.Name() })
		if i < 0 {
			out = append(out, fmt.Sprintf("component %s is removed", c.Name()))
			continue
		}
		out = append(out, changedComponent(c, next.Components[i], next)...)
		for _, raised := range current.LimitsFor(c).Raised(next.LimitsFor(next.Components[i])) {
			out = append(out, fmt.Sprintf("component %s: %s", c.Name(), raised))
		}
	}
	return out
}

// changedComponent lists what one component loses. A language dropped from
// the whole config is already reported, so only a language other components
// still check is reported here.
func changedComponent(c, n Component, next Config) []string {
	var out []string
	for _, lang := range c.Languages {
		if !n.Has(lang) && next.Has(lang) {
			out = append(out, fmt.Sprintf("component %s no longer checks %s", c.Name(), lang))
		}
	}
	for _, conv := range c.Conventions {
		if !slices.Contains(n.Conventions, conv) {
			out = append(out, fmt.Sprintf("component %s drops convention %s", c.Name(), conv))
		}
	}
	if patternsKey(c.Patterns) != patternsKey(n.Patterns) {
		out = append(out, fmt.Sprintf("component %s now covers %s instead of %s", c.Name(), patternsKey(n.Patterns), patternsKey(c.Patterns)))
	}
	if len(n.Layers) > 0 && fmt.Sprint(c.Layers) != fmt.Sprint(n.Layers) {
		out = append(out, fmt.Sprintf("component %s changes its layer paths", c.Name()))
	}
	if c.Test != nil && n.Test == nil {
		out = append(out, fmt.Sprintf("component %s drops its test recipe", c.Name()))
	}
	return out
}

func settingLoosenings(current, next Config) []string {
	var out []string
	if current.PythonTypeCheck == PythonTypeCheckMypy && next.PythonTypeCheck != PythonTypeCheckMypy {
		out = append(out, "Python type checking (mypy) is switched off")
	}
	if current.ShellLint == ShellLintShellcheck && next.ShellLint != ShellLintShellcheck {
		out = append(out, "shellcheck is switched off")
	}
	if current.AcceptanceTests != next.AcceptanceTests {
		out = append(out, fmt.Sprintf("acceptance tests move from %s to %s, so the old ones lose their protection", current.AcceptanceTests, next.AcceptanceTests))
	}
	return out
}

func patternsKey(patterns []Pattern) string {
	names := make([]string, len(patterns))
	for i, p := range patterns {
		names[i] = p.String()
	}
	return fmt.Sprint(names)
}

// packLoosenings lists what a convention pack of the same name loses:
// languages, layers, import restrictions, required tests, isolation, or its
// layer paths. Raised limits are reported per component, where they apply.
func packLoosenings(current, next Pack) []string {
	var out []string
	for _, lang := range current.Languages {
		if !next.Governs(lang) {
			out = append(out, fmt.Sprintf("convention %s no longer governs %s", current.Name, lang))
		}
	}
	for _, layer := range current.Layers {
		after := next.layer(layer.Name)
		if after == nil {
			out = append(out, fmt.Sprintf("convention %s removes layer %s", current.Name, layer.Name))
			continue
		}
		out = append(out, layerLoosenings(current.Name, layer, *after)...)
	}
	if fmt.Sprint(current.Paths) != fmt.Sprint(next.Paths) {
		out = append(out, fmt.Sprintf("convention %s changes its layer paths", current.Name))
	}
	return out
}

func layerLoosenings(pack Convention, before, after PackLayer) []string {
	var out []string
	for _, dep := range after.MayImport {
		if !slices.Contains(before.MayImport, dep) {
			out = append(out, fmt.Sprintf("convention %s: layer %s may now import %s", pack, before.Name, dep))
		}
	}
	flags := []struct {
		lost bool
		what string
	}{
		{!before.ThirdParty && after.ThirdParty, "may now import third-party code"},
		{before.Tested && !after.Tested, "no longer needs tests"},
		{before.Isolated && !after.Isolated, "is no longer isolated"},
	}
	for _, f := range flags {
		if f.lost {
			out = append(out, fmt.Sprintf("convention %s: layer %s %s", pack, before.Name, f.what))
		}
	}
	return out
}
