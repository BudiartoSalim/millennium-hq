# Field-feedback blockers: vanished cwd, and tests that see only runtime code

## Goal

Remove the three engine issues that blocked a real `/millennium:tech-lead`
run on millennium 0.3.1 (field feedback, 2026-11-18):

- **B1.** When the session's working directory had been deleted (a removed
  worktree), every hook event failed with `find repository root: git
  rev-parse --show-toplevel: exit status 128: cannot change to '<dir>'`. Every
  tool call was blocked, `cd` included, until the user ran `! cd <repo>`.
- **B2.** `uncovered-change` reported comment lines and erased TypeScript
  declarations (a doc comment and `export interface X`) as "never run" while
  every function was covered. With `tsc --sourceMap` and
  `node --enable-source-maps --experimental-test-coverage`, Node's lcov gives
  such lines `DA:<line>,0` (reproduced: a leading doc comment comes out as
  `DA:1,0`). Agents "fixed" it by moving code around.
- **B3.** `tests-missing` fired on edits that changed only comments, so an
  engineer reverted two stale-comment fixes instead of adding filler tests.

The gate's intent is unchanged: changed **runtime** code must come with a test
change and must run in a test. The fix is that the engine learns which lines
and tokens are runtime code.

## Acceptance criteria

Vanished working directory (B1):

- AC-1: A hook event whose `cwd` no longer exists, and whose nearest existing
  parent directory is inside an opted-in repository, is evaluated against that
  repository: an allowed Bash command (`cd <repo>`) passes with exit 0 and no
  denial, and the guard still denies what it denies from the repository root
  (an Edit or Write of `.millennium/config.json`).
- AC-2: A hook event whose `cwd` no longer exists, and whose nearest existing
  parent is outside any git repository, passes with exit 0, as any cwd outside
  a repository does today.
- AC-3: A `cwd` that exists but cannot be used for another reason (git fails
  on it for any reason other than the directory not existing, or the path
  exists but is not a directory) still blocks with exit 2 and a message naming
  the problem.

Coverage counts runtime lines only (B2):

- AC-4: In the full tier, a changed line of a tested-layer file that holds no
  runtime code is never reported by `uncovered-change`, even when the coverage
  report gives it zero hits. No runtime code means only comments and
  whitespace in Go, TypeScript and Python. In TypeScript it also means the
  parts of a line that the compiler erases: `interface` and `type`
  declarations, `declare` declarations, type-only imports and exports
  (`import type`, `export type`), type annotations, and overload signatures
  without a body.
- AC-5: A changed line that holds runtime code and has zero hits is still
  reported, including TypeScript constructs that look like types but emit
  code. This covers `enum` (const enums included), a `namespace` with values,
  class fields with initializers, parameter properties (`constructor(private
  x: T)`), decorators, and `import x = require(...)`. A line with both runtime
  code and a comment or type counts as runtime.

Comment-only edits need no test change (B3):

- AC-6: A modified file in a tested layer whose runtime code is unchanged
  against the base, token for token, is not reported by `tests-missing`.
  Tokens are compared after dropping comments and whitespace, and in
  TypeScript also erased type syntax. This holds in Go, TypeScript and Python.
- AC-7: Any change to runtime tokens still needs a test change. That includes
  a string literal (a Python docstring included), an identifier, an operator,
  or Python indentation, which is syntax.
- AC-8: A file added in the change, and every file of an audit (no base),
  behaves as today: production logic needs tests.
- AC-9: When the base version cannot be read or analysed, or an analyzer
  reports no runtime-code facts for either version, the file counts as
  changed (fail closed).

Generated-code markers in tested layers (TM-3, added at the user's request
on 2026-11-18):

