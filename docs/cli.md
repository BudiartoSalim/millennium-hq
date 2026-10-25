# Commands

```
millennium gate [--base REV] [--require-config] [--tier fast|full] [--format text|json]
millennium audit [--path PATH] [--tier fast|full] [--format text|json]
millennium scope --base REV --allow PATTERN [--allow PATTERN ...] [--format text|json]
millennium hook
millennium config validate
millennium config explain PATH
millennium version
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

## `millennium gate`

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
$ millennium gate
millennium: BLOCKED (findings: 2, changed files: 3)
  by rule: structure/function-length 1, structure/layer-import 1
  [structure/function-length] internal/application/usecase/sync.go:40: Sync.Execute has a length in lines of 72; the limit is 50
  [structure/layer-import] internal/domain/user.go:5: domain code must not import application code ("svc/internal/application/port"); domain may import no other layer
```

## `millennium audit`

Checks existing code instead of a change, holding every file to the full
rules, with no allowance for code that was already over a limit. Use it to plan
a refactor: the report's "by rule" line shows where the work is.

| Flag | Default | Meaning |
|---|---|---|
| `--path PATH` | whole repository | Audit only this directory or file. |
| `--tier` | `full` | As for `gate`. In an audit the full tier requires every executable line of tested layers to be covered. |
| `--format` | `text` | As for `gate`. |

An audit needs a config; without one there are no rules to audit against.

## `millennium scope`

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
| `scope/protected-path` | The branch changes `.millennium/` or the project's Claude settings. |

## `millennium hook`

Handles one Claude Code hook event from stdin. The plugin's hooks call it; see
[Hooks](hooks.md). It exits 0 with its answer on stdout, and 2 when it cannot
decide, which blocks.

## `millennium config validate`

Validates `.millennium/config.json` and reports every problem at once. Exit
status 2 when the config is invalid or missing.

## `millennium config explain PATH`

Shows which rules apply to one file: whether it is excluded, its language, the
component that owns it and that component's root, its layer under each
convention, and the limits. Use it when a finding is surprising.

```
$ millennium config explain services/game/api/internal/domain/server.go
services/game/api/internal/domain/server.go
  language:  go
  component: services/*/api (root "services/game/api")
  clean-architecture: layer domain
  limits:    function length 50 lines (test files exempt), nesting depth 3, cognitive complexity 15
```

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
