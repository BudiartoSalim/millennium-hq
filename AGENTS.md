# millennium-hq

Source of millennium, a quality- and security-first multi-agent plugin for AI
coding harnesses, Claude Code first. `docs/design.md` is the design: roles,
workflow, enforcement model, gate engine, conventions and build order. Read it
before any structural change, and update it in the same change when a decision
moves.

## Layout

```
engine/                       gate engine, Go standard library only
  main.go                     composition root ONLY
  internal/domain/            config, components, patterns, packs and layer
                              rules, limits, facts, verdict
  internal/application/
    port/                     interfaces the core needs
    gate/                     the checks (toolchain, structure, shell, code); each reports findings
    usecase/                  load config, run gates, audit, explain
  internal/infrastructure/    analysiscache, configfile, gitrepo, goanalyzer, helperanalyzer
                              (TypeScript and Python helpers, embedded), packs
                              (embedded convention packs), shellanalyzer,
                              shelltools (bash -n, shellcheck), toolpath
  internal/interface/cli/     argument parsing, rendering, exit codes
schema/millennium.schema.json JSON Schema of a project's .millennium/config.json
.millennium/config.json       this repository's own config: the engine audits itself
scripts/verify.sh             the full local check
docs/                         user documentation; docs/design.md is the design
```

Plugin components (`agents/`, `skills/`, `hooks/`, `conventions/`, `ci/`,
`templates/`, `helpers/`) arrive as the build order in `docs/design.md` reaches
them.

## Non-negotiables

**The plugin practises what it enforces.** Code here meets the standard
millennium imposes on its users: Clean Architecture, required unit tests, gated limits, no
unexplained suppressions.

**Zero third-party dependencies.** The engine uses the Go standard library
only; helpers use only their runtime's standard library; CI actions are
pinned to full commit SHAs. Adding anything needs the user's explicit
approval, recorded in `docs/design.md`.

**Fail closed.** A check that cannot run blocks. A missing tool, unreadable or
unexpected input, unexpected tool output, a gate that errors or panics: each
blocks with a message saying why and what to do. Never add a "skip when
missing" path; the predecessor setup in server-scripts had several, and they
are the bugs this project exists to fix.

**Security first.** The engine runs from hooks on repositories that agents
modify, so every repository file, git output and config value is untrusted.
Run executables without a shell and with explicit arguments; refuse values
that start with `-` where git or a tool would read them as options; pass
`-c core.fsmonitor=false` on every git call (a repository's config can name a
program git runs); bound every read; validate paths at the boundary.

**Readable code, no stacked patches.** A fix that adds one more branch to an
already branchy function is wrong; restructure instead. A function fits on one
screen (about 50 lines), nests at most three levels, and stays under cognitive
complexity 15. No boolean parameters that fork behaviour (use a typed
enum), no dead code, no TODO without a linked follow-up. Comments explain why,
not what; match the existing density. Errors carry context and are never
swallowed. Duplication is acceptable where an abstraction would have to know
per-caller special cases.

## Engine conventions

- Dependencies point inward: `domain` imports nothing of ours, `application`
  imports `domain` only, adapters and `interface` import inward, and only
  `main.go` sees everything.
- Enumerations are typed strings with an allowlist function (`Languages()`,
  `Conventions()`), and validation lives in `domain`.
- Sentinel errors live in the use-case package, or in `port` when adapters must
  return them; wrap with `%w`; map them to output only in `interface/cli`.
- **Exit status:** 0 only for a pass or a repository that is not opted in;
  every other outcome exits 2. Claude Code hooks block only on 2 and treat 1 as
  a non-blocking error, so the engine never exits 1.
- Text reports go to stderr (where hooks read them), JSON to stdout.
- Config changes touch four places in one change: the domain, the DTO in
  `infrastructure/configfile`, `schema/millennium.schema.json`, and
  `docs/configuration.md`. `schema_sync_test.go` fails when the first three
  drift.
- **Every user-facing change updates `docs/` in the same change**: a new
  command or flag in `docs/cli.md`, a new check or rule in `docs/checks.md`, a
  convention in `docs/conventions/`. A user must be able to learn every
  feature from the docs alone.
- Doc comments on every package and exported identifier.

## Testing

Every change comes with unit tests in the same change; writing them first is
welcome, not required. Tests are
table-driven, beside the code, standard `testing` only, with fakes as plain
structs implementing the port. Test the failure paths, not just the happy path.
A security property gets a test that fails when the protection is removed;
check that it does. The `gitrepo` tests run real git in `t.TempDir()` with the
user's global and system config isolated.

## Verify before you say "done"

```
eval "$(bash scripts/setup-fixtures.sh)"   # once per shell: the TypeScript test fixture
bash scripts/verify.sh
```

The TypeScript analyzer's tests need a TypeScript compiler. It is a test-only
fixture pinned by `scripts/fixtures/typescript/package-lock.json`, installed
outside the repository with install scripts disabled; `verify.sh` refuses to
run without it rather than report skipped tests as passed.

It runs gofmt, go vet for linux, darwin and windows, the full test suite, and
the engine auditing its own source against `.millennium/config.json`: a rule
the engine would enforce on a user's code fails this repository too. Fix the
code, never the config, when the self-audit blocks.
Inside Claude Code's sandbox the home directory is read-only, so first:

```
export GOCACHE="$TMPDIR/go-cache" GOMODCACHE="$TMPDIR/go-mod" GOPATH="$TMPDIR/go" GOTOOLCHAIN=local
```

## Platform notes

- Claude Code's sandbox mounts `/dev/null` character devices over protected
  paths (`.bashrc`, `.gitconfig`, `.mcp.json`, `.idea`, `.claude/settings.json`,
  ...). Inside the sandbox they appear as untracked files; they are not on disk.
  Never delete or stage them: stage explicit paths, never `git add -A`. The
  engine skips device files for the same reason.
- `CLAUDE.md` only imports this file: a `CLAUDE.md` in a directory above the
  repository stops Claude Code from loading `AGENTS.md` on its own.

## Git

Branches `feat/<topic>`, `fix/<topic>`, `chore/<topic>`. Commits
`type: short summary` with `feat`, `fix`, `docs`, `chore`, `test` or
`refactor`. Commit only when asked. Text files are LF (`.gitattributes`).
