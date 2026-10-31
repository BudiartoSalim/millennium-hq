package gate

import (
	"context"
	"path"

	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/domain"
)

// suite is one test-suite run the change needs.
type suite struct {
	kind         string
	dir          string
	component    string
	lang         domain.Language
	recipe       *domain.TestRecipe
	unconfigured bool
}

func (s suite) label() string {
	if s.dir == "" {
		return "the repository root"
	}
	return s.dir
}

func (s suite) run(ctx context.Context, runner port.TestRunner) (port.TestResult, error) {
	switch s.kind {
	case "recipe":
		return runner.Command(ctx, s.dir, *s.recipe)
	case "go":
		return runner.GoTest(ctx, s.dir)
	}
	return runner.PythonTest(ctx, s.dir)
}

// suites plans one run per touched Go module, per Python component, and per
// component with its own recipe. A TypeScript component without a recipe
// cannot be tested and is reported.
func (g *Tests) suites(in port.GateInput) []suite {
	seen := map[string]bool{}
	var out []suite
	add := func(s suite) {
		key := s.kind + "|" + s.dir + "|" + string(s.lang)
		if !seen[key] {
			seen[key] = true
			out = append(out, s)
		}
	}
	for _, file := range in.Changes.Files {
		lang, match, ok := ownedCode(in.Config, file)
		if !ok {
			continue
		}
		if s, ok := g.suiteFor(lang, match, file.Path); ok {
			add(s)
		}
	}
	return out
}

func (g *Tests) suiteFor(lang domain.Language, match domain.ComponentMatch, file string) (suite, bool) {
	base := suite{component: match.Component.Name(), lang: lang, dir: match.Root}
	switch {
	case match.Component.Test != nil:
		base.kind, base.recipe = "recipe", match.Component.Test
		return base, true
	case lang == domain.LanguageGo:
		dir := path.Dir(file)
		if dir == "." {
			dir = ""
		}
		module, ok := g.finder.NearestUp(dir, "go.mod", match.Root)
		base.kind, base.dir = "go", module
		return base, ok
	case lang == domain.LanguagePython:
		base.kind = "python"
		return base, true
	}
	base.kind, base.unconfigured = "none", true
	return base, true
}
