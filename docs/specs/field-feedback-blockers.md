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

(Filled in from the threat model.)

## Decision log

| id | from | severity | finding | disposition | reason |
|---|---|---|---|---|---|
