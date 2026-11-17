package helperanalyzer

import (
	"slices"
	"testing"

	"millennium-code-qc/internal/domain"
)

// heldLines lists the lines of src that may hold runtime code.
func heldLines(src string, r domain.RuntimeCode) []int {
	var lines []int
	for line := 1; line <= domain.LineCount([]byte(src)); line++ {
		if r.Holds(line) {
			lines = append(lines, line)
		}
	}
	return lines
}

func knownRuntime(t *testing.T, a *Analyzer, path, src string) domain.RuntimeCode {
	t.Helper()
	r := run(t, a, path, src).Runtime
	if !r.Known() {
		t.Fatalf("runtime facts of %q are unknown", src)
	}
	return r
}

func TestPythonRuntimeLines(t *testing.T) {
	a := pythonAnalyzer(t, nil)
	src := "# leading comment\n" + // 1
		"\n" + // 2
		"def f(x):\n" + // 3
		"    \"\"\"Docstrings are\n" + // 4
		"    string literals.\"\"\"\n" + // 5
		"    # inside\n" + // 6
		"    s = \"# no\"  # yes\n" + // 7
		"    return (x +\n" + // 8
		"            # between\n" + // 9
		"            1)\n" // 10
	if got, want := heldLines(src, knownRuntime(t, a, "m.py", src)), []int{3, 4, 5, 7, 8, 10}; !slices.Equal(got, want) {
		t.Fatalf("runtime lines = %v, want %v", got, want)
	}
}

func TestPythonRuntimeDigest(t *testing.T) {
	a := pythonAnalyzer(t, nil)
	const base = "def f(x):\n    \"\"\"Add one.\"\"\"\n    if x:\n        return x + 1\n    return 'a'\n"
	tests := []struct {
		name, other string
		same        bool
	}{
		{"comments and blank lines", "# head\n\ndef f(x):  # f\n    \"\"\"Add one.\"\"\"\n    # c\n    if x:\n        return x + 1\n\n    return 'a'\n", true},
		{"spacing within a line", "def f( x ):\n    \"\"\"Add one.\"\"\"\n    if x:\n        return x+1\n    return 'a'\n", true},
		{"docstring text", "def f(x):\n    \"\"\"Add two.\"\"\"\n    if x:\n        return x + 1\n    return 'a'\n", false},
		{"quote style", "def f(x):\n    \"\"\"Add one.\"\"\"\n    if x:\n        return x + 1\n    return \"a\"\n", false},
		{"identifier", "def g(x):\n    \"\"\"Add one.\"\"\"\n    if x:\n        return x + 1\n    return 'a'\n", false},
		{"dedented statement", "def f(x):\n    \"\"\"Add one.\"\"\"\n    if x:\n        return x + 1\n        return 'a'\n", false},
		{"continuation", "def f(x):\n    \"\"\"Add one.\"\"\"\n    if x:\n        return x \\\n+ 1\n    return 'a'\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := domain.SameRuntime(knownRuntime(t, a, "m.py", base), knownRuntime(t, a, "m.py", tt.other)); got != tt.same {
				t.Fatalf("same runtime = %v, want %v", got, tt.same)
			}
		})
	}
	// Tokens are compared one by one, not as one run of text.
	if domain.SameRuntime(knownRuntime(t, a, "m.py", "x = 'ab' 'c'\n"), knownRuntime(t, a, "m.py", "x = 'a' 'bc'\n")) {
		t.Fatal(`["ab","c"] and ["a","bc"] must differ`)
	}
	// A line break is a NEWLINE token; a backslash continuation is not.
	if domain.SameRuntime(knownRuntime(t, a, "m.py", "x = 1 \\\n-1\n"), knownRuntime(t, a, "m.py", "x = 1\n-1\n")) {
		t.Fatal("x = 1 \\\\\\n-1 and x = 1\\n-1 are different programs")
	}
}

func TestPythonEncodingDeclaration(t *testing.T) {
	a := pythonAnalyzer(t, nil)
	utf8 := knownRuntime(t, a, "m.py", "# -*- coding: utf-8 -*-\nx = 1\n")
	if !utf8.Holds(1) {
		t.Fatal("the encoding declaration is read by the interpreter: its line is runtime")
	}
	if domain.SameRuntime(utf8, knownRuntime(t, a, "m.py", "# -*- coding: utf8 -*-\nx = 1\n")) {
		t.Fatal("changing only the declaration must change the digest")
	}
	second := knownRuntime(t, a, "m.py", "#!/usr/bin/env python3\n# coding: utf-8\nx = 1\n")
	if second.Holds(1) || !second.Holds(2) {
		t.Fatal("a declaration on line 2 is runtime, the shebang comment before it is not")
	}
	// Python decodes these with another codec than the text the engine
	// sent; utf-7 turns the second line into print(1).
	for _, src := range []string{
		"# -*- coding: utf-7 -*-\n# +AAo-print(1)\n",
		"# coding: latin-1\nx = 1\n",
		"# coding: no-such-codec\nx = 1\n",
	} {
		r := run(t, a, "m.py", src).Runtime
		if r.Known() || !r.Holds(2) || domain.SameRuntime(r, r) {
			t.Fatalf("%q: runtime facts must be unknown", src)
		}
	}
}

func TestPythonRuntimeUnknownWhereGitNumbersOtherwise(t *testing.T) {
	a := pythonAnalyzer(t, nil)
	for _, src := range []string{"x = 1  # c\rimport os\n", "x = 'caf\xe9'\n"} {
		if r := run(t, a, "m.py", src).Runtime; r.Known() {
			t.Fatalf("%q: runtime facts must be unknown", src)
		}
	}
	if r := run(t, a, "m.py", "x = 1  # c\r\ny = 2\r\n").Runtime; !r.Known() || !r.Holds(2) {
		t.Fatal("CRLF line endings number like git")
	}
}
