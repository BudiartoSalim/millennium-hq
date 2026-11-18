# Checks

Every finding blocks. A finding is printed as `[gate/rule] file:line: message`.
This page lists each rule, what it enforces, and how to fix it.

**What runs today:** the `toolchain`, `structure`, `shell`, `hygiene`, `build`,
`tests` and `deps` gates, for Go, TypeScript, Python and shell.

**Tiers.** The **fast** tier is what hooks run on every agent turn; the
**full** tier is the default for `gate` and `audit`. They differ in two
places: the fast tier checks that changed logic comes with changed tests, and
the full tier also runs the test suites and measures coverage; and the
supply-chain checks that need the network (release age, vulnerabilities,
registry signatures) run in the full tier only.

How each language is read:

| Language | Parser | Needs |
|---|---|---|
| Go | The Go standard library, inside the engine | Nothing extra |
| TypeScript | The project's own `typescript` package, through a small helper run with `node` | The project's dependencies installed (`npm ci`) and TypeScript 6 or older: TypeScript 7 no longer ships the compiler API the helper uses, and blocks with a message saying so |
| Python | Python's `ast` module, through a small helper | Python 3.10 or newer |
| Shell | Line by line, for comments and risky constructs; `bash -n` and shellcheck in the `shell` gate | `bash`, and `shellcheck` unless `"shell": {"lint": "off"}` |

Shell scripts are `.sh` and `.bash` files, and extensionless files whose
shebang names `sh`, `bash`, `dash` or `ksh` (such as `bin/deploy`). Function
length, nesting and complexity need a full shell grammar, so they do not apply
to shell yet; the maintainability reviewer covers them.

The helpers ship inside the engine. They read nothing but the file they are
given and the project's own `typescript`; Python runs isolated (`-I`), so
modules in the repository or on `PYTHONPATH` cannot run in their place.

## `toolchain`

| Rule | Fires when | Fix |
|---|---|---|
| `tool-missing` | A tool the configured checks need cannot be found on `PATH`. | Install it. The message says which tool and why it is needed. |

Required tools per language: Go needs `go` and `gofmt`; TypeScript needs
`node`; Python needs `python3`, `python` or `py`; `"python": {"typeCheck":
"mypy"}` adds `mypy`; shell needs `bash`, plus `shellcheck` unless
`"shell": {"lint": "off"}`. A missing tool always blocks: a check that cannot run
never counts as a pass.

## `structure`

Reads the code of every changed (or, in an audit, every) file of a language
its component lists.

### Limits

