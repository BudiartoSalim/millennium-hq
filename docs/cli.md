# Commands

```
millennium-code-qc gate [--base REV] [--require-config] [--tier fast|full] [--format text|json]
millennium-code-qc audit [--path PATH] [--proposal FILE] [--tier fast|full] [--format text|json]
millennium-code-qc discover [--format text|json]
millennium-code-qc scope --base REV --allow PATTERN [--allow PATTERN ...] [--format text|json]
millennium-code-qc hook
millennium-code-qc config validate
millennium-code-qc config explain PATH
millennium-code-qc config apply [--local] [--confirm DIGEST] [PROPOSAL]
millennium-code-qc config export [--local]
millennium-code-qc convention list [--local]
millennium-code-qc convention show [--local] NAME
millennium-code-qc convention new [--local] [--from PACK] NAME
millennium-code-qc version
```

Run them anywhere inside the repository. Paths are relative to the current
directory.

## Exit status

| Status | Meaning |
|---|---|
| `0` | The check passed, or the repository is not opted in (no config). |
| `2` | Anything else: findings, an invalid config, a missing tool, an internal error, a usage error. |

The engine never exits 1. Claude Code hooks block only on exit status 2 and
treat 1 as a non-blocking error, so a check that could not run must not look
like one that only warned. Any caller that treats non-zero as failure (a git
hook, a CI step) works unchanged.

## `millennium-code-qc gate`

Checks a change: everything that differs from a base revision, staged,
unstaged and untracked files alike. This is what the plugin's hooks and the
tech lead run.

| Flag | Default | Meaning |
|---|---|---|
| `--base REV` | `HEAD` | Compare against REV instead, for example `--base main` to check a whole branch. |
| `--require-config` | off | Block when the repository has no config. Use it where a missing config must never look like a pass, such as the tech lead's final run or your own CI. |
| `--tier` | `full` | `fast` skips the test runs (what hooks use on every agent turn); `full` also runs the tests and measures coverage of the changed lines. |
| `--format` | `text` | `text` writes a report to stderr; `json` writes a machine-readable report to stdout. |

Existing code is judged leniently, new code strictly:

- new files and new functions must meet every limit;
- an existing function that already exceeded a limit may stay over it, but
  must not get worse;
- only imports the change adds are held to the layer rules.

Example:

```
$ millennium-code-qc gate
millennium-code-qc: BLOCKED (findings: 2, changed files: 3)
  by rule: structure/function-length 1, structure/layer-import 1
  [structure/function-length] internal/application/usecase/sync.go:40: Sync.Execute has a length in lines of 72; the limit is 50
  [structure/layer-import] internal/domain/user.go:5: domain code must not import application code ("svc/internal/application/port"); domain may import no other layer
```

## `millennium-code-qc audit`

Checks existing code instead of a change, holding every file to the full
rules, with no allowance for code that was already over a limit. Use it to plan
a refactor: the report's "by rule" line shows where the work is.

| Flag | Default | Meaning |
|---|---|---|
| `--path PATH` | whole repository | Audit only this directory or file. |
| `--proposal FILE` | | Audit against the config in a proposal instead of the repository's: a dry run before `config apply`. The proposal is validated as `config apply` would, and nothing is written. Works in a repository without a config. |
| `--tier` | `full` | As for `gate`. In an audit the full tier requires every executable line of tested layers to be covered. |
| `--format` | `text` | As for `gate`. |

An audit needs a config, or a proposal; without either there are no rules to
audit against.

## `millennium-code-qc discover`

Inspects a repository that has no config yet, for `/millennium:adopt-project` and
`/millennium:init-project`, and drafts a proposal:

```
$ millennium-code-qc discover
millennium-code-qc discover: 2 modules
  services/game/api (go.mod): go 42 files, 12 test files
    clean-architecture: 30 of 30 go files in its layers
  services/game/web (package.json): typescript 18 files, its dependencies suggest react-feature-based
    react-feature-based: 4 of 15 typescript files in its layers
  outside any module: shell 3 files
```

followed by the draft proposal. A module root is a directory with `go.mod`,
`package.json`, `pyproject.toml`, `setup.py` or `requirements.txt`. A
convention is drafted where its default layout places at least half of a
module's non-test files in a layer, the one placing the most where several
do. A pack whose `suggest.dependencies` names a package in the module's
`package.json` (react-feature-based names `react`) is preferred: such a
module is matched only against the packs it hints at, and other modules only
against packs without hints. The project's own packs in
`.millennium/conventions/` take part. Modules that fit no pack get no
convention and a reason. In JSON, each module's `hints` lists the packs its
dependencies suggest. Each top-level directory with code outside
any module, such as `scripts`, gets a component of its own.

It reads file names and `package.json` only, runs nothing from the
repository, and writes nothing. `--format json` prints the modules, the
counts and the draft (`proposal`) as JSON.

