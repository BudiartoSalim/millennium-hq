package goanalyzer

import (
	"slices"
	"strings"
	"testing"

	"millennium-code-qc/internal/domain"
)

func runtimeOf(t *testing.T, src string) domain.RuntimeCode {
	t.Helper()
	root := repoWith(t, map[string]string{"go.mod": "module m\n"})
	facts := analyze(t, root, "a.go", src)
	if !facts.Runtime.Known() {
		t.Fatalf("runtime facts of %q are unknown", src)
	}
	return facts.Runtime
}

func runtimeLines(t *testing.T, src string) []int {
	t.Helper()
	r := runtimeOf(t, src)
	var lines []int
	for line := 1; line <= domain.LineCount([]byte(src)); line++ {
		if r.Holds(line) {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestRuntimeLinesSkipCommentsAndBlankLines(t *testing.T) {
	src := "// Package a is documented.\n" + // 1
		"package a\n" + // 2
		"\n" + // 3
		"/* a block\n" + // 4
		"   comment */\n" + // 5
		"func f() string { // trailing\n" + // 6
		"\t// inside\n" + // 7
		"\treturn `raw\n" + // 8
		"// not a comment\n" + // 9
		"`\n" + // 10
		"}\n" // 11
	if got, want := runtimeLines(t, src), []int{2, 6, 8, 9, 10, 11}; !slices.Equal(got, want) {
		t.Fatalf("runtime lines = %v, want %v", got, want)
	}
}

func TestRuntimeDigest(t *testing.T) {
	const base = "package a\n\nfunc f(x int) int {\n\treturn x + 1\n}\n"
	tests := []struct {
		name  string
		other string
		same  bool
	}{
		{"comment added", "// Package a.\npackage a\n\n// f adds one.\nfunc f(x int) int {\n\treturn x + 1 // one\n}\n", true},
		{"whitespace within lines", "package a\n\nfunc f(x  int)  int {\n    return x+1\n}\n", true},
		{"identifier", "package a\n\nfunc f(y int) int {\n\treturn y + 1\n}\n", false},
		{"operator", "package a\n\nfunc f(x int) int {\n\treturn x - 1\n}\n", false},
		{"literal", "package a\n\nfunc f(x int) int {\n\treturn x + 2\n}\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := domain.SameRuntime(runtimeOf(t, base), runtimeOf(t, tt.other)); got != tt.same {
				t.Fatalf("same runtime = %v, want %v", got, tt.same)
			}
		})
	}
}

func TestTokenDigestIsUnambiguous(t *testing.T) {
	// Fields alternate kind and text.
	sum := func(fields ...string) string {
		d := newTokenDigest()
		for i := 0; i < len(fields); i += 2 {
			d.add(fields[i], fields[i+1])
		}
		return d.sum()
	}
	if sum("STRING", "ab", "STRING", "c") == sum("STRING", "a", "STRING", "bc") || sum("ab", "c") == sum("a", "bc") {
		t.Fatal(`["ab","c"] and ["a","bc"] must differ, as tokens and as kind and text`)
	}
	quoted := func(lit string) domain.RuntimeCode {
		return runtimeOf(t, "package a\n\nvar x = "+lit+"\n")
	}
	if domain.SameRuntime(quoted("'a'"), quoted(`"a"`)) || domain.SameRuntime(quoted(`"a"`), quoted("`a`")) {
		t.Fatal("a rune, a string and a raw string must differ")
	}
}

func TestRuntimeDigestKeepsAutomaticSemicolons(t *testing.T) {
	const head = "package a\n\nfunc f() int { return 1 }\n\nfunc g() int {\n\treturn "
	oneLine := runtimeOf(t, head+"/* c */ f()\n}\n")
	broken := runtimeOf(t, head+"/*\n*/ f()\n}\n")
	if domain.SameRuntime(oneLine, broken) {
		t.Fatal("a line break inside a comment after return ends the statement; the digests must differ")
	}
}

func TestToolchainCommentsAreRuntime(t *testing.T) {
	tests := []struct {
		name, before, after string
		line                int
	}{
		{"go directive", "package a\n\n//go:noinline\nfunc f() {}\n", "package a\n\n//go:nosplit\nfunc f() {}\n", 3},
		{"build constraint", "//go:build linux\n\npackage a\n", "//go:build darwin\n\npackage a\n", 1},
		{"legacy build tag", "// +build linux\n\npackage a\n", "//  +build darwin\n\npackage a\n", 1},
		{"line directive", "package a\n\n//line a.go:10\nfunc f() {}\n", "package a\n\n//line a.go:20\nfunc f() {}\n", 3},
		{"block line directive", "package a\n\nfunc f() { /*line a.go:10*/ }\n", "package a\n\nfunc f() { /*line a.go:20*/ }\n", 3},
		{"cgo export", "package a\n\nimport \"C\"\n\n//export F\nfunc F() {}\n", "package a\n\nimport \"C\"\n\n//export G\nfunc F() {}\n", 5},
		{"gccgo extern", "package a\n\n//extern puts\nfunc puts() int\n", "package a\n\n//extern printf\nfunc puts() int\n", 3},
		{"cgo preamble", "package a\n\n// #include <stdio.h>\nimport \"C\"\n", "package a\n\n// #include <stdlib.h>\nimport \"C\"\n", 3},
		{"grouped cgo preamble", "package a\n\nimport (\n\t// int x = 1;\n\t\"C\"\n)\n", "package a\n\nimport (\n\t// int x = 2;\n\t\"C\"\n)\n", 4},
		{"block cgo preamble", "package a\n\n/*\nint x = 1;\n*/\nimport \"C\"\n", "package a\n\n/*\nint x = 2;\n*/\nimport \"C\"\n", 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before, after := runtimeOf(t, tt.before), runtimeOf(t, tt.after)
			if domain.SameRuntime(before, after) {
				t.Fatal("changing only the directive must change the digest")
			}
			if !after.Holds(tt.line) {
				t.Fatalf("line %d holds a directive and must be a runtime line", tt.line)
			}
		})
	}
	// An ordinary doc comment of a non-C import is not a preamble.
	if lines := runtimeLines(t, "package a\n\n// fmt formats.\nimport \"fmt\"\n\nvar _ = fmt.Sprint\n"); slices.Contains(lines, 3) {
		t.Fatalf("runtime lines = %v; line 3 is a plain comment", lines)
	}
}

