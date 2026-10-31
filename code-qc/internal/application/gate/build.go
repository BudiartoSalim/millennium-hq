package gate

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"

	"millennium-code-qc/internal/application/port"
	"millennium-code-qc/internal/domain"
)

// Build runs each language's own formatter, compiler and type checker on the
// modules and projects a change touches: gofmt on the changed Go files and go
// vet on their modules, tsc on the TypeScript projects (whose tsconfig must
// keep strict mode on), and mypy when the config asks for it. Compile and
// type errors anywhere in a touched module block, because the change may be
// what broke them.
type Build struct {
	tools  port.BuildTools
	finder port.FileFinder
}

// NewBuild returns the build gate.
func NewBuild(tools port.BuildTools, finder port.FileFinder) *Build {
	return &Build{tools: tools, finder: finder}
}

// Rules lists the rules the gate reports.
func (*Build) Rules() []port.Rule {
	return []port.Rule{
		{Name: "gofmt", Disableable: true},
		{Name: "vet", Disableable: true},
		{Name: "type-error", Disableable: true},
		{Name: "tsconfig-weakened", Disableable: true},
		{Name: "tsconfig-missing", Disableable: false},
		{Name: "tool-failed", Disableable: false},
	}
}

// Name identifies the gate in findings.
func (*Build) Name() string { return "build" }

type buildPlan struct {
	goModules  map[string][]string
	tsProjects map[string]bool
	mypy       map[string][]string
	findings   []domain.Finding
}

// Run plans what to build from the changed files, then runs each tool once
// per module or project.
func (g *Build) Run(ctx context.Context, in port.GateInput) ([]domain.Finding, error) {
	plan := g.plan(in)
	findings := plan.findings
	for _, module := range sortedKeys(plan.goModules) {
		findings = append(findings, g.goModule(ctx, module, plan.goModules[module])...)
	}
	for _, project := range sortedKeys(plan.tsProjects) {
		findings = append(findings, g.tsProject(ctx, project)...)
	}
	for _, dir := range sortedKeys(plan.mypy) {
		findings = append(findings, g.toolIssues("mypy", "type-error", func() ([]domain.LintIssue, error) {
			return g.tools.Mypy(ctx, dir, plan.mypy[dir])
		})...)
	}
	return label(g.Name(), findings), nil
}

func (g *Build) plan(in port.GateInput) buildPlan {
	plan := buildPlan{goModules: map[string][]string{}, tsProjects: map[string]bool{}, mypy: map[string][]string{}}
	for _, file := range in.Changes.Files {
		lang, ok := domain.LanguageOf(file.Path)
		match, owned, err := in.Config.ComponentFor(file.Path)
		if file.Kind == domain.ChangeDeleted || !ok || err != nil || !owned || !match.Component.Has(lang) {
			continue
		}
		g.addToPlan(&plan, in.Config, lang, match, file.Path)
	}
	return plan
}

func (g *Build) addToPlan(plan *buildPlan, cfg domain.Config, lang domain.Language, match domain.ComponentMatch, file string) {
	dir := path.Dir(file)
	if dir == "." {
		dir = ""
	}
	switch lang {
	case domain.LanguageGo:
		// A Go file without go.mod is reported by the structure gate.
		if module, ok := g.finder.NearestUp(dir, "go.mod", match.Root); ok {
			plan.goModules[module] = append(plan.goModules[module], file)
		}
	case domain.LanguageTypeScript:
		project, ok := g.finder.NearestUp(dir, "tsconfig.json", match.Root)
		if !ok {
			plan.findings = append(plan.findings, domain.Finding{Rule: "tsconfig-missing", File: file, Message: fmt.Sprintf(
				"no tsconfig.json at or above %s inside component %s, so it cannot be type-checked", file, match.Component.Name())})
			return
		}
		plan.tsProjects[project] = true
	case domain.LanguagePython:
		if cfg.PythonTypeCheck == domain.PythonTypeCheckMypy {
			plan.mypy[match.Root] = append(plan.mypy[match.Root], file)
		}
	}
}

func (g *Build) goModule(ctx context.Context, module string, files []string) []domain.Finding {
	var findings []domain.Finding
	unformatted, err := g.tools.Gofmt(ctx, files)
	if err != nil {
		return []domain.Finding{toolFailed("gofmt", err)}
	}
	for _, file := range unformatted {
		findings = append(findings, domain.Finding{Rule: "gofmt", File: file, Message: "not gofmt-formatted; run gofmt -w " + file})
	}
	return append(findings, g.toolIssues("go vet", "vet", func() ([]domain.LintIssue, error) {
		return g.tools.GoVet(ctx, module)
	})...)
}

func (g *Build) tsProject(ctx context.Context, project string) []domain.Finding {
	findings := g.toolIssues("tsc", "type-error", func() ([]domain.LintIssue, error) {
		return g.tools.TypeCheck(ctx, project)
	})
	config, err := g.tools.TSConfig(ctx, project)
	if err != nil {
		return append(findings, toolFailed("tsc --showConfig", err))
	}
	problems, err := domain.TSConfigProblems(config)
	if err != nil {
		return append(findings, toolFailed("tsc --showConfig", err))
	}
	if len(problems) > 0 {
		findings = append(findings, domain.Finding{Rule: "tsconfig-weakened", File: path.Join(project, "tsconfig.json"), Message: fmt.Sprintf(
			"the effective tsconfig weakens type checking: %s", strings.Join(problems, "; "))})
	}
	return findings
}

func (g *Build) toolIssues(tool, rule string, run func() ([]domain.LintIssue, error)) []domain.Finding {
	issues, err := run()
	if err != nil {
		return []domain.Finding{toolFailed(tool, err)}
	}
	findings := make([]domain.Finding, 0, len(issues))
	for _, issue := range issues {
		message := issue.Message
		if issue.Code != "" && issue.Code != rule {
			message = issue.Code + ": " + message
		}
		findings = append(findings, domain.Finding{Rule: rule, File: issue.File, Line: issue.Line, Message: message})
	}
	return findings
}

func toolFailed(tool string, err error) domain.Finding {
	return domain.Finding{Rule: "tool-failed", Message: fmt.Sprintf("%s: %v", tool, err)}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