- AC-11: A file in a tested layer that carries a generated-code marker is
  checked like hand-written code. Markers are Go's `// Code generated ... DO
  NOT EDIT.`, TypeScript's `@generated`, and Python's `DO NOT EDIT`.
  `tests-missing`, `uncovered-change` and the limits all apply to it. This
  holds whether the marker was there before or added in the change.
- AC-12: Such a file that the config's `exclude` covers is skipped, as
  today. `exclude` is how the user declares generated code in a tested layer.
- AC-13: Outside tested layers, the marker still exempts a file from the
  limits, as today.
- AC-14: When a finding hits a tested-layer file that carries a marker, the
  message says that the marker does not exempt files in a tested layer, and
  that generated files there are declared with `exclude` in the config.
- AC-15: `docs/checks.md` and `docs/configuration.md` describe AC-11 to
  AC-14.

Documentation:

- AC-10: `docs/checks.md` states that `tests-missing` ignores changes to
  comments, whitespace and (TypeScript) erased type syntax, and that
  `uncovered-change` counts only lines with runtime code. `docs/hooks.md`
  states how a deleted working directory is handled.

## Non-goals

- The workflow and playbook items of the feedback (W1-W6, P1-P4), and R1-R5.
  They are listed as follow-ups in the decision log.
- Treating string-literal documentation (Python docstrings) as comments.
- Coverage formats other than those the engine reads today (Go profile,
  Python report, lcov).

## Constraints

- The engine stays Go standard library only. The helpers use only their
  runtime's standard library, plus the project's own TypeScript compiler,
  which the TypeScript helper already uses.
- One extra analysis of the base version per modified candidate file, at
  most. The analysis cache serves repeats. Cached facts from an older engine
  version must not be read as "no runtime code".
- Every function stays within the engine's own limits (about 50 lines,
  nesting 3, cognitive complexity 15).

## Risk

- **Gate bypass (input).** Repository files are untrusted, and an agent wants
  its change to pass. If runtime code were classified as a comment or type,
  untested code would get through `tests-missing` and `uncovered-change`.
  This is the main risk.
- **Hook input.** The hook's `cwd` comes from the harness event. Walking up
  from it must not reach a repository the event could not otherwise name, or
  turn an error into a pass.
- No auth, secrets, crypto, money or concurrency.

## Approach

Options considered:

1. **Strip comment lines with a regex on the diff.** Rejected: it is not
   language-aware (`//` inside a string, `/* */ code()` on one line), and it
   cannot see erased types.
2. **Trust the coverage tool and hide lcov lines without mappings.**
   Rejected: lcov carries no "unmapped" marker, and B3 has no coverage data.
3. **Chosen: the analyzers report runtime code.** Each analyzer already
   parses the file. It adds two facts: the lines that hold runtime tokens, and
   a digest of the runtime token sequence. `uncovered-change` ignores
   zero-hit lines outside the runtime lines (B2). `tests-missing` skips a
   modified file whose base and working digests match (B3). Classification
   errs toward runtime: anything not positively identified as a comment,
   whitespace or erased type syntax is runtime.

B1: when `git -C <cwd>` cannot run because `<cwd>` does not exist, open the
repository from the nearest existing ancestor instead. That is the main
checkout for a removed worktree under `.claude/worktrees/`. Every other
failure still blocks. Reading `CLAUDE_PROJECT_DIR` was rejected: it is
specific to Claude Code (the pi extension does not set it), and the ancestor
walk gives the same answer here.

## Security requirements

From the threat model (security-engineer, at 770fb6b). The attacker is an
agent whose change is checked, or a hostile repository file.

B1, the vanished cwd:

- SR-1: The walk moves up only when the OS reports that the cwd does not
  exist (`Lstat` gives `ErrNotExist`), never because of git's stderr. A
  file, a permission error, a dangling symlink, or an existing directory git
  rejects all block. *Test:* one table row per case.
- SR-2: The walk starts only from an absolute, already-clean cwd. It moves
  up one parent at a time, stops at the first ancestor that exists, and is
  bounded. *Test:* a relative cwd blocks; a cwd with `..` blocks; the walk
  never goes past the first existing ancestor.
- SR-3: The root still comes from `git rev-parse --show-toplevel` in the
  ancestor, and every event gets the decision it would get with that ancestor
  as cwd. *Test:* hook tests with a real repository and a removed
  `.claude/worktrees/x` cwd: Bash `cd <repo>` passes; an Edit of
  `.millennium/config.json` is denied; Stop with a failing gate blocks.
- SR-4: Hook tool paths are mapped without the engine process's working
  directory. A relative path joins the event cwd. Symlinks are resolved on
  both sides: the root, and the longest existing prefix of the target. A
  path that cannot be mapped for any reason other than lying outside the
  repository blocks (exit 2) and is never dropped. *Test:* with the process
  cwd removed, an Edit of `<root>/.millennium/config.json` is denied, and so
  is a relative `../../.millennium/config.json` from a removed worktree cwd. A
  mapping error makes the hook exit 2. An Edit through a symlinked path to
  the repository is denied.

B2 and B3, runtime facts:

