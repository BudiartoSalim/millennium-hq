package goanalyzer

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"go/ast"
	"go/scanner"
	"go/token"
	"hash"
	"strconv"
	"strings"

	"millennium-code-qc/internal/domain"
)

// directivePrefixes start the comments the Go toolchain reads: compiler and
// build directives, line directives, and cgo's export and extern markers.
var directivePrefixes = []string{"//go:", "//line", "/*line", "//export", "//extern"}

// runtimeCode reports the lines that hold runtime tokens and a digest of the
// runtime token sequence. go/scanner decides what is a comment; every other
// token is runtime, and so are the comments the toolchain reads. Automatic
// semicolons stay in the digest, because a line break (one inside a /* */
// comment included) can end a statement; they mark no line of their own.
// Lines are physical, so a //line directive cannot move them.
func runtimeCode(fset *token.FileSet, file *ast.File, content []byte) (domain.RuntimeCode, error) {
	preamble := cgoPreamble(fset, file)
	scanned := token.NewFileSet().AddFile("", -1, len(content))
	var s scanner.Scanner
	// The parser has accepted the same bytes, so the scanner meets no
	// error it would have to report.
	s.Init(scanned, content, nil, scanner.ScanComments)
	digest := newTokenDigest()
	var lines []int
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.COMMENT && !preamble[scanned.Offset(pos)] && !isDirective(lit) {
			continue
		}
		text := lit
		if text == "" {
			text = tok.String()
		}
		digest.add(tok.String(), text)
		if tok == token.SEMICOLON && lit == "\n" {
			continue
		}
		first := scanned.PositionFor(pos, false).Line
		for line := first; line <= first+strings.Count(lit, "\n"); line++ {
			lines = append(lines, line)
		}
	}
	return domain.NewRuntimeCode(lines, digest.sum(), domain.LineCount(content))
}

func isDirective(comment string) bool {
	for _, prefix := range directivePrefixes {
		if strings.HasPrefix(comment, prefix) {
			return true
		}
	}
	// go/build reads "// +build" with any spacing after the slashes.
	rest, ok := strings.CutPrefix(comment, "//")
	return ok && strings.HasPrefix(strings.TrimSpace(rest), "+build")
}

// cgoPreamble returns the byte offsets of the comments that form the cgo
// preamble: the doc comment of an `import "C"`, which cgo compiles as C.
// The declaration's doc counts too, grouped or not, erring toward runtime.
func cgoPreamble(fset *token.FileSet, file *ast.File) map[int]bool {
	offsets := map[int]bool{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.IMPORT {
			continue
		}
		for _, spec := range gen.Specs {
			if imp := spec.(*ast.ImportSpec); isImportC(imp) {
				addComments(offsets, fset, imp.Doc)
				addComments(offsets, fset, gen.Doc)
			}
		}
	}
	return offsets
}

func isImportC(spec *ast.ImportSpec) bool {
	importPath, err := strconv.Unquote(spec.Path.Value)
	return err == nil && importPath == "C"
}

func addComments(offsets map[int]bool, fset *token.FileSet, group *ast.CommentGroup) {
	if group == nil {
		return
	}
	for _, c := range group.List {
		offsets[fset.PositionFor(c.Pos(), false).Offset] = true
	}
}

// tokenDigest is SHA-256 over each token's kind and text, each prefixed
// with its length, so no two token sequences share an encoding.
type tokenDigest struct{ h hash.Hash }

func newTokenDigest() tokenDigest { return tokenDigest{h: sha256.New()} }

func (d tokenDigest) add(kind, text string) {
	for _, field := range []string{kind, text} {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(field)))
		d.h.Write(size[:])
		d.h.Write([]byte(field))
	}
}

func (d tokenDigest) sum() string { return hex.EncodeToString(d.h.Sum(nil)) }
