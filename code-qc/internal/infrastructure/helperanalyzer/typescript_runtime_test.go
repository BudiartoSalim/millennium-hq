package helperanalyzer

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"millennium-code-qc/internal/domain"
)

// runtimeProject is a TypeScript project whose tsconfig sets options.
func runtimeProject(t *testing.T, options string) (*Analyzer, string) {
	t.Helper()
	return typeScriptProject(t, map[string]string{
		"web/package.json":  `{"name":"web"}`,
		"web/tsconfig.json": `{"compilerOptions": ` + options + `}`,
	})
}

func TestTypeScriptRuntimeLinesSkipErasedSyntax(t *testing.T) {
	a, _ := runtimeProject(t, `{}`)
	src := "/** The cart. */\n" + // 1: the B2 doc comment that lcov reports as DA:1,0
		"import type { Item } from './item';\n" + // 2
		"export interface Cart {\n" + // 3
		"  items: Item[];\n" + // 4
		"}\n" + // 5
		"export type Total = number;\n" + // 6
		"export type { Item };\n" + // 7
		"declare const limit: number;\n" + // 8
		"declare function log(x: string): void;\n" + // 9
		"export function total(cart: Cart): Total;\n" + // 10
		"export function total(\n" + // 11
		"  cart: Cart,\n" + // 12
		"  scale?: {\n" + // 13
		"    by: number;\n" + // 14
		"  },\n" + // 15: the compiler maps the end of the parameter here
		"): Total {\n" + // 16
		"  // sum them\n" + // 17
		"  return cart.items.length * (scale ? scale.by : 1);\n" + // 18
		"}\n" // 19
	r := knownRuntime(t, a, "web/src/cart.ts", src)
	got := heldLines(src, r)
	for _, line := range []int{11, 12, 13, 18} {
		if !slices.Contains(got, line) {
			t.Errorf("line %d holds runtime code; runtime lines = %v", line, got)
		}
	}
	for _, line := range []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 14, 17} {
		if slices.Contains(got, line) {
			t.Errorf("line %d holds no runtime code; runtime lines = %v", line, got)
		}
	}
}

func TestTypeScriptConstructsThatEmitCodeAreRuntime(t *testing.T) {
	a, _ := runtimeProject(t, `{"experimentalDecorators": true, "useDefineForClassFields": false}`)
	tests := []struct {
		name, src string
		line      int
		runtime   bool
	}{
		{"enum", "enum E {\n  A,\n}\n", 2, true},
		{"const enum", "const enum E {\n  A = 1,\n}\n", 2, true},
		{"declare const enum", "declare const enum E {\n  A = 1,\n}\n", 2, true},
		{"namespace with values", "namespace N {\n  export const x = 1;\n}\n", 2, true},
		{"namespace of types", "namespace N {\n  export type X = 1;\n}\n", 2, false},
		{"class field with initializer", "class C {\n  x = 1;\n}\n", 2, true},
		{"class field without initializer", "class C {\n  x: number;\n}\n", 2, true},
		{"declare field", "class C {\n  declare x: number;\n}\n", 2, false},
		{"abstract field", "abstract class C {\n  abstract x: number;\n}\n", 2, false},
		{"field annotation", "class C {\n  x?:\n    number;\n}\n", 3, false},
		{"parameter property", "class C {\n  constructor(\n    private x: number,\n  ) {}\n}\n", 3, true},
		{"decorator", "class C {\n  @dec\n  m() {}\n}\n", 2, true},
		// Without emitDecoratorMetadata these annotations emit nothing here;
		// with it they do, so they count whatever the options.
		{"annotation of a decorated member", "class C {\n  @dec\n  x:\n    Foo\n    = 1;\n}\n", 4, true},
		{"parameter type of a decorated method", "class C {\n  @dec\n  m(\n    x:\n      Foo\n      = 1,\n  ) {}\n}\n", 5, true},
		{"constructor parameter type of a decorated class", "@dec\nclass C {\n  constructor(\n    x:\n      Foo\n      = 1,\n  ) {}\n}\n", 5, true},
		{"annotation of an undecorated member", "class C {\n  x:\n    Foo\n    = 1;\n}\n", 3, false},
		{"import equals require", "import fs = require('fs');\n", 1, true},
		{"export equals", "const x = 1;\nexport = x;\n", 2, true},
		{"accessor", "class C {\n  accessor x = 1;\n}\n", 2, true},
		{"unused value import", "import { a } from './a';\n", 1, true},
		{"side-effect import", "import './polyfill';\n", 1, true},
		{"value export", "const a = 1;\nexport { a };\n", 2, true},
		{"overload signature", "function f(x: string): string;\nfunction f(x: unknown) { return x; }\n", 1, false},
		{"type-only import", "import type { a } from './a';\n", 1, false},
		{"mixed line: code and comment", "const x = 1; // one\n", 1, true},
		{"mixed line: code and type", "let y: string | number = 2;\n", 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := knownRuntime(t, a, "web/src/m.ts", tt.src)
			if r.Holds(tt.line) != tt.runtime {
				t.Fatalf("line %d runtime = %v, want %v; runtime lines = %v", tt.line, !tt.runtime, tt.runtime, heldLines(tt.src, r))
			}
		})
	}
}

