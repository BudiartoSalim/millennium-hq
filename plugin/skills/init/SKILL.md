---
name: init
description: Sets up millennium for a new project or one with little code - chooses languages, conventions and layout with the user, drafts the config as a proposal, and hands the user the apply command. Use when the user starts a new repository with millennium; for a codebase that already has substantial code, use adopt.
argument-hint: "[committed|local]"
disable-model-invocation: true
---

# Set up a new project

Mode requested: $ARGUMENTS

You draft; the user decides and applies. In this skill, `millennium` means
`"${CLAUDE_PLUGIN_ROOT}/scripts/millennium"`.

## 1. Check where you are

- `git rev-parse --show-toplevel`: millennium works per git repository. If
  this is not one, ask whether to `git init`.
- `millennium config validate`: if a config exists, stop and say so.
- `millennium discover`: if it finds substantial code (more than a handful of
  files), suggest `/millennium:adopt` instead, which maps conventions onto an
  existing layout.

## 2. Decide with the user

Ask only what you cannot infer:

- **Committed or local** (unless the arguments say): committed for a project
  they own, local for one they do not.
- **What it is**: a backend service, a web app, a CLI, a monorepo of several.
- **Languages** per part: Go, TypeScript, Python, shell.
- **Conventions**: `clean-architecture` for backends and services,
  `react-feature-based` for React apps, none for scripts and small tools.
  Explain each in a sentence; the convention docs have the layouts.
- **Monorepo or single project**: one component per service or app, with
  `paths` patterns such as `services/*/api` that cover future services too.

Recommend; do not survey every option. A Go backend gets
`clean-architecture`; a React app gets `react-feature-based`.

## 3. Draft

Write `millennium.proposal.json` at the repository root:

```json
{
  "version": 1,
  "config": {
    "version": 1,
    "languages": ["go"],
    "conventions": ["clean-architecture"]
  }
}
```

or, for several parts, `components` (see the configuration docs). Include,
where they apply:

- a `test` recipe for TypeScript with coverage (an lcov report);
- `python.typeCheck: "mypy"` for typed Python;
- `run` with `start` and a localhost `healthUrl`, so qa-tester can start the
  software;
- `exclude` for generated code.

Validate the draft by dry-running it:

```
millennium audit --proposal millennium.proposal.json --tier fast
```

On an empty project this mostly confirms the toolchain: a `toolchain/*`
finding names a tool to install (gofmt, node, python, shellcheck) before the
gates can run.

## 4. Lay out the project (optional)

Offer to create the convention's directory skeleton so the first code lands
in the right layer: for Clean Architecture in Go, `internal/domain`,
`internal/application`, `internal/infrastructure`, `internal/interface` and
`main.go`; for feature-based React, `src/app`, `src/features`,
`src/components`, `src/lib`. For npm projects, add an `.npmrc` with
`ignore-scripts=true` and `save-exact=true`: the `deps` gate requires it.

Create only empty directories (with a `.gitkeep`) and the `.npmrc`; real
code goes through the normal workflow and its gates.

## 5. Hand over

Give the user the command to run themselves:

```
millennium config apply                # committed
millennium config apply --local        # local
```

It shows what it writes and asks them to confirm. Afterwards a new session
picks up the hooks, and `/millennium:lead <task>` runs the first feature
through the full workflow.