Every function must meet these limits. The values shown are the defaults;
a convention pack can set its own, and a component can override them with a
reason (see [Function limits](configuration.md#function-limits)).
`config explain <file>` shows the limits in force for a file.

| Rule | Default limit | Notes |
|---|---|---|
| `function-length` | 50 lines, from the first line of the function to its last | Test files are exempt: table-driven tests are long by nature. |
| `nesting-depth` | 3 levels | Each `if`, loop, `switch`, `select` and function literal opens a level; `else if` does not. |
| `cognitive-complexity` | 15 | See below. |

Generated files outside tested layers are skipped: a `Code generated ...
DO NOT EDIT.` header in Go, or `@generated` / `DO NOT EDIT` in the first
lines of a TypeScript or Python file. In a **tested layer** (`domain` and
`application` under clean-architecture) the marker exempts nothing, since
anyone who edits a file can add one: a marked file there is held to the
limits like hand-written code, and each finding on it says so. Declare
generated code in a tested layer with
[`exclude`](configuration.md#generated-code) in the config instead. Test files are
`_test.go`; `*.test.ts(x)`, `*.spec.ts(x)` and files under `__tests__/`;
`test_*.py`, `*_test.py` and files under `tests/`.

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
| `convention-unavailable` | A component's convention has no pack (only possible when the config was not loaded through the engine), or its pack governs the component's language but has no layer paths for it. | Add the language's paths to the pack, or remove the language from the pack's `languages`. |

## `shell`

Runs on every changed script of a component that lists `shell`.

| Rule | Fires when | Fix |
|---|---|---|
| `syntax-error` | `bash -n` cannot parse the script. Nothing else is checked until it parses. | Fix the syntax error. |
| `strict-mode` | An executable script (one with a shebang) does not switch on `errexit` and `nounset`, plus `pipefail` for bash and ksh, within its first 40 lines. | Add `set -euo pipefail` (bash) or `set -eu` (sh) after the shebang. A script without a shebang is a library meant to be sourced and is exempt. |
| `shellcheck` | shellcheck reports a problem on a line the change touched (every line, for a new script or an audit). The code, such as `SC2086`, links to shellcheck's explanation. | Fix it, or suppress that one code with a written reason: `# shellcheck disable=SC2016 # printed for the user to copy`. |

An existing script that already lacked strict mode is not blocked when it is
edited; `millennium-code-qc audit` lists it. The finding is about the whole
file and is reported on line 1, and no `millennium:allow` comment covers it
(see [Waivers](#waivers)). A test harness that must keep
running after a failing test, and so enables `errexit` only inside each test
function, can have the rule switched off for its files:
`{ "check": "shell/strict-mode", "paths": ["tests/**/*.sh"], "reason": "..." }`
in the config's [`disable`](configuration.md#disabling-a-check). shellcheck runs with `--norc`, so a
`.shellcheckrc` cannot switch checks off, and with `-x
--source-path=SCRIPTDIR`, so `# shellcheck source=` paths resolve relative to
the script. A library without a shebang or `# shellcheck shell=` directive is
linted as bash.

## `hygiene`

Runs on the lines a change touched (every line, for a new file or an audit).

| Rule | Fires when | Fix |
|---|---|---|
| `secret` | Any text file, in a component or not, holds something shaped like a credential: AWS, GitHub, Slack, Google, Stripe, Anthropic and OpenAI keys, private keys, JSON Web Tokens, or a random-looking quoted value assigned to a name such as `password`, `token` or `api_key`. Placeholders (`changeme`, `${VAR}`, `<your-key>`, `example`) are ignored. | Load it from the environment or a secret store, and rotate it if it was real. A deliberate fixture can be waived (below). |
| `banned-construct` | Analysed code uses something that is a common security hole: `eval`, `new Function`, Python `exec`; a command run through a shell (`shell=True`, `os.system`, `exec.Command("sh", "-c", ...)`, `child_process.exec`, `curl ... \| sh`); TLS verification off (`InsecureSkipVerify`, `verify=False`, `rejectUnauthorized: false`); unsafe deserialisation (`pickle.loads`, `yaml.load` without a safe loader); raw HTML (`innerHTML`, `dangerouslySetInnerHTML`); a TypeScript `any`. | Use the safe form the message names. If the construct is truly needed, waive it with a reason. |
| `suppression-without-reason` | A directive switches a checker off without saying why: `eslint-disable`, `@ts-ignore`, `@ts-expect-error`, `//nolint`, `#nosec`, `# noqa`, `# type: ignore`, `# pragma: no cover`, `# shellcheck disable=`, coverage ignores (`istanbul`, `c8` and `v8 ignore`, and Node's `node:coverage disable` and `node:coverage ignore next`). | Add the reason after ` -- ` or a second comment marker, or as a comment on the line above: `//nolint:gosec // the hash is a cache key, not a secret`. A reason has at least ten letters. |
| `todo-without-link` | A `TODO`, `FIXME`, `XXX` or `HACK` marker has no issue reference. | Link one (`#123`, `PROJ-45` or a URL), or do the work now. |
| `scan-failed` | A file cannot be read or analysed, so it could hold anything. | Fix the cause; exclude large generated or vendored files in the config. |

Binary files (a NUL byte in the first 8000 bytes, as git decides) are skipped.

### Waivers

`millennium:allow <rule> -- <reason>` on the line itself or the line directly
above waives that rule there, in any file and any comment syntax:

```python
# millennium:allow secret -- the AWS documentation's example key, used as test input
EXAMPLE_KEY = "AKIAIOSFODNN7EXAMPLE"
```

Waivers apply to `secret` and `banned-construct`. A waiver needs a reason of
at least ten letters, or it waives nothing. A waiver naming any other rule
has no effect, wherever it is written. Any other rule, such as
`shell/strict-mode`, is switched off for the files it does not fit by a
[`disable` entry with `paths`](configuration.md#disabling-a-check) in the
config, which the user applies.

## `build`

Runs each language's own tools on the modules and projects a change touches.

| Rule | Fires when | Fix |
|---|---|---|
| `gofmt` | A changed Go file is not gofmt-formatted. | Run `gofmt -w` on it. |
| `vet` | `go vet ./...` reports a problem, or a compile error, anywhere in a touched module. | Fix it. Compile errors outside the changed files still block: the change may be what broke them. |
| `type-error` | The project's own `tsc --noEmit` reports an error in a touched TypeScript project, or mypy (when `"python": {"typeCheck": "mypy"}`) reports one in the changed Python files. | Fix the type error. |
| `tsconfig-weakened` | The effective tsconfig, with `extends` resolved by `tsc --showConfig`, does not set `"strict": true`, or switches an option of the strict family (`strictNullChecks`, `noImplicitAny`, ...) back off. | Restore strict mode. |
| `tsconfig-missing` | A TypeScript file has no `tsconfig.json` at or above it inside its component. | Add one. |
| `tool-failed` | A tool could not run or failed without output the engine can read. | The message carries the tool's own output. |

TypeScript is checked with the project's own compiler from its `node_modules`,
or a parent's in a workspace that hoists it; `go vet` runs with
`GOFLAGS=-mod=readonly`, so it cannot change `go.mod`.

## `tests`

Changed logic in a **tested layer** (`domain` and `application` under
clean-architecture) is held to its tests. A file is logic when it has at least
one function and is not test code; files that only declare types or
interfaces are exempt. A generated-code marker (`Code generated ... DO NOT
EDIT.`, `@generated`, `DO NOT EDIT`) does not exempt a file here, whether it
was there before or added in the change: `tests-missing` and
`uncovered-change` apply to it, and their findings say that the marker does
not exempt files in a tested layer. Generated code in a tested layer is
declared with [`exclude`](configuration.md#generated-code), which no check reads
past.

Only **runtime code** counts: what is left once comments, whitespace and, in
TypeScript, the syntax the compiler erases are taken away. The erased syntax
is `interface` and `type` declarations, `declare` declarations, type-only
imports and exports (`import type`, `export type`), type annotations and
overload signatures without a body; the project's own TypeScript compiler
decides, from the code it emits. Code that only looks like a type still
counts: enums (`const enum` and `declare const enum` included), namespaces
with values, class fields that are not `declare` or `abstract`, parameter
properties, decorators and the annotations of what they decorate, value
imports, `import x = require(...)` and `export =`. So do the comments the
toolchain reads: Go's `//go:` directives, `//line`, `// +build`, `//export`,
`//extern` and the cgo preamble; TypeScript's triple-slash directives, JSX
pragmas (`@jsx`, `@jsxFrag`, `@jsxImportSource`, `@jsxRuntime`) and
`//# sourceMappingURL=` and `//# sourceURL=`; and Python's encoding
declaration. A Python docstring is a string, so it is runtime code too.

When the engine cannot tell which code is runtime, it treats all of it as
runtime: for a Python file that declares an encoding other than UTF-8, for
TypeScript or Python content that is not valid UTF-8 or that ends a line
with anything but a line feed (a lone carriage return, U+2028, U+2029), and
whenever the analysis of a version fails.

| Rule | Tier | Fires when | Fix |
|---|---|---|---|
| `tests-missing` | both | Changed logic comes without a change to its tests (in an audit: has no tests). A modified file whose runtime code is unchanged against the base, token for token, needs no test change: a change to comments, whitespace or (TypeScript) erased type syntax alone does not fire. Changing a string, a docstring, an identifier, an operator or Python indentation does. A new file always needs its tests, and so does a modified file whose base version cannot be read or analysed. A test counts when it is, in Go, any `_test.go` file in the same package; in TypeScript, `<name>.test.ts`/`.spec.ts` or `__tests__/<name>.ts` in the component; in Python, `test_<name>.py` or `<name>_test.py` in the component. | Add or update the test. Writing it first is welcome, not required. |
| `tests-failed` | full | A test suite of a touched module or component fails, or does not compile. | Fix the code or the test. |
| `uncovered-change` | full | A changed executable line of logic never runs in any test of its module (any test, not only those of its own package). Only lines that hold runtime code count: a comment line or an erased declaration that the coverage report gives zero hits (as Node's report does through source maps) is not reported, while a line with both code and a comment is. In an audit every line counts. | Add a test that exercises it, or remove the line if no caller can reach it. |
| `tests-unconfigured` | full | A TypeScript component has no test recipe, so its tests cannot be run. | Add `"test"` to the component (see [Configuration](configuration.md#test-recipes)). |
| `tool-failed` | full | A suite could not run at all. | The message says why. |

How each language's suite runs:

| Language | Runner | Coverage |
|---|---|---|
| Go | `go test -count=1 -coverpkg=./... ./...` per touched module | the coverage profile |
| Python | `unittest` discovery from the component root (`tests/` when present), through a helper | `sys.monitoring` on Python 3.12+, coverage.py below |
| TypeScript, or any component with a `test` recipe | the recipe's command | the lcov report it writes |

When a suite fails, its files' coverage is not reported on top: the failure
already blocks.

## `deps`

The supply chain of a change. It reads the manifests and lockfiles the change
touches, in any directory, whatever languages the config governs.

| Rule | Tier | Fires when | Fix |
|---|---|---|---|
| `unapproved-dependency` | both | A changed lockfile locks a package version that was not locked before and no approval record covers it. Transitive packages count. | Vet it (the `vet-dependency` skill drafts the record into a proposal) and have the user apply it, see [Approving a dependency](configuration.md#approving-a-dependency). |
| `install-script` | both | A newly locked npm package has an install script (`hasInstallScript` in `package-lock.json`). | Pick another package. With `ignore-scripts` on it will not work; with it off it can run anything at install. |
| `unpinned` | both | A touched `package.json` dependency is not an exact version (`^`, `~`, ranges, tags), or a touched requirements line is not pinned with `==`. Local `file:`, `workspace:` and `link:` specs and `peerDependencies` are allowed. | Pin it. Set `save-exact=true` in `.npmrc`. |
| `unhashed` | both | A touched line of `requirements*.txt` has no `--hash`. (`requirements*.in` is the input to a resolver and needs no hashes.) | Compile it with hashes (`pip-compile --generate-hashes`, `uv pip compile --generate-hashes`) and install with `--require-hashes`. |
| `unpinned-action` | both | A touched `uses:` in `.github/workflows/*.yml` is not pinned to a full 40-character commit SHA. Local `./` actions and `docker://` are skipped. | Pin the SHA, with the tag in a comment: `uses: actions/checkout@<sha> # v4.2.2`. |
| `unpinned-image` | both | A touched `FROM` in a Dockerfile, or `image:` in a compose file, has no `@sha256:` digest. Earlier build stages, `scratch` and `$VARIABLE` images are skipped. | Pin the digest: `FROM node:22@sha256:…`. |
| `npmrc-unsafe` | both | A changed npm project has no `.npmrc` beside its `package.json`, or it does not set `ignore-scripts=true` and `save-exact=true`. | Add them to the project's `.npmrc`. |
| `too-new` | full | A newly locked version was published less than 7 days ago, by the npm registry, the Go module proxy or PyPI. | Wait, or, for a release that fixes a vulnerability, record the advisory as `securityPatch` in its approval. |
| `known-vulnerability` | full | OSV lists an advisory for any package the changed lockfiles lock, new or not. | Upgrade to a fixed version. |
| `integrity-weakened` | full | A changed Go module's `go env` turns off checksum verification: `GOSUMDB=off`, `GONOSUMCHECK`, `GONOSUMDB=*`, `GOINSECURE`, or `GOFLAGS` with `-insecure` or `-mod=mod`. | Unset it. |
| `signatures-failed` | full | `npm audit signatures` fails for a changed npm project with packages installed. With nothing installed there is nothing to verify; `npm ci` checks the lockfile's integrity hashes when it installs. | Reinstall from the lockfile with `npm ci`; find the package that fails. |

These block because the check could not run, and cannot be disabled:

| Rule | Fires when |
|---|---|
| `manifest-unreadable` | A changed manifest cannot be parsed, or `package-lock.json` is lockfile version 1 (regenerate it with npm 7 or newer). |
| `invalid-approval` | An approval record cannot be read, is invalid, or its file name does not match what it approves. |
| `registry-failed` | A registry, OSV, or `go env` could not answer. A supply-chain check never passes because the network was down. |

What counts as a lockfile: `package-lock.json` (v2 and v3), `go.sum` (the
modules with a code hash; `/go.mod`-only lines are module-graph metadata and
never build), and `requirements*.txt`. `pnpm-lock.yaml`, `yarn.lock`,
`uv.lock`, `poetry.lock` and `Pipfile.lock` are not read yet; a project that
uses them gets the pinning checks but not the approvals.

## Gate failures

| Rule | Fires when |
|---|---|
| `gate-failed` | A gate itself errored or crashed. The change is blocked rather than passed; the message says why. Report it as a bug. |