func TestTypeScriptRuntimeDigest(t *testing.T) {
	a, _ := runtimeProject(t, `{}`)
	const base = "export interface P { n: number }\nexport function f(p: P): number {\n  return p.n + 1;\n}\nexport const s = 'a';\n"
	tests := []struct {
		name, other string
		same        bool
	}{
		{"comments", "// head\n/** P. */\nexport interface P { n: number }\nexport function f(p: P): number { // f\n  /* add */ return p.n + 1;\n}\nexport const s = 'a';\n", true},
		{"layout", "export interface P { n: number }\nexport function f(p: P): number { return p.n+1 }\nexport const s = 'a';\n", true},
		{"erased types", "export interface P { n: number; m?: string }\nexport function f(p: P): number | undefined {\n  return p.n + 1;\n}\nexport const s = 'a';\n", true},
		{"identifier", "export interface P { n: number }\nexport function g(p: P): number {\n  return p.n + 1;\n}\nexport const s = 'a';\n", false},
		{"operator", "export interface P { n: number }\nexport function f(p: P): number {\n  return p.n - 1;\n}\nexport const s = 'a';\n", false},
		{"quote style", "export interface P { n: number }\nexport function f(p: P): number {\n  return p.n + 1;\n}\nexport const s = \"a\";\n", false},
		{"value import", "import { x } from './x';\nexport interface P { n: number }\nexport function f(p: P): number {\n  return p.n + 1;\n}\nexport const s = 'a';\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := domain.SameRuntime(knownRuntime(t, a, "web/src/m.ts", base), knownRuntime(t, a, "web/src/m.ts", tt.other)); got != tt.same {
				t.Fatalf("same runtime = %v, want %v", got, tt.same)
			}
		})
	}
}

// Inline type specifiers are erased like whole type-only imports: adding
// or removing one changes no runtime token and marks no line (QT-1). Value
// specifiers still count.
func TestTypeScriptInlineTypeSpecifiersAreErased(t *testing.T) {
	a, _ := runtimeProject(t, `{}`)
	const body = "export function sum(p: Pair): number {\n  return add(p[0], p[1]);\n}\n"
	tests := []struct {
		name, before, after string
		same                bool
	}{
		{"type specifier added to an import", "import { type Pair, add } from './calc.js';\n", "import { type Pair, type Op, add } from './calc.js';\n", true},
		{"type specifier added at the end", "import { add } from './calc.js';\n", "import { add, type Pair } from './calc.js';\n", true},
		{"type specifier added to an export", "import { add } from './calc.js';\nexport { add as plus, type Pair };\n",
			"import { add } from './calc.js';\nexport { add as plus, type Pair, type Op };\n", true},
		{"value specifier added", "import { add } from './calc.js';\n", "import { add, mul } from './calc.js';\n", false},
		{"value specifier renamed", "import { add } from './calc.js';\n", "import { add as plus } from './calc.js';\n", false},
		{"type specifier made a value", "import { type Pair, add } from './calc.js';\n", "import { Pair, add } from './calc.js';\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before, after := knownRuntime(t, a, "web/src/m.ts", tt.before+body), knownRuntime(t, a, "web/src/m.ts", tt.after+body)
			if got := domain.SameRuntime(before, after); got != tt.same {
				t.Fatalf("same runtime = %v, want %v", got, tt.same)
			}
		})
	}
	src := "import {\n  type Pair,\n  add,\n} from './calc.js';\n" + body
	if got := heldLines(src, knownRuntime(t, a, "web/src/m.ts", src)); slices.Contains(got, 2) || !slices.Contains(got, 3) {
		t.Fatalf("runtime lines = %v; want line 3 (add) and not line 2 (type Pair)", got)
	}
}

