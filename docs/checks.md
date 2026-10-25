# Checks

Every finding blocks. A finding is printed as `[gate/rule] file:line: message`.
This page lists each rule, what it enforces, and how to fix it.

**What runs today:** the `toolchain` and `structure` gates, for Go. The other
checks in [design.md](design.md#checks) (format and types, banned APIs,
suppressions, secrets, tests required, coverage, supply chain, shell) are
being built.

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
| `function-length` | 50 lines, from `func` to the closing brace | Test files (`_test.go`) are exempt: table-driven tests are long by nature. |
| `nesting-depth` | 3 levels | Each `if`, loop, `switch`, `select` and function literal opens a level; `else if` does not. |
| `cognitive-complexity` | 15 | See below. |

Generated files (with a `Code generated ... DO NOT EDIT.` header) are skipped.

**Cognitive complexity** measures how hard a function is to follow, using the
SonarSource definition:

- `if`, `switch`, `select`, `for` and `range` cost 1, plus 1 for each level
  they are nested in;
- `else if` and `else` cost 1;
- each run of the same logical operator costs 1: `a && b && c` is 1,
  `a && b || c` is 2;
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
| `analysis-failed` | A file cannot be read or parsed (syntax error, symlink, larger than 4 MiB, no `go.mod`). | Fix the file. Exclude generated or vendored files in the config. |
| `component-ambiguous` | Two components match a file equally. | Make one component's pattern more specific. |
| `analysis-unavailable` | A component lists a language whose code checks this engine version does not have yet (TypeScript and Python today). | None yet: the code cannot pass until the analyzer ships. |
| `convention-unavailable` | A component selects a convention this engine version does not have yet (`react-feature-based` today), or one with no rules for the component's language. | None yet; remove the convention only if you accept losing its rules. |

## Gate failures

| Rule | Fires when |
|---|---|
| `gate-failed` | A gate itself errored or crashed. The change is blocked rather than passed; the message says why. Report it as a bug. |
