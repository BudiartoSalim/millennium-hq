package goanalyzer

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func measureSource(t *testing.T, src string) metrics {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "x.go", "package x\n"+src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			return measure(fn)
		}
	}
	t.Fatal("no function in source")
	return metrics{}
}

// The expected values are worked out by hand from the rules documented on
// metrics; each case names the increments it exercises.
func TestCognitiveComplexityAndNesting(t *testing.T) {
	tests := []struct {
		name      string
		src       string
		cognitive int
		nesting   int
	}{
		{"empty", `func f() {}`, 0, 0},
		{"if: +1", `func f(x int) int { if x > 0 { return 1 }; return 0 }`, 1, 1},
		{"if, else if, else: +1 +1 +1", `func f(a, b bool) { if a { } else if b { } else { } }`, 3, 1},
		{"for, nested if, nested for: +1 +2 +3", `func f(xs []int) { for i := range xs { if i > 0 { for { } } } }`, 6, 3},
		{"one run of &&: +1, plus if", `func f(a, b, c bool) { if a && b && c { } }`, 2, 1},
		{"&& then ||: +2, plus if", `func f(a, b, c bool) { if a && b || c { } }`, 3, 1},
		{"parenthesised mix outside a condition: +2", `func f(a, b, c bool) bool { return a && (b || c) }`, 2, 0},
		{"switch +1, if in case +2", `func f(x int, y bool) { switch x { case 1: if y { }; case 2: } }`, 3, 2},
		{"type switch: +1", `func f(x any) { switch v := x.(type) { case int: _ = v } }`, 1, 1},
		{"select +1, if in case +2", `func f(c chan int, a bool) { select { case <-c: if a { } } }`, 3, 2},
		{"function literal nests: if +2", `func f(a bool) { g := func() { if a { } }; g() }`, 2, 2},
		{"labelled continue: for +1, inner for +2, continue +1", `func f() { outer: for { for { continue outer } } }`, 4, 2},
		{"goto: +1", `func f() { goto end; end: }`, 1, 0},
		{"else block nests: if +1, else +1, inner if +2", `func f(a, b bool) { if a { } else { if b { } } }`, 4, 2},
		{"recursion: if +1, call +1", `func fact(n int) int { if n <= 1 { return 1 }; return n * fact(n-1) }`, 2, 1},
		{"method recursion: +1", `func (t *T) walk() { t.walk() }`, 1, 0},
		{"call to another function is free", `func f() { g() }`, 0, 0},
		{"condition in for header counts once", `func f(a, b bool) { for a && b { } }`, 2, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := measureSource(t, tt.src)
			if m.cognitive != tt.cognitive || m.maxDepth != tt.nesting {
				t.Fatalf("cognitive %d nesting %d, want %d and %d", m.cognitive, m.maxDepth, tt.cognitive, tt.nesting)
			}
		})
	}
}