// A hashbang picks the program that runs the file, and the emit keeps it,
// so it is runtime (QT-2).
func TestTypeScriptHashbangIsRuntime(t *testing.T) {
	a, _ := runtimeProject(t, `{}`)
	const body = "export const x = 1;\n"
	node, bun := knownRuntime(t, a, "web/src/m.ts", "#!/usr/bin/env node\n"+body), knownRuntime(t, a, "web/src/m.ts", "#!/usr/bin/env bun\n"+body)
	if domain.SameRuntime(node, bun) || !bun.Holds(1) {
		t.Fatalf("changing the hashbang must change the digest, and its line must be runtime; lines %v", heldLines("#!\n"+body, bun))
	}
}

// A plain analysis leaves the runtime code out: only a request for it pays
// for the transpile (PERF-1).
func TestTypeScriptPlainAnalysisLeavesRuntimeUnknown(t *testing.T) {
	a, _ := runtimeProject(t, `{}`)
	const src = "export function f(): number {\n  return 1;\n}\n"
	if r := run(t, a, "web/src/m.ts", src).Runtime; r.Known() {
		t.Fatal("a plain analysis established runtime code")
	}
	if r := runRuntime(t, a, "web/src/m.ts", src).Runtime; !r.Known() {
		t.Fatal("a runtime analysis of the same file must establish it")
	}
}

// poisonedTypeScript is a typescript package that wraps the fixture's and
// makes node.getChildren throw: the call builds and keeps a node for every
// token, which on a 4 MiB file took the helper past a 512 MB heap.
const poisonedTypeScript = `
const ts = require(process.env.MILLENNIUM_TYPESCRIPT + '/node_modules/typescript');
const sample = ts.createSourceFile('x.ts', 'f(a + 1); class C { m() {} }', ts.ScriptTarget.Latest, true);
const prototypes = new Set();
const collect = (node) => { prototypes.add(Object.getPrototypeOf(node)); ts.forEachChild(node, collect); };
collect(sample);
for (const proto of prototypes) {
  proto.getChildren = () => { throw new Error('getChildren was called'); };
}
module.exports = ts;
`

// The runtime walk never asks the compiler for child token nodes (PERF-2):
// it walks forEachChild and rescans the tokens between children.
func TestTypeScriptRuntimeWalkMakesNoChildTokenNodes(t *testing.T) {
	node, fixture := typeScriptFixture(t)
	t.Setenv("MILLENNIUM_TYPESCRIPT", fixture)
	root := repoWith(t, map[string]string{
		"web/package.json":                         `{"name":"web"}`,
		"web/tsconfig.json":                        `{"compilerOptions": {"experimentalDecorators": true}}`,
		"web/node_modules/typescript/package.json": `{"name":"typescript","main":"index.js"}`,
		"web/node_modules/typescript/index.js":     poisonedTypeScript,
	})
	a := TypeScript(root, node)
	t.Cleanup(a.Close)
	src := "import { a, type B } from './a';\n@dec\nexport class C {\n  @dec x: B = a;\n}\nexport enum E { A }\n"
	facts, err := a.AnalyzeRuntime(t.Context(), "web/src/m.ts", []byte(src))
	if err != nil || !facts.Runtime.Known() {
		t.Fatalf("AnalyzeRuntime = %+v, %v; want known runtime code without getChildren", facts.Runtime, err)
	}
}

func TestTypeScriptDigestKeepsMeaningfulLineBreaks(t *testing.T) {
	a, _ := runtimeProject(t, `{}`)
	const head = "export function f(v: number) {\n  return"
	pairs := [][2]string{
		{head + " /* c */ v;\n}\n", head + " /*\n c */ v;\n}\n"},
		{head + " v;\n}\n", head + "\n  v;\n}\n"},
	}
	for _, pair := range pairs {
		if domain.SameRuntime(knownRuntime(t, a, "web/src/m.ts", pair[0]), knownRuntime(t, a, "web/src/m.ts", pair[1])) {
			t.Errorf("%q and %q must differ: a line break after return ends the statement", pair[0], pair[1])
		}
	}
}