- SR-5: Runtime facts have an explicit known or unknown state. They are
  unknown when an analyzer does not report them, when a helper answer lacks
  them, or when analysis fails. With unknown facts, every touched line counts
  for `uncovered-change` and the file counts as changed for `tests-missing`.
  Two unknown digests never compare equal. A known file with no runtime lines
  is not the same as unknown. *Test:* a gate test with a fake analyzer that
  reports no runtime facts; a decode test for an answer without the fields.
- SR-6: The decoder rejects the whole helper answer when the digest is not
  64 lowercase hex characters, or when a runtime line is not an integer from
  1 to the content's line count. *Test:* a decode table.
- SR-7: The digest is SHA-256 over an unambiguous, length-prefixed encoding
  of each runtime token's kind and exact text. Hashing the compiler's emitted
  output, as SR-12 allows, also meets this. *Test:* `["ab","c"]` and
  `["a","bc"]` differ; `'a'` and `"a"` differ; docstrings that differ only in
  text differ.
- SR-8: The digest keeps every line break that changes meaning: Go's
  automatic semicolons (including one caused by a multi-line `/* */`
  comment), TypeScript's automatic semicolon insertion (including a line
  break inside a comment), and Python's NEWLINE, INDENT and DEDENT. *Test:*
  pairs that must differ: Go `return /* */ f()` and `return /*\n*/ f()`; TS
  `return /* c */ v` and `return /*\n c */ v`; TS `return v` and
  `return\nv`; Python `x = 1 \\\n-1` and `x = 1\n-1`.
- SR-9: Comments that the toolchain reads count as runtime, both as tokens
  and as lines:
  - Go: `//go:` directives, `//line`, `// +build`, `//export`, `//extern`,
    and the cgo preamble;
  - TypeScript: JSX pragmas (`@jsx`, `@jsxFrag`, `@jsxImportSource`,
    `@jsxRuntime`), triple-slash directives, `//# sourceMappingURL=` and
    `//# sourceURL=`;
  - Python: an encoding declaration.

  *Test:* one per directive. Changing only the directive changes the digest,
  and its line is a runtime line.
- SR-10: Runtime facts are unknown for a Python file that declares an
  encoding other than UTF-8, and for any helper-language content that is not
  valid UTF-8. (A `utf-7` cookie turns `# +AAo-print(1)` into code that
  runs.) *Test:* that utf-7 sample counts as changed and its line counts as
  runtime.
- SR-11: Classification comes from each language's own parser or scanner,
  never from patterns. Anything not positively identified as a comment,
  whitespace or erased type syntax is runtime. *Test:* these are runtime:
  `"http://x" // c`, a regex literal containing `/*`, a template literal
  with `//` and `${f()}`, Python `s = "# no"`, `let type = 1`, and a file
  named `Foo.TSX`.
- SR-12: TypeScript erasure is decided by the compiler. The recommended
  approach takes the runtime digest and the runtime lines from the
  comment-free `ts.transpileModule` emit and its source map. Either way,
  these stay runtime: enums (const and `declare const` included), namespaces
  with values, class fields unless `declare` or abstract, parameter
  properties, decorators and the annotations of decorated members,
  `import x = require()`, `export =`, `accessor`, and value imports. *Test:*
  one per construct, plus a differential test: no line marked non-runtime
  has a source-map mapping in `transpileModule` output, with
  `emitDecoratorMetadata` and `useDefineForClassFields` both on and both off.
- SR-13: Runtime lines use git's numbering, where only `\n` ends a line. Go
  positions are physical (`PositionFor(pos, false)`), so `//line` cannot
  shift them; the same goes for the function limits. A TypeScript or Python
  file with a line terminator git does not count (a lone CR, U+2028,
  U+2029) has unknown runtime lines. *Test:* one per language.
- SR-14: The base is read with `ReadAt` at `Changes.Base`. A base that is not
  found, cannot be read, or fails analysis counts as changed. Equal digests
  take a file out of `tests-missing` only. Added files and audits are never
  compared. *Test:* a fake SourceReader records the revision asked for. An
  audit never calls `ReadAt`. A file with equal digests still gets
  `uncovered-change` for a zero-hit runtime line.
- SR-15: Each modified candidate costs at most one extra analysis. A helper
  failure on the base counts as changed. *Test:* a counting fake gives at
  most 2N analyses for N candidates.