func TestStringsThatLookLikeCommentsAreRuntime(t *testing.T) {
	src := "package a\n\nvar u = \"http://x\" // c\nvar r = `/* not a comment */`\n"
	if got := runtimeLines(t, src); !slices.Equal(got, []int{1, 3, 4}) {
		t.Fatalf("runtime lines = %v, want [1 3 4]", got)
	}
}

func TestLineDirectivesDoNotMovePositions(t *testing.T) {
	// A //line directive renumbers what follows it. The facts must keep
	// physical lines, or a long function could pass as a short one, and
	// runtime lines would not match git's.
	body := strings.Repeat("\t_ = 1\n", 60)
	src := "package a\n\nimport \"os\"\n\nfunc long() {\n//line a.go:1\n" + body + "}\n\nvar _ = os.Args\n"
	root := repoWith(t, map[string]string{"go.mod": "module m\n"})
	facts := analyze(t, root, "a.go", src)
	if fn := facts.Functions[0]; fn.Line != 5 || fn.Lines != 63 {
		t.Fatalf("long() measured as %+v, want line 5 and 63 lines", fn)
	}
	// Physically: the body ends on 66, the brace on 67, 68 is blank and 69
	// is the var.
	if r := facts.Runtime; !r.Holds(66) || !r.Holds(67) || r.Holds(68) || !r.Holds(69) {
		t.Fatal("runtime lines must be physical")
	}
	src = "package a\n\n//line a.go:100\nimport \"os/exec\"\n\n//line b.go:1\nfunc f(cmd string) { // c\n\t_ = exec.Command(\"sh\", \"-c\", cmd)\n}\n"
	facts = analyze(t, root, "a.go", src)
	if facts.Imports[0].Line != 4 || facts.Constructs[0].Line != 8 || facts.Comments[2].Line != 7 {
		t.Fatalf("imports %+v, constructs %+v, comments %+v; want physical lines 4, 8 and 7", facts.Imports, facts.Constructs, facts.Comments)
	}
}
