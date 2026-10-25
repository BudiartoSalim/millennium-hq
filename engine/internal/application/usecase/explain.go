package usecase

import (
	"context"
	"errors"
	"fmt"

	"millennium/internal/application/port"
	"millennium/internal/domain"
)

// Explanation says which rules apply to one file and why.
type Explanation struct {
	Path     string
	Excluded bool
	// Language is empty for files no analyzer reads.
	Language domain.Language
	// Component is nil when no component owns the file.
	Component *domain.ComponentMatch
	Layers    []LayerAssignment
	Limits    domain.Limits
}

// LayerAssignment is the file's layer under one convention, or why there is
// none.
type LayerAssignment struct {
	Convention domain.Convention
	// Layer is empty when the file is in no layer.
	Layer string
	// Problem says why the convention cannot be applied, if it cannot.
	Problem string
}

// ExplainFile answers "which rules apply to this file?", the first question
// when a finding is surprising.
type ExplainFile struct {
	config *LoadConfig
	packs  port.PackSource
}

// NewExplainFile returns the explain use-case.
func NewExplainFile(config *LoadConfig, packs port.PackSource) *ExplainFile {
	return &ExplainFile{config: config, packs: packs}
}

// Execute explains the repository-relative path.
func (u *ExplainFile) Execute(ctx context.Context, path string) (Explanation, error) {
	if !domain.IsRepoRelative(path) {
		return Explanation{}, fmt.Errorf("path %q must be inside the repository", path)
	}
	cfg, enabled, err := u.config.Execute(ctx)
	if err != nil {
		return Explanation{}, err
	}
	if !enabled {
		return Explanation{}, ErrConfigRequired
	}
	out := Explanation{Path: path, Excluded: cfg.Excluded(path), Limits: domain.DefaultLimits()}
	out.Language, _ = domain.LanguageOf(path)
	match, ok, err := cfg.ComponentFor(path)
	if err != nil {
		return Explanation{}, err
	}
	if !ok {
		return out, nil
	}
	out.Component = &match
	for _, convention := range match.Component.Conventions {
		out.Layers = append(out.Layers, u.assign(convention, out.Language, match, path))
	}
	return out, nil
}

func (u *ExplainFile) assign(convention domain.Convention, lang domain.Language, match domain.ComponentMatch, path string) LayerAssignment {
	assignment := LayerAssignment{Convention: convention}
	pack, err := u.packs.Pack(convention)
	if errors.Is(err, port.ErrPackUnavailable) {
		assignment.Problem = "not available in this engine version"
		return assignment
	}
	if err != nil {
		assignment.Problem = err.Error()
		return assignment
	}
	layering, err := pack.For(lang, match)
	if err != nil {
		assignment.Problem = err.Error()
		return assignment
	}
	assignment.Layer = layering.LayerOf(path)
	return assignment
}
