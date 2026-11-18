package helperanalyzer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"unicode/utf8"

	"millennium-code-qc/internal/domain"
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
	// Runtime is absent, or null, when the helper could not establish the
	// file's runtime code.
	Runtime *runtimeDTO `json:"runtime"`
}

type runtimeDTO struct {
	Lines  []int  `json:"lines"`
	Digest string `json:"digest"`
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

// decode turns one answer line about content into facts. The helper runs
// inside a repository an agent can modify, so its answer is checked rather
// than trusted: unknown kinds, targets outside the repository and malformed
// runtime facts are errors.
func decode(path string, content, line []byte) (domain.FileFacts, error) {
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
	return resp.Facts.toDomain(path, content)
}

func (f factsDTO) toDomain(path string, content []byte) (domain.FileFacts, error) {
	runtime, err := f.runtimeCode(path, content)
	if err != nil {
		return domain.FileFacts{}, err
	}
	facts := domain.FileFacts{Path: path, Generated: f.Generated, Test: f.Test, Runtime: runtime}
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

// runtimeCode converts the helper's runtime facts. An answer without them
// leaves them unknown, and so does content the helper cannot describe in
// git's terms; facts that are there but malformed reject the whole answer.
func (f factsDTO) runtimeCode(path string, content []byte) (domain.RuntimeCode, error) {
	if f.Runtime == nil || !numberedLikeGit(content) {
		return domain.RuntimeCode{}, nil
	}
	runtime, err := domain.NewRuntimeCode(f.Runtime.Lines, f.Runtime.Digest, domain.LineCount(content))
	if err != nil {
		return domain.RuntimeCode{}, fmt.Errorf("analyzer answer for %s: %w", path, err)
	}
	return runtime, nil
}

// numberedLikeGit reports whether a helper sees content as git does. The
// request carries text, so bytes that are not UTF-8 reach the helper
// replaced, and it would describe other code. The helpers' parsers also end
// lines where git does not (a lone CR; in TypeScript also U+2028 and
// U+2029), so their line numbers would drift from git's, and a comment
// could end where git's reading of the line does not.
func numberedLikeGit(content []byte) bool {
	if !utf8.Valid(content) || bytes.ContainsRune(content, '\u2028') || bytes.ContainsRune(content, '\u2029') {
		return false
	}
	for i, b := range content {
		if b == '\r' && (i+1 == len(content) || content[i+1] != '\n') {
			return false
		}
	}
	return true
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
