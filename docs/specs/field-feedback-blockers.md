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
| CR-1 | correctness-reviewer | medium | A comment-only edit to a tested-layer file that no test loads still gets `uncovered-change: no test executed this file`. | fix | AC-4. Proof test from the reviewer. Package F2. |
| CR-2 / SEC-7 | correctness-reviewer, security-engineer | medium | The SR-3 test with a real repository and a removed `.claude/worktrees/x` cwd is missing. The `ErrNoDirectory` mapping in `main.go` is untested. | fix | SR-3. Package F1. |
| CR-3 | correctness-reviewer | low | The SR-12 differential test compares runtime lines with the same source map they come from. | fix | SR-12. Add expected runtime lines that do not depend on the source map. Package F2. |
| MR-1 | maintainability-reviewer | low | U+2028 and U+2029 are written as invisible literal runes in `decode.go`. | fix | Readability. Package F2. |
| MR-2 | maintainability-reviewer | low | When the walk is refused, the message repeats "the directory does not exist" and gives no next step. The cli sentinel is declared in `hook.go`, not `cli.go`. | fix | AGENTS.md: a block says why and what to do. Package F1. |
| MR-3 | maintainability-reviewer | low | The gitrepo `RepoPath` tests assert the guard's policy, not the mapping. | fix | The adapter owns the mapping. Package F1. |
| MR-4 | maintainability and security reviewers (not reviewed) | medium | Tool paths have `..` removed as text before symlinks are resolved. `/tmp/l/../.millennium/config.json`, with `l` pointing into the repository, maps as outside, while the OS opens the repository's config. | fix | Fail closed: a tool path with a `..` segment blocks. Package F1. |
| PERF-1 | performance-reviewer | medium | Every TypeScript analysis now runs `transpileModule`, a re-parse and a token walk, even for files no gate reads runtime facts for (16 KB file: 2.5 to 21.5 ms; fast tier on every Stop). | fix | Constraints (hook latency). Compute runtime facts only on request, for the tests gate's candidates. Package F2. |
| PERF-2 | performance-reviewer | low | Peak memory of the TypeScript helper on a 4 MiB file goes from 0.4 to 1.3 GB, and it fails with a 512 MB heap. | fix | Walk the tree without creating child nodes, and release the emit early. Package F2. |
| PERF-3 | performance-reviewer | low | The tests gate reads the base blob again with 3 git calls, and also reads it when the working facts are unknown. | fix (early return), defer (blob cache) | Returning early when facts are unknown costs nothing. A per-run blob cache is a follow-up. |
| SEC-1 | security-engineer | medium | New with B1: an agent that renames its own worktree is evaluated from the main checkout, where the worktree's `.millennium/` is not protected. | fix | SR-3 and SR-4: protect the protected paths of every worktree of the repository. Package F1. |
| SEC-2 | security-engineer | high | Predates this change: an agent in a worktree can write the main checkout's `.millennium/` by absolute path. | fix | Same fix as SEC-1. Package F1. |
| SEC-3 | security-engineer | high | Predates this change: on case-insensitive filesystems a case variant (`.MILLENNIUM/config.json`) passes the guard. | fix | Match protected paths without regard to case. Package F1. |
| SEC-4 | security-engineer | medium | Predates this change: a Go `//line` directive moves cover-profile positions off the changed lines, so `uncovered-change` never fires. | fix | Block `//line` directives in tested-layer Go code unless the file is excluded, and reject profile entries outside the file. Package F2. |
| SEC-5 | security-engineer | medium | Predates this change: the local-config protection compares paths as written, so a symlink in the repository reaches the local configs. | fix | Resolve the path like the guard does, and fail closed. Package F1. |
| SEC-6 | security-engineer | low | Python 3.12+: whitespace inside a debug f-string (`{x = }`) is printed but left out of the digest. | fix | AC-7. Hash the exact f-string source text. Package F2. |
| QT-1 | qa-tester | low | TypeScript inline `type` import and export specifiers (`import { type A, b }`) count as runtime, so a type-only edit gets `tests-missing`. | fix | AC-6. It fails safe, but it is the same friction as B3. Package F2. |
| QT-2 | qa-tester | low | A TypeScript hashbang change counts as non-runtime, although the compiler keeps the hashbang. | fix | SR-11 (err toward runtime). A hashbang is runtime in TypeScript and in Python. Package F2. |
| QT-3 | qa-tester | low | A `node:coverage enable` directive on the line above is accepted as the reason for the next suppression. | fix | SR-16. A directive is never a reason. Package F2. |
| QT-4 | qa-tester | low | When the walk is refused, the message is repeated and misleading (same as MR-2). | fix | Package F1. |
| QT-n1 | qa-tester | n/a | Fixing a stale trailing comment on an uncovered runtime line (`return n * 2 // old`) still gets `uncovered-change`. | reject | The spec's AC-5 counts a mixed line as runtime. A token-level rule would be more complex and easier to bypass. |
| F1-n1 | software-engineer (F1) | low | The qa-tester read rule (`cfg.Reaches`) matches case-sensitively, so on a case-insensitive filesystem a qa-tester could read implementation code under another spelling. | fix (round 2) | A read-isolation variant of SEC-3. Package G1. |
| R2-CR-1 / R2-SEC-3 | correctness-reviewer, security-engineer (round 2) | high | The Go cover-profile check applies to the whole module, so a `//line` directive in any file (goyacc output, an excluded file, an untested layer) makes every full-tier run a `tool-failed` that cannot be disabled. The advice it prints (`exclude`) cannot work. Contradicts SEC-4's "unless the file is excluded" and AC-12. Proof test from the reviewer. | fix (round 2) | Check positions only for files the gate judges (tested-layer candidates), in the tests gate. Drop other entries. Correct the message and the goyacc claim in the docs. Package G2. |
| R2-SEC-1 | security-engineer (round 2) | medium | A `//line real.go:5` directive in an excluded sibling file lends coverage to untested lines of `real.go` in a tested layer. Cover keeps only the directive's base name within the package directory. | fix (round 2) | Coverage of a tested-layer file may come only from that file. A `//line` directive in any Go file of a candidate's directory, excluded files included, that names a candidate blocks. Package G2. |
| R2-SEC-2 | security-engineer (round 2) | medium | One worktree root that cannot be resolved (parent replaced by a dangling symlink or a file) blocks every Read, Write, Edit, Grep and Glob, even in repositories that are not opted in. | fix (round 2) | No worktree mapping outside opted-in repositories. For an unresolvable other root, compare as git names it rather than block. Package G1. |
| R2-SEC-4 | security-engineer (round 2) | medium | On Windows, a rooted (`\repo\...`) or drive-relative (`C:x`) tool path is joined to the cwd, although the OS reads it otherwise. | fix (round 2) | SR-4: refuse any path that is not absolute but has a volume name or a leading separator. Package G1. |
| R2-SEC-5 | security-engineer (round 2) | low | A qa-tester reads implementation code through another worktree's absolute path. | fix (round 2) | The black-box rule applies to the paths in every worktree. Package G1. |
| R2-MR-1 | maintainability-reviewer (round 2) | medium | The coverage-bounds rule lives in the Go runner, with its own copy of git's line counting. | fix (round 2) | Together with R2-CR-1: a domain rule applied in the tests gate to every coverage source, using `domain.LineCount`. Package G2. |
| R2-MR-2 | maintainability-reviewer (round 2) | low | A `//line` directive is modelled as a banned construct, then special-cased out of `banned-construct`. | fix (round 2) | Report it as its own fact. Package G2. |
| R2-MR-3 | maintainability-reviewer (round 2) | low | The fallback "an analyzer without AnalyzeRuntime gives unknown runtime code" is written twice (gate and cache). | fix (round 2) | Keep it in one place. Package G2. |
| R2-MR-4 / R2-PERF-2 | maintainability, performance (round 2) | medium | Every file-tool call runs `git worktree list` (about 110 ms on a Windows drive under WSL), although only the write rules (and, after R2-SEC-5, the qa-tester read rule) use it. | fix (round 2) | List worktrees only for write tools and for the qa-tester role, and skip the listing when `<git-common-dir>/worktrees` does not exist. Package G1. |
| R2-PERF-1 | performance-reviewer (round 2) | medium | The hook resolves the same tool path's symlinks once per worktree root (about 10 ms each on a Windows drive). | fix (round 2) | Resolve each path and each root once, then compare in memory. Package G1. |
| R2-PERF-3 | performance-reviewer (round 2) | medium | The TypeScript token walk scans an empty gap, so it costs depth × token length: a crafted 3.9 MB file takes 14.7 s instead of 0.35 s. | fix (round 2) | Return early when `pos >= end`, which gives an identical digest. Package G2. |
| R2-PERF-4 | performance-reviewer (round 2) | low | A TypeScript file that does not parse is still transpiled for each runtime request. | fix (round 2) | The cache answers a runtime request with a cached plain parse error. Package G2. |
| QT2-5 | qa-tester (round 2) | medium | A worktree moved with plain `mv` drops out of `git worktree list`, so its `.millennium/` is writable from the main checkout or from the removed path. | fix (round 2), design change | Enumerating checkouts keeps leaking (QT2-5, QT2-9). Decision: for a write tool, a protected path (`.millennium/`, `.claude/settings.json`, `.claude/settings.local.json`, `.pi/settings.json`, `.pi/extensions/`) is matched as a path segment sequence anywhere in the path, as named and resolved, ignoring case, whatever repository it lies in. The worktree listing remains only for the role rules (the software-engineer's acceptance tests, the qa-tester's black box). Package G1. |
| QT2-6 | qa-tester (round 2) | low | A TypeScript hashbang (now a runtime line, QT-2) gets `uncovered-change` from Node's lcov (`DA:1,0`), which no test can clear. | fix (round 2) | Keep the hashbang in the digest but not in the runtime lines. Package G2. |
| QT2-7 | qa-tester (round 2) | low | Another worktree's acceptance tests are protected under the acting worktree's `paths.acceptanceTests`. | reject (document) | The role rules apply the acting session's config, the rules it is checked against. Documented in `docs/hooks.md`. Package G1. |
| QT2-8 | qa-tester (round 2) | low | For `node:coverage`, the suggested ` -- <reason>` form makes Node ignore the directive. | fix (round 2) | For `node:coverage` directives, recommend only the reason on the line above, and report a `node:coverage` directive carrying extra text. Package G2. |
| QT2-9 | qa-tester (round 2) | low | A worktree whose path contains a newline is not protected. | fix (round 2) | The QT2-5 design protects the paths without the listing. For the role rules, a listing that cannot be parsed unambiguously blocks the call. Package G1. |
| QT2-10 | qa-tester (round 2) | low | The docs say `DO NOT EDIT` marks a TypeScript file as generated. Only `@generated` and `Code generated ... DO NOT EDIT.` do. | fix (round 2) | Correct `docs/checks.md` and `docs/configuration.md`. Package G2. |
| R3-SEC-1 | security-engineer (round 3) | high | The guard allows everything when the session's own checkout is not opted in (a worktree at a commit from before opt-in, a submodule, a nested `git init`). From there a file tool writes the main checkout's `.millennium/config.json`. This predates the branch, but `docs/hooks.md` now promises protection "of any other checkout on the machine". | fix docs; defer design (user, 2026-11-18) | The user kept the plugin's promise: it guards only sessions in an opted-in checkout. Correct `docs/hooks.md` to say so (package H1). Follow-up: decide opt-in by the checkout each write target lies in. |
| R3-SEC-2 / QT3-1 | security-engineer, qa-tester (round 3) | high | A `node:coverage disable` or `ignore next` inside a string or template literal switches Node's coverage off (Node matches raw lines), with no finding. Untested code passes the full tier. Proven. | fix (round 3) | Run the hygiene check on the raw lines of TS/JS source, strings included, as Node does. Package H2. |
| R3-SEC-3 | security-engineer (round 3) | medium | A protected location reached through a symlinked parent (`ln -s conf .pi`, then Write `conf/extensions/x.ts`) is writable by its real path. Proven. | fix (round 3) | Resolve each protected location of the checkouts in question and deny targets inside the resolved locations. Package H1. |
| R3-SEC-4 / R3-CR-1 | security-engineer, correctness-reviewer (round 3) | medium | Foreign coverage is detected only for the built-in Go runner. A Go component with a test recipe (proven by test) and a JavaScript file whose inline source map names a judged `.ts` file (proven) both lend coverage to untested lines. | fix Go part (round 3), defer source maps | The Go part: run the directive scan from the tests gate for candidates' directories, whatever runner ran (package H2). Source maps are a new attack class: follow-up issue. |
| R3-SEC-5 | security-engineer (round 3) | medium | A `.go` name that `go build` ignores (`_link.go` symlinked to a directory, a file over 64 MiB) makes every full-tier run a `tool-failed` that cannot be disabled. Proven end to end. | fix (round 3) | Skip names `go build` ignores, and scan only the candidates' directories. Package H2. |
| R3-SEC-6 | security-engineer (round 3) | medium | On native Windows, a trailing dot or space, or `::$DATA`, on a protected file that does not exist yet passes the guard. Not run. | fix (round 3) | Normalise segments as Win32 does (trailing dots and spaces, `:` streams) before matching, on every OS. Package H1. |
| R3-SEC-7 | security-engineer (round 3) | low | An empty directory under `.git/worktrees` pads the record count, so a crafted newline worktree path hides from the role rules. Proven. | fix (round 3) | Count only records with a `gitdir` file. Package H1. |
| R3-PERF-1 | performance-reviewer (round 3) | low | The foreign-coverage scan reads every `.go` file of the module (1.1 s per full tier here), and runs when the suite failed. | fix (round 3) | With R3-SEC-4: scan only candidates' directories, and skip after a failed suite. Package H2. |
| R3-MR-1 | maintainability-reviewer (round 3) | medium | `PathNeedOf` restates the guard's dispatch, so a new role rule could get no paths and allow silently. | fix (round 3) | Add a domain test over every tool and role that ties `PathNeedOf` to `Guard`. Package H1. |
| R3-MR-2 | maintainability-reviewer (round 3) | medium | Path-location policy (the opt-in check, unresolvable roots) lives in the CLI layer and depends on the guard use case's behaviour. The config is loaded twice for role calls. | defer (user, 2026-11-18) | A refactor with no behaviour change. Follow-up issue. |
| QT3-2 | qa-tester (round 3) | medium | `scope`'s `protected-path` is case-sensitive and matches only at the root (`.MILLENNIUM/config.json`, `sub/.millennium/` pass). | fix (round 3) | Reuse the guard's segment and case matching in `scope`. Package H1. |
| QT3-3 | qa-tester (round 3) | low | Role rules miss a worktree moved with plain `mv`. git lists the old path as prunable. | fix (round 3) | Document the limit in `docs/hooks.md`. Package H1. |
| QT3-4 | qa-tester (round 3) | low | The out-of-range coverage message tells TypeScript users to remove a Go `//line` directive. | fix (round 3) | Name only the causes that fit the report's language. Package H2. |
| R4-SEC-1 / R4-CR-1 | security-engineer, correctness-reviewer (check of round 3) | medium | Another checkout's symlinked protected location (`wt/.pi -> conf`) is resolved only for the software engineer's writes. The main session and qa-tester can write `wt/conf/extensions/evil.ts`. Proven. | fix (round 4, the user chose to fix all five, 2026-11-18) | Fix: resolve the locations of every listed worktree for every write. Or accept the limit, which `docs/hooks.md` already states. |
| R4-SEC-2 / R4-CR-3 | security-engineer, correctness-reviewer (check of round 3) | medium | Regression from round 3. A dangling `.pi`/`.claude` symlink, or a regular file named `.claude`, blocks every write in that checkout and every software-engineer write in all worktrees. A committed relative symlink that dangles under `.claude/worktrees/` triggers it. Proven. | fix (round 4, the user chose to fix all five, 2026-11-18) | Fix: resolve a dangling location by reading the link instead of blocking, and treat ENOTDIR as reaching nothing. |
| R4-SEC-3 | security-engineer (check of round 3) | medium | `scope` does not flag a branch that commits `.pi` or `.claude` as a symlink (and `conf/extensions/evil.ts`). Proven. | fix (round 4, the user chose to fix all five, 2026-11-18) | Fix: flag a changed path that is exactly a leading part of a protected path. |
| R4-SEC-4 | security-engineer (check of round 3) | medium | On native Windows, `EvalSymlinks` does not follow directory junctions (Go 1.23+, `winsymlink=1`), and `mklink /J` needs no privilege. The symlink protections do not hold there. Not run. | fix (round 4, the user chose to fix all five, 2026-11-18) | Needs Windows-specific code and a Windows test. Belongs with the filesystem-alias follow-up. |
| R4-CR-2 | correctness-reviewer (check of round 3) | low | A worktree record whose `gitdir` exists but cannot be read is counted, though git skips it, so a forged record fits again. Proven. | fix (round 4, the user chose to fix all five, 2026-11-18) | Fix: count a record only if its `gitdir` can be read. |
| R5-SEC-1 | security-engineer (check of round 4) | medium | `chmod 000` on another worktree makes its protected locations unknown, and unknown locations of other checkouts are skipped, so a link leading out of that worktree is unguarded again. Proven. | fix (round 5, the user chose to fix all seven, 2026-11-18) | Fail closed on any listed checkout's unknown location (more blocking), or accept. |
| R5-SEC-2 | security-engineer (check of round 4) | medium | Windows regression from round 4. A repository under a folder-mounted volume has every write blocked: Go 1.23+ `os.Readlink` returns `\\?\Volume{GUID}\`, which the walker refuses. Not run. | fix (round 5, the user chose to fix all seven, 2026-11-18) | Map volume targets to their mount path, or do not follow volume mount points. Needs Windows to verify. |
| R5-SEC-3 / R5-CR-1 | security-engineer, correctness-reviewer (check of round 4) | medium | No test runs the Windows junction code (`entryOf`, `normalize`, `os.Readlink` on a real junction). CI runs only on Ubuntu. | fix (round 5, the user chose to fix all seven, 2026-11-18) | Needs a Windows runner (a pinned action) or a recorded manual Windows run. |
| R5-CR-2 | correctness-reviewer (check of round 4) | medium | No test covers the rule that the acting checkout's unknown location blocks a write outside that checkout (`i == 0`). | fix (round 5, the user chose to fix all seven, 2026-11-18) | One test row. |
| R5-CR-3 | correctness-reviewer (check of round 4) | low | The new shell-guard check (not asked for by any row) denies read-only commands that name `.claude` or `.pi` next to a redirect: `ls -la .claude 2>/dev/null`, `du -sh ~/.claude 2>&1`. | fix (round 5, the user chose to fix all seven, 2026-11-18) | Match only the operands of ln, mv, cp, rm, install and git mv, or drop the check. |
| R5-SEC-4 | security-engineer (check of round 4) | low | A link loop as a protected location blocks every write in its checkout, and from the main checkout every agent worktree. Proven. | fix (round 5, the user chose to fix all seven, 2026-11-18) | Treat a loop like a dangling link for a location (it reaches nothing). |
| R5-SEC-5 | security-engineer (check of round 4) | low | Link targets under `/proc/self`, `/dev/fd` and similar resolve in the engine's process. Proven: `B/.pi -> /proc/self/cwd/conf` lets a write of `B/conf/extensions/evil.ts` through. | fix (round 5, the user chose to fix all seven, 2026-11-18) | Treat such targets as unknown. |
| R6-SEC-1 | software-engineer (K1, proven by probe) | medium | `chmod 000 .claude/worktrees` makes another worktree's root unresolvable (EACCES). The rule kept since R2-SEC-2 compares an unresolvable root as named, so its protected locations are not looked at. With `other/.pi -> <main>/conf`, a Write of `<main>/conf/extensions/evil.ts` is allowed. | open: needs the user's decision | Fix: compare as named only roots that reach nothing (missing, dangling, below a file), and block on other errors such as EACCES. That changes the R2-SEC-2 rule. |
| R6-SEC-W | security-engineer, correctness-reviewer (check of round 5) | medium | The Windows test workflow was not committed: the integration filter for sandbox placeholder files also matched `.github/`. | fixed | Committed in e0a2d28, the same file as reviewed in K1's worktree. |
| R6-CR-1 | correctness-reviewer (check of round 5) | high | Regression from round 5. A full protected path with quotes inside it (`cp evil.json '.claude'/settings.json`, `echo '{}' > '.claude'/settings.json`, `tee ".pi"/extensions/x.ts`) is no longer denied by the shell guard. Proven: passes on 7f65e61, fails on 66e877d. | open: for the user | Match the full protected paths on the scanner's words, redirection targets included. |
| R6-CR-2 / R6-SEC-5 | correctness-reviewer, security-engineer (check of round 5) | medium | Regression from round 5. The narrowed lead-in check lets through replacing forms the round-4 check denied: `timeout 5 ln -s conf .pi`, `nice -n 5 mv conf .pi`, `find . -exec ln -s conf .pi \;`, `eval 'ln -s conf .pi'`, `git rm -r .claude`, `git checkout main -- .claude`, `git restore --source=HEAD~3 .pi`. Proven. | open: for the user | Treat a simple command as replacing when a replacing name (or git rm, checkout, restore, eval, timeout, find -exec) appears among its words. |
| R6-SEC-4 | security-engineer (check of round 5) | low | The Windows workflow passes when the volume-GUID test skips, so the kernel32 call may never run. | open: for the user | Also require `--- PASS: TestWalkFollowsAJunctionToAVolumeByItsGUID`. |
| R6-SEC-2 / R6-CR-4 | security-engineer, correctness-reviewer (check of round 5) | low | Windows: a volume mounted at two paths gives a protected location and a tool path two spellings, so a junction to `\\?\Volume{G}\…` can be bypassed through the other mount. Not run. | open: for the user | One spelling per volume. Needs Windows to verify. |
| R6-SEC-3 | security-engineer (check of round 5) | low | Windows: a junction to a folder-mounted volume costs the walk two links against the OS's one, so a long chain "reaches nothing" while Windows opens it. Not run. | open: for the user | Start below the mount folder as resolved. Needs Windows to verify. |
| L-1 | tech lead (stop hook during this run) | medium | An untracked nested checkout (an agent worktree in a repository that does not ignore `.claude/worktrees/`) is listed by git as `dir/`, and the engine blocks with "git reported a path outside the repository", which names neither the cause nor the fix. | fix here (ignore), defer (message) | This repository's `.gitignore` lacked the entry `config apply` writes for users; added. The engine message should say "nested git checkout; ignore it or remove it": follow-up issue to file. |