## `millennium-code-qc scope`

Checks that a branch stays inside its work package. The tech lead runs it in
an engineer's worktree before merging, so it holds however the files were
written.

| Flag | Meaning |
|---|---|
| `--base REV` | The revision the branch started from, such as `main`. |
| `--allow PATTERN` | A path the package may change; repeat it. `**` matches any depth: `services/game/api/internal/domain/**`. |

| Rule | Fires when |
|---|---|
| `scope/out-of-scope` | A changed file matches no `--allow` pattern. |
| `scope/acceptance-test-changed` | The branch changes an acceptance test. |
| `scope/protected-path` | The branch changes `.millennium/`, the project's Claude settings, or pi's project settings or extensions (`.pi/settings.json`, `.pi/extensions/`). |

## `millennium-code-qc hook`

Handles one hook event from stdin, in Claude Code's hook event format. The
plugin's hooks call it, and millennium's pi extension translates pi's events
into the same format; see [Hooks](hooks.md) and [pi](pi.md). It exits 0 with
its answer on stdout, and 2 when it cannot decide, which blocks. The engine
command named in the session briefing is the launcher under
`MILLENNIUM_PLUGIN_ROOT` (set by the pi extension) or `CLAUDE_PLUGIN_ROOT`
(set by Claude Code).

## `millennium-code-qc config validate`

Validates `.millennium/config.json` and reports every problem at once. Exit
status 2 when the config is invalid or missing.

## `millennium-code-qc config apply`

Applies a proposal (default `millennium.proposal.json` at the repository
root), a new config, convention packs, dependency approvals, or several of
them, after showing what it writes and what it loosens; see
[Configuration](configuration.md#changing-the-config-proposals-and-config-apply),
[Proposing a convention pack](configuration.md#proposing-a-convention-pack)
and [Approving a dependency](configuration.md#approving-a-dependency). Pack
folders named in the proposal are read relative to the proposal's own
folder.

| Flag | Meaning |
|---|---|
| `--local` | Apply as this machine's local config, outside the repository. |
| `--confirm DIGEST` | Confirm without a terminal, with the digest a previous run printed. A digest that does not match the proposal is refused. |

## `millennium-code-qc config export`

Prints the config (`--local`: the local config) as a proposal on stdout.

## `millennium-code-qc config explain PATH`

Shows which rules apply to one file: whether it is excluded, its language, the
component that owns it and that component's root, its layer under each
convention, and the limits. Use it when a finding is surprising.

```
$ millennium-code-qc config explain services/game/api/internal/domain/server.go
services/game/api/internal/domain/server.go
  language:  go
  component: services/*/api (root "services/game/api")
  clean-architecture: layer domain
  limits:    function length 50 lines (test files exempt), nesting depth 3, cognitive complexity 15
```

## `millennium-code-qc convention list`

Lists every convention pack a config can name: its name, where it comes from
(`builtin`, `project` for `.millennium/conventions`, `local` beside a local
config) and the languages it governs. `--local` lists the packs beside this
machine's local config instead of the repository's.

```
$ millennium-code-qc convention list
clean-architecture       builtin  go, typescript, python
hexagonal                project  go
react-feature-based      builtin  typescript
```

## `millennium-code-qc convention show NAME`

Prints one pack: its languages, function limits, discovery hints, each layer
with what it may import, whether it may import third-party code, needs tests
or is isolated, the default paths per language, and its guide. Agents use it
to read a guide that did not fit in their briefing.

## `millennium-code-qc convention new NAME`

Writes a new pack to edit, copied from an existing one, to
`millennium.proposal/conventions/NAME/` (git-ignored), and prints a proposal
that adds it; see [Writing a convention](conventions/writing-a-convention.md).
It refuses a name a pack already has, and an existing draft. Flags may come
before or after NAME.

| Flag | Default | Meaning |
|---|---|---|
| `--from PACK` | `clean-architecture` | The pack to copy. |
| `--local` | off | Look up names among the packs beside the local config. |

Agents may run it: it writes only the draft. Only `config apply` adds the
pack to the rules.

## JSON reports

With `--format json`, `gate` and `audit` write one JSON object to stdout:

```json
{
  "status": "blocked",
  "files": 3,
  "gates": ["toolchain", "structure"],
  "findings": [
    {
      "gate": "structure",
      "rule": "layer-import",
      "message": "domain code must not import application code (...)",
      "file": "internal/domain/user.go",
      "line": 5
    }
  ]
}
```

`status` is `pass`, `blocked`, `not-enabled` or `error`; an `error` report
carries an `error` message instead of findings. `disabled` lists the checks
the config switched off, each with its `reason` and the number of findings it
`suppressed`. `file` and `line` are left out
when a finding is not about one place.
