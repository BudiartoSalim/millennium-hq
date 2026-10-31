// Package goanalyzer turns Go source into domain.FileFacts with the standard
// library parser: imports classified against the file's module, every
// function's length, nesting depth and cognitive complexity, comments, and
// risky constructs.
package goanalyzer

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"

	"millennium-code-qc/internal/domain"
)

// Analyzer analyses Go files of one repository.
type Analyzer struct {
	modules *moduleFinder
}

// New returns an analyzer for the repository at root.
func New(root string) *Analyzer {
	return &Analyzer{modules: newModuleFinder(root)}
}

// Analyze parses content as the Go file at the repository-relative path.
func (a *Analyzer) Analyze(_ context.Context, path string, content []byte) (domain.FileFacts, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, content, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return domain.FileFacts{}, fmt.Errorf("parse %s: %w", path, err)
	}
	mod, err := a.modules.forFile(path)
	if err != nil {
		return domain.FileFacts{}, err
	}
	facts := domain.FileFacts{
		Path:       path,
		Generated:  ast.IsGenerated(file),
		Test:       strings.HasSuffix(path, "_test.go"),
		Imports:    imports(fset, file, mod),
		Functions:  functions(fset, file),
		Comments:   comments(fset, file),
		Constructs: constructs(fset, file),
	}
	return facts, nil
}

func imports(fset *token.FileSet, file *ast.File, mod module) []domain.Import {
	out := make([]domain.Import, 0, len(file.Imports))
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			// The parser accepted the file, so the literal is well-formed.
			importPath = spec.Path.Value
		}
		kind, target := mod.classify(importPath)
		out = append(out, domain.Import{
			Path:   importPath,
			Line:   fset.Position(spec.Pos()).Line,
			Kind:   kind,
			Target: target,
		})
	}
	return out
}

func functions(fset *token.FileSet, file *ast.File) []domain.FunctionFacts {
	var out []domain.FunctionFacts
	seen := map[string]int{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		key := functionKey(fn)
		seen[key]++
		if seen[key] > 1 {
			key = fmt.Sprintf("%s#%d", key, seen[key])
		}
		m := measure(fn)
		start, end := fset.Position(fn.Pos()).Line, fset.Position(fn.End()).Line
		out = append(out, domain.FunctionFacts{
			Key:       key,
			Line:      start,
			Lines:     end - start + 1,
			Nesting:   m.maxDepth,
			Cognitive: m.cognitive,
		})
	}
	return out
}

// functionKey names a function by receiver type and name, so a method keeps
// its identity across revisions even when its receiver changes from value
// to pointer.
func functionKey(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	return receiverType(fn.Recv.List[0].Type) + "." + fn.Name.Name
}

func receiverType(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return receiverType(t.X)
	case *ast.IndexExpr:
		return receiverType(t.X)
	case *ast.IndexListExpr:
		return receiverType(t.X)
	case *ast.Ident:
		return t.Name
	}
	return "?"
}
