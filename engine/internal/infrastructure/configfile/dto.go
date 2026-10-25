package configfile

import "millennium/internal/domain"

// configDTO mirrors the file format. Pointers and nil slices tell an absent
// setting (take the default) from an explicit one (validate it). The JSON
// Schema in schema/millennium.schema.json must list exactly these fields.
type configDTO struct {
	Schema           string         `json:"$schema"`
	Version          *int           `json:"version"`
	Languages        []string       `json:"languages"`
	Conventions      []string       `json:"conventions"`
	Components       []componentDTO `json:"components"`
	Exclude          []string       `json:"exclude"`
	Testing          *string        `json:"testing"`
	DependencyPolicy *string        `json:"dependencyPolicy"`
	Python           *pythonDTO     `json:"python"`
	Paths            *pathsDTO      `json:"paths"`
	Run              *runDTO        `json:"run"`
}

type componentDTO struct {
	Paths       []string            `json:"paths"`
	Languages   []string            `json:"languages"`
	Conventions []string            `json:"conventions"`
	Layers      map[string][]string `json:"layers"`
	Reason      string              `json:"reason"`
}

type pythonDTO struct {
	TypeCheck *string `json:"typeCheck"`
}

type pathsDTO struct {
	AcceptanceTests *string `json:"acceptanceTests"`
}

type runDTO struct {
	Start     string `json:"start"`
	HealthURL string `json:"healthUrl"`
}

func (d configDTO) toInput() domain.ConfigInput {
	in := domain.ConfigInput{
		Version:          d.Version,
		Languages:        d.Languages,
		Conventions:      d.Conventions,
		Exclude:          d.Exclude,
		Testing:          d.Testing,
		DependencyPolicy: d.DependencyPolicy,
	}
	if d.Components != nil {
		in.Components = make([]domain.ComponentInput, len(d.Components))
		for i, c := range d.Components {
			in.Components[i] = domain.ComponentInput{
				Paths: c.Paths, Languages: c.Languages, Conventions: c.Conventions, Layers: c.Layers, Reason: c.Reason,
			}
		}
	}
	if d.Python != nil {
		in.PythonTypeCheck = d.Python.TypeCheck
	}
	if d.Paths != nil {
		in.AcceptanceTests = d.Paths.AcceptanceTests
	}
	if d.Run != nil {
		in.Run = &domain.RunRecipe{Start: d.Run.Start, HealthURL: d.Run.HealthURL}
	}
	return in
}
