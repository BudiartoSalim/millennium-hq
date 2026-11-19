# Changelog

User-facing changes to millennium and its engine, `millennium-code-qc`.
Releases of the engine are tagged `code-qc/vX.Y.Z` (see docs/releasing.md).

## Unreleased

From the field feedback of a `/millennium:tech-lead` run on 0.3.1. The spec
and the full decision log (119 findings and what was done with each) are in
`docs/specs/field-feedback-blockers.md`.

### Fixed

- **A removed working directory no longer locks the session out (B1).** When
  a hook event's cwd no longer exists (typically a worktree removed while a
  session was in it), the engine uses its nearest existing parent instead of
  blocking every tool call, `cd` included. A cwd that exists but cannot be
  used still blocks. See docs/hooks.md, "A working directory that is gone".
- **`uncovered-change` counts only runtime lines (B2).** Comments, blank
  lines and code the TypeScript compiler erases (`interface`, `type`,
  `declare`, type-only imports and exports, overload signatures, annotations)
  are no longer reported as "never run", even when a source-mapped lcov
  report gives them zero hits. A comment-only edit of a file no test loads is
  not reported either.
- **`tests-missing` ignores edits that leave runtime code unchanged (B3).**
  A modified file whose runtime tokens match its base (only comments,
  whitespace or erased TypeScript types changed) needs no test change. The
  analyzers report runtime lines and a runtime digest for Go, TypeScript and
  Python; anything they cannot classify counts as changed.

### Changed

- **A generated-code marker no longer exempts files in a tested layer.**
  `// Code generated ... DO NOT EDIT.`, `@generated` or (Python) `DO NOT EDIT`
  in a domain or application file no longer takes it out of `tests-missing`,
  `uncovered-change` or the limits. Declare generated code there with
  `exclude` in the config. Outside tested layers the marker still exempts a
  file from the limits.
- **Protected paths are protected wherever they lie.** For every write in an
  opted-in checkout, `.millennium/`, `.claude/settings.json`,
  `.claude/settings.local.json`, `.pi/settings.json` and `.pi/extensions/` are
  matched as path segments anywhere in the path: in every worktree, in
  checkouts git no longer lists, without regard to case, through symlinks and
  Windows junctions, and with Windows trailing dots, spaces and `:` streams
  normalised. A tool path with a `..` part, a Windows rooted or drive-relative
  path, and a protected location or worktree root the engine cannot resolve
  (such as a directory it may not search) block the call with a message
  naming the fix. `scope`'s `protected-path` uses the same matching.
- **The shell guard denies what it cannot prove read-only.** A Bash command
  that names a protected path and can write is denied unless it is provably
  read-only: one line of whitelisted characters, every command from a short
  list (`ls`, `cat`, `head`, `tail`, `wc`, `grep`, `rg`, `find`, `du`, `stat`,
  read-only `git` subcommands, `cd` and others), each with an allowlist of
  read-only options. Some read-only commands that name a protected path next
  to a redirect are now denied (parentheses, `<`, `&>`, unlisted options such
  as `rg -v` or `git status -sb`); split such a command or drop the redirect.
  See docs/hooks.md for the exact rules and their limits.
- **Coverage must come from the file it describes.** In the full tier, a
  coverage report that gives a judged file a line it does not have is a
  `tool-failed` finding on that file, and code another file lends to a judged
  Go file through a `//line` directive is `foreign-coverage`. A report odd
  about files the gate does not judge (goyacc output, excluded files) blocks
  nothing.
- **Hook latency.** Runtime facts are computed only for the tests gate's
  candidates, and the worktree list is read only when a rule needs it
  (main-session Read and Write hooks on a Windows drive under WSL went from
  about 0.2 s to about 0.03 s; worktree-listing calls stay around 0.2 to 0.3 s).
- `MILLENNIUM_LOCAL_DIR` must be an absolute path.

### Added

- Rule `tests/line-directive`: a Go `//line` or `/*line` directive in a
  tested-layer file blocks (it moves the lines coverage reports).
- Rule `tests/foreign-coverage`: a `//line` directive in any Go file of a
  judged file's directory, excluded files included, that credits its code to
  the judged file blocks.
- Rule `hygiene/ignored-directive`: a `node:coverage` comment in a form Node
  does not read. Node's directives are now read from raw TypeScript lines,
  strings included, as Node does, and need a reason on the line above.
- Python and TypeScript hashbangs, Go toolchain directives, JSX pragmas,
  triple-slash directives and Python encoding declarations count as runtime.
- `.github/workflows/test-code-qc-windows.yml`: runs the engine's
  Windows-specific code (junctions, volume mount points, 8.3 names) on a
  Windows runner, without third-party actions.

### Known limits

Listed in docs/hooks.md under "What the hooks cannot do", among them: a
relative path that resolves into a protected location without naming it,
spliced names such as `.p${X}i`, programs started through a repository's
config, and a session whose own checkout is not opted in. Deferred follow-ups
are the rows marked `defer` in the decision log.
