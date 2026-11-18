package goanalyzer

import (
	"go/scanner"
	"go/token"
	"strconv"
	"strings"

	"millennium-code-qc/internal/domain"
)

// LineDirectives returns the comments of Go source that go/scanner reads as
// line directives, "//line " and "/*line ", which move the positions the
// compiler and the coverage tool report. A directive anywhere counts, erring
// toward reporting one the compiler would ignore, and the source need not
// parse. Lines are physical, as git numbers them.
func LineDirectives(content []byte) []domain.LineDirective {
	file := token.NewFileSet().AddFile("", -1, len(content))
	var s scanner.Scanner
	// Errors do not matter here: the scanner goes on past them, and a file
	// the compiler rejects runs no test.
	s.Init(file, content, nil, scanner.ScanComments)
	var out []domain.LineDirective
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			return out
		}
		if tok == token.COMMENT && (strings.HasPrefix(lit, "//line ") || strings.HasPrefix(lit, "/*line ")) {
			out = append(out, domain.LineDirective{Line: file.PositionFor(pos, false).Line, Text: lit, File: directiveFile(lit)})
		}
	}
}

// directiveFile is the file name a line directive gives, read as go/scanner
// reads it: what precedes a trailing ":line" or ":line:col". The name may
// hold colons itself (a Windows path). It is empty when the directive gives
// none, or has no line number, which the compiler ignores or rejects.
func directiveFile(directive string) string {
	text := directive[len("//line "):]
	if strings.HasPrefix(directive, "/*") {
		text = strings.TrimSuffix(text, "*/")
	}
	name, ok := cutNumber(text)
	if !ok {
		return ""
	}
	if withoutLine, ok := cutNumber(name); ok {
		return withoutLine
	}
	return name
}

// cutNumber removes a trailing ":<digits>" from s.
func cutNumber(s string) (string, bool) {
	i := strings.LastIndexByte(s, ':')
	if i < 0 {
		return "", false
	}
	if _, err := strconv.ParseUint(s[i+1:], 10, 0); err != nil {
		return "", false
	}
	return s[:i], true
}