- SR-16: Node's coverage directives (`node:coverage disable`,
  `node:coverage ignore next`) are suppressions, so they need a reason like
  istanbul, c8 and v8 ones. *Test:* hygiene table rows.

TM-3, generated markers:

- SR-17: In a tested layer, whether a file is exempt from the tests gate and
  the limits depends only on what the agent cannot change: the config's
  `exclude`, and the layer the config and packs assign. It never depends on
  the file's own content. *Test:* adding `// Code generated by x. DO NOT
  EDIT.` (Go), `@generated` (TS) or `# DO NOT EDIT` (Python) to an
  application-layer file with untested logic and an over-long function makes
  `tests-missing`, `uncovered-change` and the function-length limit fire. The
  same file under `exclude` gets no findings.

Accepted risks:

- The project's own TypeScript package is trusted. It already decides what a
  function is.
- The Stop hook compares with HEAD, so walking to the main checkout of a
  removed worktree exposes nothing that was hidden before.
- Bundler hint comments (`/*#__PURE__*/`, webpack magic comments) count as
  non-runtime. They change bundles, not test runs.

## Decision log

| id | from | severity | finding | disposition | reason |
|---|---|---|---|---|---|
| TM-1 (SEC-1) | security-engineer | high | `RepoPath` uses `os.Getwd`, and the hook drops paths it cannot map. A hook process started in a removed cwd would let the guard see no paths after B1. | fix | SR-4, package P1b |
| TM-2 (SEC-2) | security-engineer | high | The Python helper parses a str and ignores the PEP 263 encoding cookie, so code can hide in a utf-7 "comment". | fix | SR-10, package P2 |
| TM-3 (SEC-3) | security-engineer | high | A generated-code marker comment (`DO NOT EDIT`, `@generated`) takes a tested-layer file out of the tests gate and the limits, and no gate notices it being added. | fix | The user chose to fix it in this branch (2026-11-18): AC-11 to AC-15 and SR-17, package P3. Outside tested layers the marker still exempts a file from the limits. That residual risk is low, since it affects the limits only. |
| L-2 | tech lead | n/a | The repository has no acceptance-test path (`paths.acceptanceTests`), so the qa-tester cannot keep black-box tests here (feedback R2). | reject (acceptance writing) | Named Go tests prove each AC and SR. The qa-tester runs exploratory testing against the built engine instead. |
| L-3 | tech lead | n/a | In this sandbox, `TestTypeScriptFailsClosed/no_typescript_package` fails at the base too: a `node_modules` above the sandbox temp directory supplies a TypeScript compiler. | environment | Gates ran with `TMPDIR` and `GIT_CEILING_DIRECTORIES` set to an ignored directory in the repository. The final gate must also pass outside the sandbox. |
| TM-4 (SEC-4) | security-engineer | medium | `//line` directives shift Go analyzer positions, which bypasses the function-length limit and would skew runtime lines. | fix | SR-13, package P2 |
| TM-5 (SEC-5) | security-engineer | medium | Node's `node:coverage` directives are not treated as suppressions. | fix | SR-16, package P2. Node is the coverage tool B2 is about. |
| TM-6 (SEC-6) | security-engineer | medium | Guard path mapping resolves symlinks in the root but not in the target. | fix | SR-4, package P1b (same function) |
| TM-7 (SEC-7) | security-engineer | low | Analyzer line numbers can disagree with git's on a lone CR, U+2028 or U+2029. | fix | SR-13, package P2 |
| TM-8 | security-engineer | n/a | Should a `tests-missing` skipped because runtime code is unchanged appear in the JSON report? | defer | Useful for audit, but it changes the report format. Follow-up. |
| P1-6 | software-engineer (P1) | n/a | A removed worktree's leftover `.git` file, pointing at a pruned gitdir, makes git say "not a git repository", so the hook passes with no guard. | defer | This predates the change and is outside the spec. It belongs with TM-3 in a follow-up on fail-open paths in repository discovery. |
| L-1 | tech lead (stop hook during this run) | medium | An untracked nested checkout (an agent worktree in a repository that does not ignore `.claude/worktrees/`) is listed by git as `dir/`, and the engine blocks with "git reported a path outside the repository", which names neither the cause nor the fix. | fix here (ignore), defer (message) | This repository's `.gitignore` lacked the entry `config apply` writes for users; added. The engine message should say "nested git checkout; ignore it or remove it": follow-up issue to file. |
