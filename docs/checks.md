# Checks

Every finding blocks. A finding is printed as `[gate/rule] file:line: message`.
This page lists each rule, what it enforces, and how to fix it.

**What runs today:** the `toolchain` and `structure` gates, for Go, TypeScript
and Python. The other checks in [design.md](design.md#checks) (format and
types, banned APIs, suppressions, secrets, tests required, coverage, supply
chain, shell) are being built.

How each language is read:

| Language | Parser | Needs |
|---|---|---|
| Go | The Go standard library, inside the engine | Nothing extra |
| TypeScript | The project's own `typescript` package, through a small helper run with `node` | The project's dependencies installed (`npm ci`) and TypeScript 6 or older: TypeScript 7 no longer ships the compiler API the helper uses, and blocks with a message saying so |
| Python | Python's `ast` module, through a small helper | Python 3.10 or newer |

The helpers ship inside the engine. They read nothing but the file they are
given and the project's own `typescript`; Python runs isolated (`-I`), so
modules in the repository or on `PYTHONPATH` cannot run in their place.

## `toolchain`

| Rule | Fires when | Fix |
|---|---|---|
| `tool-missing` | A tool the configured checks need cannot be found on `PATH`. | Install it. The message says which tool and why it is needed. |

Required tools per language: Go needs `go` and `gofmt`; TypeScript needs
`node`; Python needs `python3`, `python` or `py`; `"python": {"typeCheck":
"mypy"}` adds `mypy`. A missing tool always blocks: a check that cannot run
never counts as a pass.

## `structure`

Reads the code of every changed (or, in an audit, every) file of a language
its component lists.

### Limits

Every function must meet these limits:

| Rule | Limit | Notes |
|---|---|---|
| `function-length` | 50 lines, from the first line of the function to its last | Test files are exempt: table-driven tests are long by nature. |
| `nesting-depth` | 3 levels | Each `if`, loop, `switch`, `select` and function literal opens a level; `else if` does not. |
| `cognitive-complexity` | 15 | See below. |

Generated files are skipped: a `Code generated ... DO NOT EDIT.` header in
Go, or `@generated` / `DO NOT EDIT` in the first lines of a TypeScript or
Python file. Test files are `_test.go`; `*.test.ts(x)`, `*.spec.ts(x)` and
files under `__tests__/`; `test_*.py`, `*_test.py` and files under `tests/`.

Which functions are measured: in Go, every function and method. In TypeScript,
function declarations, functions and arrow functions assigned to top-level
constants, class methods, accessors and arrow-function properties, methods of
top-level object literals, and callbacks passed to top-level calls (keyed like
`app.get("/users")` or `test("rejects bad input")`). In Python, module-level
functions and class methods, nested classes included. Nested functions count
toward the function that contains them.

**Cognitive complexity** measures how hard a function is to follow, using the
SonarSource definition:

- branching and looping structures cost 1, plus 1 for each level they are
  nested in: `if`, `switch`/`match`, `select`, every loop, a ternary
  (`a ? b : c`, `b if a else c`), and `catch`/`except`;
- `else if`/`elif` and `else` cost 1, and so does a loop's `else` in Python;
- each run of the same logical operator costs 1: `a && b && c` is 1,
  `a && b || c` is 2 (`??`, `and` and `or` count the same way);
- `goto`, labelled `break` and `continue`, and a direct recursive call cost 1.

**Fix:** split the function by responsibility; return early instead of
nesting; move a complex condition into a well-named helper. A fix that adds one
more branch to an already branchy function is the wrong fix.

**Existing code:** when `gate` compares with a base, a function that already
exceeded a limit may stay over it but must not get worse. `audit` applies the
full limits to everything.

### Layer rules

| Rule | Fires when | Fix |
|---|---|---|
| `layer-import` | A file imports something its layer may not depend on. | Depend on an interface (a port) in an inner layer instead, and implement it outward. The message names both layers and what is allowed. |

Only imports a change adds are checked; existing imports are held to the rules
by `audit`. The layers and their rules come from the convention: see
[Clean Architecture](conventions/clean-architecture.md).

### Problems that block because a check cannot run

| Rule | Fires when | Fix |
|---|---|---|
| `analysis-failed` | A file cannot be read or parsed: a syntax error, a symlink, a file larger than 4 MiB, a Go file without `go.mod`, a TypeScript project whose dependencies are not installed or whose TypeScript is 7 or newer, Python older than 3.10. | Fix the cause the message names. Exclude generated or vendored files in the config. |
| `component-ambiguous` | Two components match a file equally. | Make one component's pattern more specific. |
| `analysis-unavailable` | A component lists a language whose code checks this engine version does not have. | None: the code cannot pass until an analyzer for it ships. |
| `convention-unavailable` | A component selects a convention this engine version does not have yet (`react-feature-based` today), or one with no rules for the component's language. | None yet; remove the convention only if you accept losing its rules. |

## Gate failures

| Rule | Fires when |
|---|---|
| `gate-failed` | A gate itself errored or crashed. The change is blocked rather than passed; the message says why. Report it as a bug. |
