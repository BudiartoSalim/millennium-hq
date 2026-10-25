package configfile

import "millennium/internal/domain"

// configDTO mirrors the file format. Pointers tell an absent setting (take
// the default) from an explicit one (validate it). The JSON Schema in
// schema/millennium.schema.json must list exactly these fields.
type configDTO struct {
	Schema           string     `json:"$schema"`
	Version          *int       `json:"version"`
	Languages        []string   `json:"languages"`
	Conventions      []string   `json:"conventions"`
	Testing          *string    `json:"testing"`
	DependencyPolicy *string    `json:"dependencyPolicy"`
	Python           *pythonDTO `json:"python"`
	Paths            *pathsDTO  `json:"paths"`
	Run              *runDTO    `json:"run"`
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
		Testing:          d.Testing,
		DependencyPolicy: d.DependencyPolicy,
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
