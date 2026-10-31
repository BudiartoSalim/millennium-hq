package configfile

import "millennium-code-qc/internal/domain"

// configDTO mirrors the file format. Pointers and nil slices tell an absent
// setting (take the default) from an explicit one (validate it). The JSON
// Schema in schema/millennium.schema.json must list exactly these fields.
type configDTO struct {
	Schema           string         `json:"$schema"`
	Version          *int           `json:"version"`
	Languages        []string       `json:"languages"`
	Conventions      []string       `json:"conventions"`
	Components       []componentDTO `json:"components"`
	Test             *testDTO       `json:"test"`
	Exclude          []string       `json:"exclude"`
	Disable          []disableDTO   `json:"disable"`
	Testing          *string        `json:"testing"`
	DependencyPolicy *string        `json:"dependencyPolicy"`
	Python           *pythonDTO     `json:"python"`
	Shell            *shellDTO      `json:"shell"`
	Paths            *pathsDTO      `json:"paths"`
	Run              *runDTO        `json:"run"`
}

type componentDTO struct {
	Paths       []string            `json:"paths"`
	Languages   []string            `json:"languages"`
	Conventions []string            `json:"conventions"`
	Layers      map[string][]string `json:"layers"`
	Reason      string              `json:"reason"`
	Test        *testDTO            `json:"test"`
}

type disableDTO struct {
	Check  string `json:"check"`
	Reason string `json:"reason"`
}

type testDTO struct {
	Command []string `json:"command"`
	LCOV    string   `json:"lcov"`
}

func (t *testDTO) toDomain() *domain.TestRecipe {
	if t == nil {
		return nil
	}
	return &domain.TestRecipe{Command: t.Command, LCOV: t.LCOV}
}

type pythonDTO struct {
	TypeCheck *string `json:"typeCheck"`
}

type shellDTO struct {
	Lint *string `json:"lint"`
}

type pathsDTO struct {
	AcceptanceTests *string `json:"acceptanceTests"`
}

type runDTO struct {
	Start     string `json:"start"`
	HealthURL string `json:"healthUrl"`
}

func disabledChecks(in []disableDTO) []domain.DisabledCheck {
	out := make([]domain.DisabledCheck, len(in))
	for i, d := range in {
		out[i] = domain.DisabledCheck(d)
	}
	return out
}

func (d configDTO) toInput() domain.ConfigInput {
	in := domain.ConfigInput{
		Version:          d.Version,
		Languages:        d.Languages,
		Conventions:      d.Conventions,
		Exclude:          d.Exclude,
		Test:             d.Test.toDomain(),
		Disable:          disabledChecks(d.Disable),
		Testing:          d.Testing,
		DependencyPolicy: d.DependencyPolicy,
	}
	if d.Components != nil {
		in.Components = make([]domain.ComponentInput, len(d.Components))
		for i, c := range d.Components {
			in.Components[i] = domain.ComponentInput{
				Paths: c.Paths, Languages: c.Languages, Conventions: c.Conventions, Layers: c.Layers, Reason: c.Reason,
				Test: c.Test.toDomain(),
			}
		}
	}
	if d.Python != nil {
		in.PythonTypeCheck = d.Python.TypeCheck
	}
	if d.Shell != nil {
		in.ShellLint = d.Shell.Lint
	}
	if d.Paths != nil {
		in.AcceptanceTests = d.Paths.AcceptanceTests
	}
	if d.Run != nil {
		in.Run = &domain.RunRecipe{Start: d.Run.Start, HealthURL: d.Run.HealthURL}
	}
	return in
}
