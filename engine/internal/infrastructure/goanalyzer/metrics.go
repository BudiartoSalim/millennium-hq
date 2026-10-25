package goanalyzer

import (
	"go/ast"
	"go/token"
)

// metrics walks one function body. Cognitive complexity follows the
// SonarSource definition as applied to Go:
//
//   - +1, plus the current nesting level, for if, switch, type switch, select,
//     for and range;
//   - +1 for else if and else, without the nesting bonus;
//   - +1 for each run of the same logical operator in a condition
//     ("a && b && c" is 1, "a && b || c" is 2);
//   - +1 for goto and for labelled break and continue;
//   - +1 for each direct recursive call.
//
// The bodies of those structures and of function literals are one nesting
// level deeper. Nesting depth is the deepest such level reached.
type metrics struct {
	name      string
	recv      string
	cognitive int
	maxDepth  int
}

func measure(fn *ast.FuncDecl) metrics {
	m := metrics{name: fn.Name.Name}
	if fn.Recv != nil && len(fn.Recv.List) > 0 && len(fn.Recv.List[0].Names) > 0 {
		m.recv = fn.Recv.List[0].Names[0].Name
	}
	m.walk(fn.Body, 0)
	return m
}

// walk visits node at the given nesting level. Plain nodes are descended
// into by ast.Inspect; control structures handle their own children so each
// child gets the right level. Absent optional fields (a for loop without a
// condition) arrive as nil interfaces and are skipped.
func (m *metrics) walk(node ast.Node, nesting int) {
	if node == nil {
		return
	}
	ast.Inspect(node, func(n ast.Node) bool {
		if n == nil || n == node && !isControl(n) {
			return true
		}
		return m.visit(n, nesting)
	})
}

func isControl(n ast.Node) bool {
	switch n.(type) {
	case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt,
		*ast.SelectStmt, *ast.FuncLit, *ast.BinaryExpr:
		return true
	}
	return false
}

// visit handles one node and reports whether ast.Inspect should descend.
func (m *metrics) visit(n ast.Node, nesting int) bool {
	switch n := n.(type) {
	case *ast.IfStmt:
		m.ifStmt(n, nesting, false)
	case *ast.ForStmt:
		m.structure(nesting, n.Body, n.Init, n.Cond, n.Post)
	case *ast.RangeStmt:
		m.structure(nesting, n.Body, n.Key, n.Value, n.X)
	case *ast.SwitchStmt:
		m.structure(nesting, n.Body, n.Init, n.Tag)
	case *ast.TypeSwitchStmt:
		m.structure(nesting, n.Body, n.Init, n.Assign)
	case *ast.SelectStmt:
		m.structure(nesting, n.Body)
	case *ast.FuncLit:
		m.deepen(nesting + 1)
		m.walk(n.Body, nesting+1)
	case *ast.BinaryExpr:
		return m.logical(n, nesting)
	case *ast.BranchStmt:
		if n.Tok == token.GOTO || n.Label != nil {
			m.cognitive++
		}
		return true
	case *ast.CallExpr:
		if m.isRecursive(n) {
			m.cognitive++
		}
		return true
	default:
		return true
	}
	return false
}

// structure scores a loop, switch or select: its header at the current
// level, its body one level deeper.
func (m *metrics) structure(nesting int, body *ast.BlockStmt, header ...ast.Node) {
	m.cognitive += 1 + nesting
	m.deepen(nesting + 1)
	for _, h := range header {
		m.walk(h, nesting)
	}
	m.walk(body, nesting+1)
}

func (m *metrics) ifStmt(n *ast.IfStmt, nesting int, elseIf bool) {
	if elseIf {
		m.cognitive++
	} else {
		m.cognitive += 1 + nesting
	}
	m.deepen(nesting + 1)
	if n.Init != nil {
		m.walk(n.Init, nesting)
	}
	m.walk(n.Cond, nesting)
	m.walk(n.Body, nesting+1)
	switch alt := n.Else.(type) {
	case *ast.IfStmt:
		m.ifStmt(alt, nesting, true)
	case *ast.BlockStmt:
		m.cognitive++
		m.walk(alt, nesting+1)
	}
}

// logical scores a chain of && and || once, at its outermost operator, then
// walks the operands so nested function literals and calls are still seen.
func (m *metrics) logical(n *ast.BinaryExpr, nesting int) bool {
	if !isLogical(n.Op) {
		return true
	}
	var ops []token.Token
	var operands []ast.Expr
	flattenLogical(n, &ops, &operands)
	for i, op := range ops {
		if i == 0 || op != ops[i-1] {
			m.cognitive++
		}
	}
	for _, operand := range operands {
		m.walk(operand, nesting)
	}
	return false
}

func flattenLogical(expr ast.Expr, ops *[]token.Token, operands *[]ast.Expr) {
	switch e := expr.(type) {
	case *ast.ParenExpr:
		if inner, ok := e.X.(*ast.BinaryExpr); ok && isLogical(inner.Op) {
			flattenLogical(inner, ops, operands)
			return
		}
	case *ast.BinaryExpr:
		if isLogical(e.Op) {
			flattenLogical(e.X, ops, operands)
			*ops = append(*ops, e.Op)
			flattenLogical(e.Y, ops, operands)
			return
		}
	}
	*operands = append(*operands, expr)
}

func isLogical(op token.Token) bool { return op == token.LAND || op == token.LOR }

func (m *metrics) isRecursive(call *ast.CallExpr) bool {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return m.recv == "" && fun.Name == m.name
	case *ast.SelectorExpr:
		x, ok := fun.X.(*ast.Ident)
		return ok && m.recv != "" && x.Name == m.recv && fun.Sel.Name == m.name
	}
	return false
}

func (m *metrics) deepen(level int) {
	if level > m.maxDepth {
		m.maxDepth = level
	}
}
