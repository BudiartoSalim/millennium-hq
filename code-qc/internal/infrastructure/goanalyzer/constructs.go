package goanalyzer

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"

	"millennium-code-qc/internal/domain"
)

// shells are programs that interpret their next argument as a command line.
var shells = map[string]string{
	"sh": "-c", "bash": "-c", "/bin/sh": "-c", "/bin/bash": "-c", "zsh": "-c",
	"cmd": "/c", "cmd.exe": "/c", "powershell": "-Command", "pwsh": "-Command",
}

func comments(fset *token.FileSet, file *ast.File) []domain.Comment {
	var out []domain.Comment
	for _, group := range file.Comments {
		for _, c := range group.List {
			out = append(out, domain.Comment{Line: line(fset, c.Pos()), Text: c.Text})
		}
	}
	return out
}

// constructs finds risky constructs: TLS verification switched off,
// commands run through a shell, and line directives.
func constructs(fset *token.FileSet, file *ast.File) []domain.Construct {
	var out []domain.Construct
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.KeyValueExpr:
			if isIdent(n.Key, "InsecureSkipVerify") && isIdent(n.Value, "true") {
				out = append(out, domain.Construct{Kind: domain.ConstructTLSVerifyOff, Line: line(fset, n.Pos()), Detail: "InsecureSkipVerify: true"})
			}
		case *ast.CallExpr:
			if detail, ok := shellCommand(n); ok {
				out = append(out, domain.Construct{Kind: domain.ConstructShellCommand, Line: line(fset, n.Pos()), Detail: detail})
			}
		}
		return true
	})
	return append(out, lineDirectives(fset, file)...)
}

// lineDirectives finds the comments go/scanner reads as line directives,
// "//line " and "/*line ", which move the positions the compiler and the
// coverage tool report. Positions anywhere count, erring toward reporting
// one the compiler would ignore.
func lineDirectives(fset *token.FileSet, file *ast.File) []domain.Construct {
	var out []domain.Construct
	for _, group := range file.Comments {
		for _, c := range group.List {
			if strings.HasPrefix(c.Text, "//line ") || strings.HasPrefix(c.Text, "/*line ") {
				out = append(out, domain.Construct{Kind: domain.ConstructLineDirective, Line: line(fset, c.Pos()), Detail: c.Text})
			}
		}
	}
	return out
}

// shellCommand recognises exec.Command("sh", "-c", ...) and
// exec.CommandContext(ctx, "bash", "-c", ...).
func shellCommand(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !isIdent(sel.X, "exec") {
		return "", false
	}
	args := call.Args
	switch sel.Sel.Name {
	case "Command":
	case "CommandContext":
		if len(args) == 0 {
			return "", false
		}
		args = args[1:]
	default:
		return "", false
	}
	if len(args) < 2 {
		return "", false
	}
	program, flag := stringLit(args[0]), stringLit(args[1])
	if want, ok := shells[program]; ok && flag == want {
		return "exec." + sel.Sel.Name + "(" + strconv.Quote(program) + ", " + strconv.Quote(flag) + ", ...)", true
	}
	return "", false
}

func isIdent(expr ast.Expr, name string) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == name
}

func stringLit(expr ast.Expr) string {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return ""
	}
	return value
}