func TestTypeScriptToolchainCommentsAreRuntime(t *testing.T) {
	a, _ := runtimeProject(t, `{"jsx": "react"}`)
	const body = "export const x = 1;\n"
	tests := []struct {
		name, before, after string
		line                int
	}{
		{"@jsx", "/** @jsx h */\n" + body, "/** @jsx preact.h */\n" + body, 1},
		{"@jsxFrag", "/** @jsxFrag Fragment */\n" + body, "/** @jsxFrag F */\n" + body, 1},
		{"@jsxImportSource", "/** @jsxImportSource preact */\n" + body, "/** @jsxImportSource react */\n" + body, 1},
		{"@jsxRuntime", "/** @jsxRuntime classic */\n" + body, "/** @jsxRuntime automatic */\n" + body, 1},
		{"triple-slash reference", "/// <reference path=\"a.d.ts\" />\n" + body, "/// <reference path=\"b.d.ts\" />\n" + body, 1},
		{"triple-slash types", "/// <reference types=\"node\" />\n" + body, "/// <reference types=\"bun\" />\n" + body, 1},
		{"sourceMappingURL", body + "//# sourceMappingURL=a.js.map\n", body + "//# sourceMappingURL=b.js.map\n", 2},
		{"sourceURL", body + "//# sourceURL=a.ts\n", body + "//# sourceURL=b.ts\n", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before, after := knownRuntime(t, a, "web/src/m.tsx", tt.before), knownRuntime(t, a, "web/src/m.tsx", tt.after)
			if domain.SameRuntime(before, after) {
				t.Fatal("changing only the directive must change the digest")
			}
			if !after.Holds(tt.line) {
				t.Fatalf("line %d holds a directive and must be a runtime line", tt.line)
			}
		})
	}
}

func TestTypeScriptLookalikesAreRuntime(t *testing.T) {
	a, _ := runtimeProject(t, `{"jsx": "react-jsx"}`)
	tests := []struct {
		name, path, src string
		line            int
	}{
		{"URL in a string", "web/src/m.ts", "const u = 1;\nexport const v =\n  \"http://x\"; // c\n", 3},
		{"regex holding a comment opener", "web/src/m.ts", "const u = 1;\nexport const r =\n  /a\\/*b/;\n", 3},
		{"template with slashes and a substitution", "web/src/m.ts", "const f = () => 1;\nexport const t =\n  `// ${f()}`;\n", 3},
		{"multi-line template", "web/src/m.ts", "export const t = `a\nb\nc\nd`;\n", 3},
		{"type as an identifier", "web/src/m.ts", "export const u = 1;\nlet type = 1;\n", 2},
		{"JSX in an upper-case .TSX", "web/src/Foo.TSX", "export const App = () =>\n  <div>hi</div>;\n", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if r := knownRuntime(t, a, tt.path, tt.src); !r.Holds(tt.line) {
				t.Fatalf("line %d must be runtime; runtime lines = %v", tt.line, heldLines(tt.src, r))
			}
		})
	}
}

func TestTypeScriptRuntimeUnknownWhereGitNumbersOtherwise(t *testing.T) {
	a, _ := runtimeProject(t, `{}`)
	for _, src := range []string{
		"export const x = 1; // c\u2028export const y = 2;\n",
		"export const x = 1; // c\u2029export const y = 2;\n",
		"export const x = 1; // c\rexport const y = 2;\n",
	} {
		if r := runRuntime(t, a, "web/src/m.ts", src).Runtime; r.Known() {
			t.Fatalf("%q: runtime facts must be unknown", src)
		}
	}
}

// differentialCorpus holds the constructs whose emit depends on the
// options, laid out so that types, decorators and fields sit on lines of
// their own.
const differentialCorpus = `// Line 1 is a comment.
import { Injectable, Inject, Input } from './di';
import type { Token } from './di';
import * as helpers from './helpers';
import legacy = require('./legacy');

export interface Shape {
  area(): number;
}

export enum Color {
  Red = 'red',
  Blue = 'blue',
}

export const enum Flag {
  On = 1,
}

export namespace Geometry {
  export const unit = 1;
  export type Unit = number;
}

@Injectable()
export class Service
  implements Shape
{
  static instances = 0;
  declare readonly brand: string;
  @Input()
  label:
    string;
  count:
    number;
  ready = false;
  accessor size = 2;
  #secret = 'x';

  constructor(
    @Inject('token')
    private readonly token:
      Token,
    public name: string,
  ) {
    Service.instances++;
  }

  @Input()
  area(
    scale:
      number,
  ):
    number {
    return helpers.scale(this.size, scale) + legacy.offset;
  }

  get double(): number {
    return this.size * 2;
  }

  overload(x: string): string;
  overload(x: number): number;
  overload(x: unknown): unknown {
    return x;
  }
}

export abstract class Base {
  abstract kind: string;
  protected constructor(readonly id: number) {}
}

export function make<T extends Shape>(
  shape: T,
): T {
  const copy: T = { ...shape };
  return copy;
}

export default Service;
`

