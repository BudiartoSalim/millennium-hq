# Configuration

A repository opts in to millennium with `.millennium/config.json` at its root.
Without that file the engine checks nothing. Once it exists, every problem
blocks: a typo in the config is an error, never a silently ignored setting.

The JSON Schema in [`schema/millennium.schema.json`](../schema/millennium.schema.json)
gives editors autocompletion and validation. Point to it with `$schema`:

```json
{ "$schema": "https://<where you host it>/millennium.schema.json", "version": 1 }
```

`millennium-code-qc config validate` checks the file against the engine itself, which
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

Run `millennium-code-qc config explain <file>` to see which component, layer and limits
apply to any file.

## Settings

| Setting | Required | Default | Meaning |
|---|---|---|---|
| `version` | yes | | Config format version; must be `1`. |
| `languages` | single-project form | | Languages to check: `go`, `typescript`, `python`, `shell`. |
| `conventions` | no | `[]` | Convention packs: the built-in `clean-architecture` and `react-feature-based`, or the project's own (see [Convention packs](#convention-packs)). |
| `limits` | no | the conventions' | Single-project form: function limits that replace the conventions' own (see [Function limits](#function-limits)). |
| `components` | monorepo form | | See above. |
| `exclude` | no | `[]` | Paths no check reads. `**` matches any number of segments; a directory excludes everything inside it. This is how generated code in a tested layer is declared: there a generated-code marker in the file exempts it from nothing (see [Generated code](#generated-code)). |
| `test` | no | | Single-project form: the test recipe (below). |
| `disable` | no | `[]` | Rules switched off, each with a reason (below). |
| `testing` | no | `tests-required` | Changed production code needs tests. |
| `dependencyPolicy` | no | `strict` | Every new dependency needs a vetting record. |
| `python.typeCheck` | no | `off` | `mypy` turns on Python type checking; a missing mypy then blocks. |
| `shell.lint` | no | `shellcheck` | `shellcheck` lints shell scripts and is then required; `off` for a project that cannot install it. |
| `paths.acceptanceTests` | no | `tests/acceptance` | Where qa-tester's black-box tests live; the software-engineer agent cannot change them. |
| `run.start`, `run.healthUrl` | no | | How qa-tester starts the software. `healthUrl` must be on `localhost` or a loopback address, so a config can never point tests at a shared deployment. |

Component fields:

| Field | Required | Meaning |
|---|---|---|
| `paths` | yes | Component roots. `"."` is the repository root. |
| `languages` | yes | Languages of this component. |
| `conventions` | no | Convention packs for this component. Each must govern at least one of its languages. |
| `layers` | no | Overrides a convention's layer paths, relative to the component root, for layouts that differ from the pack's default. Each key must be a layer of one of the component's conventions, or `composition`. |
| `reason` | no | Why the component is an exception; shown by `config explain`. |
| `test` | no | The component's test recipe (below). |
| `limits` | no | The component's own function limits, with a reason (below). |

### Generated code

A generated-code marker in a file (Go's `// Code generated ... DO NOT
EDIT.`, `@generated` or `DO NOT EDIT` in the first lines of a TypeScript or
Python file) exempts the file from the function limits only outside tested
layers. In a tested layer (`domain` and `application` under
clean-architecture) the marker counts for nothing: anyone who edits a file
can add one, so a marked file there gets `tests-missing`, `uncovered-change`
and the limits like hand-written code, and each of those findings says that
the marker does not exempt files in a tested layer. To keep generated code
in a tested layer out of the checks, list it in `exclude`, which only the
user changes (through `config apply`):

```json
"exclude": ["services/billing/internal/domain/zz_generated.go"]
```

### Convention packs

A convention is a **pack**: `pack.json`, the rules the gates enforce (layers,
what each may import, which need tests, default paths, function limits), and
`guide.md`, what every agent is told about the convention. The engine knows
no convention by name; it applies whichever packs exist:

- **Built in:** `clean-architecture` and `react-feature-based`, shipped
  inside the engine.
- **The project's own:** `.millennium/conventions/<name>/`, beside the
  config, protected like it. A local config's packs live beside it, in
  `<local config dir>/conventions/<name>/`.

`millennium-code-qc convention list` lists them and `convention show <name>`
prints one. A name in `conventions` that no pack has is a config error.
[Writing a convention](conventions/writing-a-convention.md) covers the pack
format and how to create one; you add a pack the way you change the config,
with a proposal.

### Function limits

Every function must stay within three limits: length in lines, nesting
depth and cognitive complexity (see [Checks](checks.md#limits)). They come
from, in order:

1. the component's own `limits`, field by field;
2. its conventions' packs: the strictest value among them;
3. the engine's defaults: 50 lines, nesting depth 3, cognitive complexity 15.

```json
{ "paths": ["services/parser"], "languages": ["go"], "conventions": ["clean-architecture"],
  "limits": { "functionLines": 120, "reason": "generated parser tables are long by design" } }
```

A component's `limits` need a `reason` of at least ten letters, and at least
one limit; each limit is a whole number from 1 to 1000. Raising a limit is a
loosening `config apply` lists. `config explain <file>` shows the limits in
force for a file.

### Disabling a check

A project can switch off one rule of one gate when it does not fit, everywhere
or only in some paths, but only with a reason, and every report lists it:

```json
"disable": [
  { "check": "structure/function-length", "reason": "upstream style keeps 80-line handlers" }
]
```

```
millennium-code-qc: passed (gates: 6, changed files: 4)
  disabled: structure/function-length (hid 3 findings): upstream style keeps 80-line handlers
```

- `check` is `gate/rule`, as findings print it; a whole gate cannot be
  disabled. A name that is not a rule of any gate is a config error, so a typo
  never leaves a rule running while you believe it off.
- `reason` needs at least ten letters.
- `paths`, optional, limits the disable to the files these patterns match as
  a whole path: `**` matches any number of segments, so `tests/**` covers
  everything under `tests` and `**/*_test.sh` every file named so. Unlike
  `exclude`, a bare directory name covers nothing inside it, and neither does
  a directory whose name happens to match a file pattern. The rule still runs everywhere else, and a
  finding of it that is not about one file is not hidden, so a rule that only
  reports on the whole change (`deps` approvals and advisories, `tests`
  failing) cannot be limited to paths: the report shows such a disable hiding
  nothing. Leave `paths` out to disable the rule everywhere; an empty list, a
  repeated path or `.` is an error. Each rule is listed once, with all its
  paths:

  ```json
  "disable": [
    { "check": "shell/strict-mode", "paths": ["tests/**/*.sh"],
      "reason": "the harness enables errexit inside each test, so the runner can record a failing test" }
  ]
  ```

  ```
    disabled: shell/strict-mode in tests/**/*.sh (hid 2 findings): the harness enables errexit ...
  ```

  `config apply` lists a newly disabled rule, each path newly covered, and a
  rule widened from some paths to everywhere as loosenings.
- Rules that report a check failing to run cannot be disabled, because that
  would let unchecked code pass: `toolchain/tool-missing`, and every
  `analysis-failed`, `analysis-unavailable`, `convention-unavailable`,
  `component-ambiguous`, `syntax-error`, `scan-failed`, `tool-failed`,
  `tsconfig-missing` and `tests-unconfigured` rule.
- For one line of `secret` or `banned-construct`, use a
  [waiver](checks.md#waivers) instead.

### Test recipes

The full tier runs each component's tests. Go and Python have built-in runners;
TypeScript needs a **recipe**, and any component can use one to replace the
built-in runner:

```json
"test": {
  "command": ["npm", "test", "--", "--experimental-test-coverage",
              "--test-reporter=lcov", "--test-reporter-destination=lcov.info",
              "--test-reporter=spec", "--test-reporter-destination=stdout"],
  "lcov": "lcov.info"
}
```

- `command` is an argument list run from the component root without a shell,
  with a 15-minute limit; a non-zero exit is a test failure.
- `lcov` is where the command writes an lcov coverage report, relative to the
  component root. The engine deletes it before the run, so a stale report
  cannot pass for a new one. Node's test runner does not create directories,
  so write it to an existing one, such as the component root. It must cover the TypeScript sources (enable
  source maps when tests run on compiled JavaScript).

`dependencyPolicy` has one value, `strict`, which the `deps` gate enforces;
it exists so a later, looser policy has to be named in the config, where
`config apply` lists it as a loosening. `paths.acceptanceTests` is enforced by
the hooks and `millennium-code-qc scope`; `run` is the recipe the qa-tester agent
follows.

## How strictly the file is read

The engine rejects, with a message naming each problem:

- unknown fields, at any level (`"lnaguages"` is an error, not ignored). The
  message says where the field is and lists the fields allowed there:

  ```
  parse: unknown field "pahts" in $.components[1]; allowed fields there: paths, languages, conventions, layers, reason, test, limits
  ```

  The same applies to proposals, convention packs and approval records. In
  a proposal, the path starts at the part named before it, so
  `proposal config: ... in $.components[0]` is `config.components[0]`;
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

It may instead (or also) carry convention packs, see
[Proposing a convention pack](#proposing-a-convention-pack), and dependency
approvals, see [Approving a dependency](#approving-a-dependency). You apply
it:

```
millennium-code-qc config apply
```

It validates the proposal as strictly as a config, shows every file it will
write, lists every way it **loosens** the rules compared with the current
config (a language, component or convention dropped, a path excluded, a check
disabled or disabled in more paths, mypy or shellcheck switched off, component or acceptance-test paths
moved, a limit raised, a convention pack's rules weakened), and asks you to
type `yes`. Without a terminal, for example from
Claude Code's `!` prefix, it prints a digest instead; re-run with
`--confirm <digest>` after reading the summary. Piped input is refused.

For a config in the repository, it writes:

- `.millennium/config.json`;
- `.claude/settings.json`, merged with what is there: the plugin enabled for
  the project, the sandbox on with unsandboxed commands off, writes to
  `.millennium` denied for every command, `Edit` on it denied for every
  agent, the hosts the full-tier supply-chain checks contact allowed through
  the sandbox (`registry.npmjs.org`, `proxy.golang.org`, `pypi.org`,
  `api.osv.dev` and `tuf-repo-cdn.sigstore.dev`, port 443 only), and subagent
  worktrees branched from the current `HEAD`
  (`worktree.baseRef: "head"`), so parallel engineers start from the tech
  lead's work rather than the default branch;
- a managed section in `AGENTS.md` describing the gates to every agent, plus an
  `@AGENTS.md` import in `CLAUDE.md` when the repository has one;
- the proposed convention packs in `.millennium/conventions/<name>/`;
- `millennium.proposal.json`, the `millennium.proposal/` folder of draft
  packs, and the agent worktrees (`.claude/worktrees/`, and
  `.pi/worktrees/` for [pi](pi.md)) in `.gitignore`.

The applied default proposal is removed; its draft packs stay in
`millennium.proposal/` until you delete them. Inside an agent's sandbox the command
cannot write `.millennium`, and the hooks deny agents from running it at all.

## Proposing a convention pack

A proposal adds or replaces project packs with `packs`, naming each pack's
folder relative to the proposal:

```json
{
  "version": 1,
  "packs": { "hexagonal": "millennium.proposal/conventions/hexagonal" },
  "config": { "version": 1, "languages": ["go"], "conventions": ["hexagonal"] }
}
```

`millennium-code-qc convention new hexagonal --from clean-architecture`
writes such a folder to start from. `config apply`:

- reads each folder as strictly as an applied pack, and refuses a pack named
  like a built-in one;
- checks the proposed config, or the current one when the proposal has none,
  against the packs it would leave in place;
- prints each pack's `pack.json`, and lists what it loosens: a layer or
  language removed, a layer that may now import more or third-party code,
  no longer needs tests or is no longer isolated, changed layer paths, and
  raised limits;
- includes the packs' files in the confirmation digest, so a pack edited
  after you read the summary cannot be applied under it;
- writes them to `.millennium/conventions/<name>/` (or beside the local
  config with `--local`), and refreshes the `AGENTS.md` section.

A pack the proposal does not name stays as it is. To remove one, stop using
it in the config, then delete its folder yourself.

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
you apply it. They are read as strictly too (see
[How strictly the file is read](#how-strictly-the-file-is-read)): a record
that is a symlink, larger than 1 MiB, or has an unknown field, a duplicate key
or trailing data blocks.

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
millennium-code-qc config apply --local my-proposal.json
```

The config goes to `<user config dir>/millennium/projects/<id>/config.json`
(set `MILLENNIUM_LOCAL_DIR` to move it), where `<id>` comes from the clone's
git directory, so every worktree of it uses the same config. Nothing in the
repository changes. A committed config always wins over a local one. Sandboxed
agents cannot write there, and the hooks deny their file tools.

`millennium-code-qc config export --local` prints the local config as a proposal, to
back it up or apply it on another machine. A local config is lost when the
directory is removed; it is not tied to the plugin's data directory.

## Removing the config

Deleting `.millennium/config.json` does not switch the checks off while the
file is still committed: the engine blocks with "tracked at HEAD but missing
from the working tree". Restore it, or commit its removal deliberately.
