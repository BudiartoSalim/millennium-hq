package helperanalyzer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"millennium/internal/domain"
)

type response struct {
	Facts *factsDTO `json:"facts"`
	Error string    `json:"error"`
}

type factsDTO struct {
	Generated  bool           `json:"generated"`
	Test       bool           `json:"test"`
	Imports    []importDTO    `json:"imports"`
	Functions  []functionDTO  `json:"functions"`
	Comments   []commentDTO   `json:"comments"`
	Constructs []constructDTO `json:"constructs"`
}

type importDTO struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Kind   string `json:"kind"`
	Target string `json:"target"`
}

type functionDTO struct {
	Key       string `json:"key"`
	Line      int    `json:"line"`
	Lines     int    `json:"lines"`
	Nesting   int    `json:"nesting"`
	Cognitive int    `json:"cognitive"`
}

type commentDTO struct {
	Line int    `json:"line"`
	Text string `json:"text"`
}

type constructDTO struct {
	Kind   string `json:"kind"`
	Line   int    `json:"line"`
	Detail string `json:"detail"`
}

// decode turns one answer line into facts. The helper runs inside a
// repository an agent can modify, so its answer is checked rather than
// trusted: unknown kinds and targets outside the repository are errors.
func decode(path string, line []byte) (domain.FileFacts, error) {
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	var resp response
	if err := dec.Decode(&resp); err != nil {
		return domain.FileFacts{}, fmt.Errorf("unreadable analyzer answer for %s: %w", path, err)
	}
	if resp.Error != "" {
		return domain.FileFacts{}, errors.New(resp.Error)
	}
	if resp.Facts == nil {
		return domain.FileFacts{}, fmt.Errorf("the analyzer returned no facts for %s", path)
	}
	return resp.Facts.toDomain(path)
}

func (f factsDTO) toDomain(path string) (domain.FileFacts, error) {
	facts := domain.FileFacts{Path: path, Generated: f.Generated, Test: f.Test}
	for _, imp := range f.Imports {
		converted, err := imp.toDomain()
		if err != nil {
			return domain.FileFacts{}, fmt.Errorf("analyzer answer for %s: %w", path, err)
		}
		facts.Imports = append(facts.Imports, converted)
	}
	for _, fn := range f.Functions {
		facts.Functions = append(facts.Functions, domain.FunctionFacts(fn))
	}
	for _, c := range f.Comments {
		facts.Comments = append(facts.Comments, domain.Comment(c))
	}
	for _, c := range f.Constructs {
		kind := domain.ConstructKind(c.Kind)
		if !slices.Contains(domain.ConstructKinds(), kind) {
			return domain.FileFacts{}, fmt.Errorf("analyzer answer for %s: unknown construct kind %q", path, c.Kind)
		}
		facts.Constructs = append(facts.Constructs, domain.Construct{Kind: kind, Line: c.Line, Detail: c.Detail})
	}
	return facts, nil
}

func (i importDTO) toDomain() (domain.Import, error) {
	kind := domain.ImportKind(i.Kind)
	switch kind {
	case domain.ImportStandard, domain.ImportExternal:
		return domain.Import{Path: i.Path, Line: i.Line, Kind: kind}, nil
	case domain.ImportProject:
		if i.Target != "" && !domain.IsRepoRelative(i.Target) {
			return domain.Import{}, fmt.Errorf("import %q has a target outside the repository: %q", i.Path, i.Target)
		}
		return domain.Import{Path: i.Path, Line: i.Line, Kind: kind, Target: i.Target}, nil
	}
	return domain.Import{}, fmt.Errorf("import %q has an unknown kind %q", i.Path, i.Kind)
}
