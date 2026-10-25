# Configuration

A repository opts in to millennium with `.millennium/config.json` at its root.
Without that file the engine checks nothing. Once it exists, every problem
blocks: a typo in the config is an error, never a silently ignored setting.

The JSON Schema in [`schema/millennium.schema.json`](../schema/millennium.schema.json)
gives editors autocompletion and validation. Point to it with `$schema`:

```json
{ "$schema": "https://<where you host it>/millennium.schema.json", "version": 1 }
```

`millennium config validate` checks the file against the engine itself, which
also enforces rules the schema cannot express.

## Two forms

### Single project

For a repository that is one project, list its languages and conventions at
the top level:

```json
{
  "version": 1,
  "languages": ["go"],
  "conventions": ["clean-architecture"]
}
```

The whole repository is one component rooted at the repository root.

### Monorepos: components

For a repository with several projects, list **components** instead. Each maps
paths to languages and conventions:

```json
{
  "version": 1,
  "components": [
    { "paths": ["services/*/api"], "languages": ["go"],
      "conventions": ["clean-architecture"] },
    { "paths": ["services/identity/api"], "languages": ["typescript"],
      "conventions": ["clean-architecture"] },
    { "paths": ["services/kokoro-tts/app"], "languages": ["python"],
      "conventions": ["clean-architecture"],
      "layers": { "domain": ["*/domain.py"], "application": ["*/service.py"] } },
    { "paths": ["services/websearch/api"], "languages": ["go"], "conventions": [],
      "reason": "older flat package, not the template" }
  ],
  "exclude": ["**/node_modules/**", "**/dist/**"]
}
```

How a file finds its component:

- Each path is a **component root**. `*` and `?` match within one path
  segment, so `services/*/api` matches `services/game-control/api` and every
  file under it.
- **The most specific match wins**: the pattern with more segments, then the
  one with more literal (wildcard-free) segments. Above,
  `services/identity/api` beats `services/*/api` for files in that service, so
  it is checked as TypeScript.
- **Two equally specific matches are an error**, reported for the file, never
  resolved by guessing. Make one pattern more specific.
- **A file in no component** gets only the checks that apply to any file; no
  convention or code limit applies to it.
- A new service that matches an existing pattern is covered automatically.

The two forms cannot be mixed: with `components`, `languages` and
`conventions` belong inside each component.

Run `millennium config explain <file>` to see which component, layer and limits
apply to any file.

## Settings

| Setting | Required | Default | Meaning |
|---|---|---|---|
| `version` | yes | | Config format version; must be `1`. |
| `languages` | single-project form | | Languages to check: `go`, `typescript`, `python`, `shell`. |
| `conventions` | no | `[]` | Convention packs: `clean-architecture`, `react-feature-based`. |
| `components` | monorepo form | | See above. |
| `exclude` | no | `[]` | Paths no check reads. `**` matches any number of segments; a directory excludes everything inside it. |
| `test` | no | | Single-project form: the test recipe (below). |
| `disable` | no | `[]` | Rules switched off, each with a reason (below). |
| `testing` | no | `tests-required` | Changed production code needs tests. |
| `dependencyPolicy` | no | `strict` | Every new dependency needs a vetting record. |
| `python.typeCheck` | no | `off` | `mypy` turns on Python type checking; a missing mypy then blocks. |
| `shell.lint` | no | `shellcheck` | `shellcheck` lints shell scripts and is then required; `off` for a project that cannot install it. |
| `paths.acceptanceTests` | no | `tests/acceptance` | Where qa-tester's black-box tests live; the engineer agent cannot change them. |
| `run.start`, `run.healthUrl` | no | | How qa-tester starts the software. `healthUrl` must be on `localhost` or a loopback address, so a config can never point tests at a shared deployment. |

Component fields:

| Field | Required | Meaning |
|---|---|---|
| `paths` | yes | Component roots. `"."` is the repository root. |
| `languages` | yes | Languages of this component. |
| `conventions` | no | Convention packs for this component. |
| `layers` | no | Overrides a convention's layer paths, relative to the component root, for layouts that differ from the pack's default. |
| `reason` | no | Why the component is an exception; shown by `config explain`. |
| `test` | no | The component's test recipe (below). |

### Disabling a check

A project can switch off one rule of one gate when it does not fit, but only
with a reason, and every report lists it:

```json
"disable": [
  { "check": "structure/function-length", "reason": "upstream style keeps 80-line handlers" }
]
```

```
millennium: passed (gates: 6, changed files: 4)
  disabled: structure/function-length (hid 3 findings): upstream style keeps 80-line handlers
```

- `check` is `gate/rule`, as findings print it; a whole gate cannot be
  disabled. A name that is not a rule of any gate is a config error, so a typo
  never leaves a rule running while you believe it off.
- `reason` needs at least ten letters.
- Rules that report a check failing to run cannot be disabled, because that
  would let unchecked code pass: `toolchain/tool-missing`, and every
  `analysis-failed`, `analysis-unavailable`, `convention-unavailable`,
  `component-ambiguous`, `syntax-error`, `scan-failed`, `tool-failed`,
  `tsconfig-missing` and `tests-unconfigured` rule.
