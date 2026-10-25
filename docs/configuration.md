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

Not every setting is enforced yet: `testing`, `dependencyPolicy`,
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

## Removing the config

Deleting `.millennium/config.json` does not switch the checks off while the
file is still committed: the engine blocks with "tracked at HEAD but missing
from the working tree". Restore it, or commit its removal deliberately.