// differentialRuntime and differentialErased are the lines of
// differentialCorpus that hold runtime code and those that hold none, as
// read from the corpus by hand, whatever the options: a check of the helper
// that does not go through any source map. Lines left out of both (blank
// lines, lone braces, and line 35: an undecorated field's annotation, whose
// closing semicolon the compiler maps) may go either way.
var (
	differentialRuntime = []int{
		2, 4, 5, // value imports, import = require
		11, 12, 13, 14, 16, 17, 18, // enums, const included
		20, 21, // a namespace with values
		25, 26, 29, // a decorated class, a static field
		31, 32, 33, // a decorated field and its annotation
		34, 36, 37, 38, // fields
		40, 41, 42, 43, 44, 46, // parameter properties, a decorated parameter's annotation
		49, 50, 51, 52, 54, 55, // a decorated method, its parameter and return types
		58, 59, 64, 65, // a getter, the implementation of an overload
		69, 71, // an abstract class, a parameter property
		74, 75, 77, 78, 81, // a generic function, the default export
	}
	differentialErased = []int{
		1,       // a comment
		3,       // a type-only import
		7, 8, 9, // an interface
		22,     // a type in a namespace
		27,     // implements
		30,     // a declare field
		62, 63, // overload signatures
		70, // an abstract field
	}
)

// checkHandReadLines compares the runtime lines of differentialCorpus with
// the lines read from it by hand.
func checkHandReadLines(t *testing.T, r domain.RuntimeCode) {
	t.Helper()
	for _, line := range differentialRuntime {
		if !r.Holds(line) {
			t.Errorf("line %d holds runtime code but is marked non-runtime", line)
		}
	}
	for _, line := range differentialErased {
		if r.Holds(line) {
			t.Errorf("line %d holds no runtime code but is marked runtime", line)
		}
	}
}

// transpiledLines transpiles src on its own, with the given options, and
// returns the one-based source lines the output's source map points at,
// decoded by the compiler's own decoder rather than the helper's.
func transpiledLines(t *testing.T, node, project, options, src string) []int {
	t.Helper()
	script := `
const ts = require(require('node:path').join(process.argv[1], 'node_modules', 'typescript'));
const [options, src] = JSON.parse(require('node:fs').readFileSync(0, 'utf8'));
const settings = Object.assign(ts.convertCompilerOptionsFromJson(options, '.').options, { sourceMap: true, removeComments: true });
const out = ts.transpileModule(src, { compilerOptions: settings, fileName: 'm.ts' });
const lines = new Set();
for (const m of ts.decodeMappings(JSON.parse(out.sourceMapText).mappings)) lines.add(m.sourceLine + 1);
process.stdout.write(JSON.stringify([...lines]));
`
	input, err := json.Marshal([]any{json.RawMessage(options), src})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "-e", script, filepath.Join(project, "web"))
	cmd.Stdin = strings.NewReader(string(input))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	var lines []int
	if err := json.Unmarshal(out, &lines); err != nil {
		t.Fatalf("transpile output %q: %v", out, err)
	}
	return lines
}

// The differential test of SR-12: no line the helper marks as non-runtime
// has a mapping in the compiler's own output, whichever way the options
// that change class and decorator emit are set.
func TestTypeScriptRuntimeLinesCoverEveryMappedLine(t *testing.T) {
	src := differentialCorpus
	node, _ := typeScriptFixture(t)
	for _, options := range []string{
		`{"experimentalDecorators": true, "emitDecoratorMetadata": true, "useDefineForClassFields": true, "target": "es2022"}`,
		`{"experimentalDecorators": true, "emitDecoratorMetadata": false, "useDefineForClassFields": false, "target": "es2022"}`,
		`{"emitDecoratorMetadata": false, "useDefineForClassFields": false, "target": "es2017", "module": "commonjs"}`,
	} {
		t.Run(options, func(t *testing.T) {
			a, project := runtimeProject(t, options)
			r := knownRuntime(t, a, "web/m.ts", src)
			mapped := transpiledLines(t, node, project, options, src)
			if len(mapped) == 0 {
				t.Fatal("the compiler mapped no line; the corpus is not being transpiled")
			}
			if missed := slices.DeleteFunc(mapped, r.Holds); len(missed) > 0 {
				t.Errorf("lines %v are mapped in the compiler's output but marked non-runtime", missed)
			}
			checkHandReadLines(t, r)
		})
	}
}