- For one line rather than a whole project, use a
  [waiver](checks.md#waivers) instead.

### Test recipes

The full tier runs each component's tests. Go and Python have built-in runners;
TypeScript needs a **recipe**, and any component can use one to replace the
built-in runner:

```json
"test": {
  "command": ["npm", "test", "--", "--experimental-test-coverage",
              "--test-reporter=lcov", "--test-reporter-destination=coverage/lcov.info",
              "--test-reporter=spec", "--test-reporter-destination=stdout"],
  "lcov": "coverage/lcov.info"
}
```

- `command` is an argument list run from the component root without a shell,
  with a 15-minute limit; a non-zero exit is a test failure.
- `lcov` is where the command writes an lcov coverage report, relative to the
  component root. The engine deletes it before the run, so a stale report
  cannot pass for a new one. It must cover the TypeScript sources (enable
  source maps when tests run on compiled JavaScript).

Not every setting is enforced yet: `dependencyPolicy`,
`paths.acceptanceTests` and `run` are validated now and enforced by checks
still being built. [Checks](checks.md) lists what runs today.

## How strictly the file is read

The engine rejects, with a message naming each problem:

- unknown fields, at any level (`"lnaguages"` is an error, not ignored);
- a key that appears twice, even in different case: JSON parsers keep the last
  value, so a duplicate could hide the effective setting from a reviewer;
- anything after the closing brace;
- a config that is a symlink, a directory, or larger than 64 KiB;
- paths with `.` or `..` segments, absolute paths, or backslashes.

## Changing the config: proposals and `config apply`

No agent can change the rules it is checked against. Agents write a
**proposal**, `millennium.proposal.json` at the repository root:

```json
{
  "version": 1,
  "config": { "version": 1, "languages": ["go"], "conventions": ["clean-architecture"] }
}
```

It may instead (or also) carry dependency approvals, see
[Approving a dependency](#approving-a-dependency). You apply it:

```
millennium config apply
```

It validates the proposal as strictly as a config, shows every file it will
write, lists every way it **loosens** the rules compared with the current
config (a language, component or convention dropped, a path excluded, a check
disabled, mypy or shellcheck switched off, component or acceptance-test paths
moved), and asks you to type `yes`. Without a terminal, for example from
Claude Code's `!` prefix, it prints a digest instead; re-run with
`--confirm <digest>` after reading the summary. Piped input is refused.

For a config in the repository, it writes:

- `.millennium/config.json`;
- `.claude/settings.json`, merged with what is there: the plugin enabled for
  the project, the sandbox on with unsandboxed commands off, writes to
  `.millennium` denied for every command, and `Edit` on it denied for every
  agent;
- a managed section in `AGENTS.md` describing the gates to every agent, plus an
  `@AGENTS.md` import in `CLAUDE.md` when the repository has one;
- `millennium.proposal.json` in `.gitignore`.

The applied default proposal is removed. Inside an agent's sandbox the command
cannot write `.millennium`, and the hooks deny agents from running it at all.

## Approving a dependency

Every package version a lockfile adds needs an approval record (the `deps`
gate's `unapproved-dependency`, see [Checks](checks.md#deps)). A record is
one file per package under `.millennium/deps/<ecosystem>/`, named
`<name>@<version>.json` with any `/` in the name written as `__`:

```
.millennium/deps/npm/express@4.21.2.json
.millennium/deps/npm/@types__node@22.10.1.json
.millennium/deps/go/github.com__redis__go-redis__v9@v9.7.0.json
.millennium/deps/pypi/numpy@2.1.0.json
```

Records are protected like the config: agents draft them into a proposal and
you apply it.

```json
{
  "version": 1,
  "approvals": [
    {
      "ecosystem": "npm",
      "name": "express",
      "version": "4.21.2",
      "decision": "approved",
      "reason": "HTTP routing for the identity API; no stdlib equivalent",
      "checked": "2026-10-20",
      "transitive": ["body-parser@1.20.3", "ms@2.0.0"]
    }
  ]
}
```

| Field | Meaning |
|---|---|
| `ecosystem` | `npm`, `go` or `pypi`. |
| `name`, `version` | The exact package version. A new version needs a new record. |
| `decision` | Always `approved`; a rejected package is simply not recorded. |
| `reason` | Why it is needed and what vetting found, at least 10 letters. |
| `checked` | The date it was vetted, `YYYY-MM-DD`. |
| `transitive` | Optional: the `name@version` of packages it brings in, approved with it. |
| `securityPatch` | Optional: the advisory this release fixes. It lets a release younger than the 7-day cooldown through. |

`config apply` lists each record it writes, with its reason. With `--local`
the records go to `deps/` beside the local config instead. A record whose
file name does not match what it approves is refused (`invalid-approval`).

## Local mode: a config outside the repository

For a repository you do not own, such as an open-source project you contribute
to, keep the config on your machine instead:

```
millennium config apply --local my-proposal.json
```

The config goes to `<user config dir>/millennium/projects/<id>/config.json`
(set `MILLENNIUM_LOCAL_DIR` to move it), where `<id>` comes from the clone's
git directory, so every worktree of it uses the same config. Nothing in the
repository changes. A committed config always wins over a local one. Sandboxed
agents cannot write there, and the hooks deny their file tools.

`millennium config export --local` prints the local config as a proposal, to
back it up or apply it on another machine. A local config is lost when the
directory is removed; it is not tied to the plugin's data directory.

## Removing the config

Deleting `.millennium/config.json` does not switch the checks off while the
file is still committed: the engine blocks with "tracked at HEAD but missing
from the working tree". Restore it, or commit its removal deliberately.
