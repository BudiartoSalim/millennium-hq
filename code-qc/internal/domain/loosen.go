package domain

import (
	"fmt"
	"slices"
)

// Loosenings lists every way next checks less than current: a language,
// component, convention or test recipe gone, more paths excluded, a check
// disabled, an optional checker switched off, component or acceptance-test
// paths moved. `config apply` shows these before it asks for confirmation,
// so a weaker rule set is never applied by accident. A first config
// loosens nothing.
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
	out = append(out, componentLoosenings(current.Components, next)...)
	for _, p := range next.Exclude {
		if !slices.ContainsFunc(current.Exclude, func(c Pattern) bool { return c.String() == p.String() }) {
			out = append(out, fmt.Sprintf("%s is newly excluded from every check", p))
		}
	}
	for _, d := range next.Disabled {
		if !slices.ContainsFunc(current.Disabled, func(c DisabledCheck) bool { return c.Check == d.Check }) {
			out = append(out, fmt.Sprintf("%s is newly disabled: %s", d.Check, d.Reason))
		}
	}
	return append(out, settingLoosenings(*current, next)...)
}

func componentLoosenings(current []Component, next Config) []string {
	var out []string
	for _, c := range current {
		i := slices.IndexFunc(next.Components, func(n Component) bool { return n.Name() == c.Name() })
		if i < 0 {
			out = append(out, fmt.Sprintf("component %s is removed", c.Name()))
			continue
		}
		out = append(out, changedComponent(c, next.Components[i], next)...)
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
