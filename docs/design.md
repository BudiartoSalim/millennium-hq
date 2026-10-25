# millennium: design

Status: agreed in planning on 2026-10-25. Items under
[To verify before building](#to-verify-before-building) are platform details
still to confirm.

## Purpose

millennium is a plugin for AI coding agents, Claude Code first, that makes
quality and security the default outcome of agent-driven development. Its
selling point is that the quality measurement itself is hard to game: rules that
can be checked mechanically are checked by deterministic gates the agents cannot
talk their way past, and the judgement calls that remain go to fresh-context
reviewers whose findings the tech lead must explicitly dispose of.

It generalises the per-repo setup in `server-scripts` (CLAUDE.md, `.claude/rules`,
hooks, `/vet-dependency`) into something any project can install.

## Principles

1. **Gate what can be gated.** Every rule is labelled either *gated* (a
   deterministic check enforces it) or *reviewed* (a reviewer judges it). Prose
   is for what no tool can decide.
2. **Fail closed.** A missing tool, an unreadable config, or a network check that
   could not run is a block, never a silent pass.
3. **Thin and portable.** Users install Claude Code, git, and the toolchains of
   the languages they already use. Nothing else is required.
4. **Strict supply chain.** The `server-scripts` stance (stdlib first, vet every
   dependency, exact pins, lockfiles, no install scripts) is the default, and the
   procedural parts of it become gates.
5. **Harness-agnostic where possible.** Skills (Agent Skills format), the gate
   engine, convention packs, and the AGENTS.md template are portable. Agent
   definitions and hook wiring are a thin per-harness adapter.
6. **Independent verification.** The author of a change never certifies it alone:
   fresh-context reviewers see the spec and the diff, not the author's reasoning,
   and a significant rewrite gets a fresh review.

## Scope of v1

- Harness: Claude Code.
- Languages: TypeScript (Node backend and React), Go, Python.
- Conventions: Clean Architecture for backend code, feature-based structure for
  React, enforced TDD.
- CI: GitHub Actions templates, behind a provider-agnostic interface.

## Roles

| Agent | Skills / modes | Context | Tools |
|---|---|---|---|
| tech-lead | `tech-lead` | Full | All |
| engineer | `engineering-standards` + active packs | Task spec + standards | Edit/Write/Bash, own git worktree |
| qa-engineer | `correctness-review` | Spec + diff (incl. tests) | Read-only on the change, own worktree for proof-of-bug tests |
| qa-tester | `smoke`, `acceptance`, `exploratory` | Spec + run recipe; no implementation source | Bash (run the app, HTTP), browser for web |
| security-engineer | `threat-model`, `security-review`, `security-research` | Spec + diff (+ codebase for research) | Read-only |
| maintainability-reviewer | `maintainability-review` + pack checklists | Spec + diff + conventions | Read-only |
| performance-reviewer | `performance-review` | Spec + diff | Read-only |

Notes:

- **The tech lead is a skill, not a mandatory layer.** `/millennium:lead <task>`
  loads it into the main session, so the user sees and can overrule every
  triage decision. The same skill can be preloaded by a `tech-lead` agent for
  headless runs (CI, background). Sessions that do not invoke it work normally;
  the gates still apply.
- **Agents are split by tools and permissions, persona is a mode.** An agent
  definition is a template that can run as many parallel instances, so one
  `security-engineer` with three modes avoids a bottleneck without a separate
  researcher persona. qa-tester is separate from qa-engineer because it runs the
  software and must stay black-box.
- **qa-engineer owns test quality.** The engineer writes its own tests (TDD), so
  `correctness-review` checks them: do the assertions prove the spec, which
  requirements and edge cases have no test, would a test pass whatever the code
  did. The maintainability reviewer covers test readability only. Every finding
  carries a concrete failure scenario, ideally a failing test.
- **qa-tester is black-box.** A PreToolUse hook denies Read/Grep/Glob on source
  paths when `agent_type` is qa-tester. Bash cannot be path-filtered reliably,
  so this is best-effort; it protects test independence, not a security
  boundary.

## Workflow

```
spec + acceptance criteria (lead, with the user)
  -> [threat-model when the change touches auth, input, secrets, crypto, permissions]
  -> engineer: TDD in its own worktree
  -> gates: red-before-green, format/lint/types, architecture, complexity,
            changed-line coverage, dependencies, secrets
  -> qa-tester smoke            (fail: straight back to the engineer)
  -> in parallel:
       qa-tester acceptance + exploratory
       qa-engineer correctness-review   (always)
       maintainability-reviewer         (always)
       security-engineer review         (by risk)
       performance-reviewer             (by risk)
  -> lead triage: every finding gets a disposition (fix / defer + reason /
     reject + reason) in the decision log
  -> fix loop, bounded rounds: a bug becomes a failing test first, then a fix;
     a significant rewrite gets a fresh review and a re-test
  -> final gates: full suite, supply chain, CI-equivalent run
  -> summary with the decision log
```

Reviewer output is structured JSON (severity, `file:line`, evidence, failure
scenario, suggested fix) so findings can be triaged and logged mechanically.

## Enforcement model

An agent with Bash on the same machine can, in principle, forge anything that
only an in-session hook checks. Enforcement is therefore layered, and every
layer runs the same gate engine so they cannot drift:

1. **Claude Code hooks.** Fast feedback; block an agent's stop while gates fail;
   per-agent rules via `agent_type` (engineer cannot edit acceptance tests,
   qa-tester cannot read source). Repeated stop attempts escalate to the user
   instead of passing (the `server-scripts` stop hook passes on the second
   attempt).
2. **OS sandbox write-deny** on protected paths: gate configs, tool configs
   (tsconfig, lint config), CI workflows, `.millennium/`. Unlike hooks, this also
   covers files written through Bash.
3. **Diff-based checks on merge.** The engineer works on its own worktree
   branch; before the lead merges it, the engine checks the branch diff (for
   example, it must not touch `tests/acceptance/**`). This holds however the
   files were written.
4. **Git hooks** (pre-commit, pre-push) running the engine.
5. **CI with required checks and branch protection.** The root of trust: agents
   cannot touch it. CODEOWNERS requires a human on protected paths, including
   dependency approvals.

Additional rules:

- **Opt-in per repository.** Hooks do nothing in a repo without
  `.millennium/config.json`; once it exists, everything fails closed.
- **Hook-owned state** (test-run records, attempt counters) lives in
  `${CLAUDE_PLUGIN_DATA}`, outside the sandboxed Bash tool's writable area.
- **Config integrity.** The engine verifies that tool configs have not been
  weakened (required tsconfig strict flags present, lint thresholds unchanged)
  rather than trusting them.
- **Suppressions need a reason.** New `eslint-disable`, `@ts-ignore`, `nosec`,
  `# type: ignore`, `//nolint` and similar comments are blocked unless they carry
  a written justification, and every one is listed in the summary.

## Gate engine

### Language: Go, stdlib only

The engine runs on every user's machine (in hooks, git hooks, and CI), so its
runtime is a hard requirement for every user regardless of project language.

| Option | User must install | Problem |
|---|---|---|
| Bash | Nothing on Linux/WSL | macOS ships bash 3.2 (no `mapfile`, no associative arrays); no JSON parsing without jq; hard to keep readable and tested |
| Python | Python 3.11+ | Not preinstalled on Windows or current macOS |
| Node | Node | Claude Code's native installer no longer requires it |
| **Go binary** | **Nothing** | Must be built and distributed per OS/arch |

Go compiles to one static binary with no runtime, and its standard library parses
Go source natively (`go/parser`, `go/ast`). Distribution:

- Reproducible release builds (`CGO_ENABLED=0`, `-trimpath`, pinned Go version)
  for linux/darwin/windows on amd64/arm64.
- The plugin repo commits `checksums.txt`. A launcher script detects the OS and
  architecture itself (users never pick a build), downloads the matching release
  binary into `${CLAUDE_PLUGIN_DATA}` on first use, and verifies the SHA-256
  before it ever runs. Anyone can rebuild and compare hashes.
- Users with Go installed can build from source instead.
- Binaries stay out of the plugin's top-level `bin/`, which claude.ai and Cowork
  refuse to install; skills reference the launcher through `${CLAUDE_PLUGIN_ROOT}`.

### Config: `.millennium/config.json`

JSON because Go's standard library parses it; a published JSON Schema gives
editors autocompletion and validation, which compensates for the lack of
comments.

```json
{
  "$schema": "https://.../millennium.schema.json",
  "languages": ["go", "typescript", "python"],
  "conventions": ["clean-architecture", "react-feature-based"],
  "testing": "tdd-strict",
  "dependencyPolicy": "strict",
  "ci": "github-actions",
  "paths": {
    "acceptanceTests": "tests/acceptance"
  },
  "run": {
    "start": "...",
    "healthUrl": "http://127.0.0.1:8080/health"
  }
}
```

### Analysis without extra tools

Each analysis uses either the engine itself or the toolchain the project already
has, so no linter has to be installed. Go is parsed natively; TypeScript and
Python are parsed by two small helpers of ours (a JS file and a stdlib Python
script) because a correct TS or Python parser in Go would be a large source of
bugs. The helpers only run on projects that have that runtime by definition.

| | Go | TypeScript | Python |
|---|---|---|---|
| Parser | `go/ast` in the engine | TypeScript compiler API, via a small JS helper run with the project's own `typescript` | `ast`, via a stdlib-only helper run with the project's Python |
| Format / vet / types | `gofmt`, `go vet` | `tsc --noEmit` with verified strict flags | optional mypy (`"typeCheck": "mypy" \| "off"`); when on, a missing mypy blocks |
| Coverage | `go test -cover` | Node's built-in test coverage | own `sys.monitoring` helper on 3.12+; coverage.py (vetted) below 3.12, blocking with an install hint when absent |

On top of the parsers, the engine implements, with one definition across all
languages:

- layer and import boundaries (convention packs),
- cognitive complexity, function length, nesting depth,
- banned APIs per language (`eval`, `shell=True`, `InsecureSkipVerify`, string-built
  `exec`, ...),
- `any` / untyped boundaries,
- suppression comments, TODOs without a linked follow-up,
- high-confidence secret patterns.

Initial limits come from `server-scripts`: a function fits on one screen
(about 50 lines), nests at most three levels, and stays under cognitive
complexity 15. Packs and projects can tighten them but not loosen them past a
floor.

## Convention packs

A convention is a pack; the engine is generic.

```
conventions/<name>/
  pack.json            name, languages, layer -> path mapping, required gates
  SKILL.md             guidance the agents load
  rules/               boundary and limit rules the engine evaluates
  review-checklist.md  added to the maintainability reviewer's checklist
  scaffold/<lang>/     starting skeleton (successor of /new-service)
```

v1 packs:

- **clean-architecture** (Go, TypeScript, Python backends): `domain` ->
  `application` (ports + use-cases) -> `infrastructure` (adapters) and
  `interface` (delivery), dependencies pointing inward, `main` as the only
  composition root. Ported from the `server-scripts` rules.
- **react-feature-based**: `app/` (routes, providers), `features/<name>/`
  (components, hooks, api, types), shared `components/`, `lib/`, `hooks/`.
  Imports flow shared -> features -> app; features never import each other.
  Logic lives in plain modules and hooks with unit tests; components stay thin.
  Feature-Sliced Design is a candidate for a later pack.

Future packs (hexagonal, feature-sliced, test-after testing profile) are new
folders, not engine changes.

## Testing

- **TDD (gated).** A test wrapper records each run; a PreToolUse hook refuses
  edits to production code in TDD-scoped layers (`domain`, `application`, React
  logic modules) unless a failing run was recorded for the current change. CI
  backs this with "changed production code has changed tests" and changed-line
  coverage on those layers.
- **Test quality (reviewed).** qa-engineer `correctness-review`, see Roles.
- **Acceptance tests.** qa-tester writes black-box regression tests in the
  developed project's `tests/acceptance/`, committed and run in that project's
  CI. The engineer cannot change them (agent hook + merge-time diff check).
  API and CLI acceptance tests use the language's built-in runner; browser
  end-to-end tests need Playwright and are opt-in under the dependency policy.
- **Built-in runners first:** `go test`, `node:test`, `unittest`.

## Supply chain

Carried over from `server-scripts`: stdlib first, exact pins, committed
lockfiles, `npm ci` with `ignore-scripts`, `min-release-age`, hash-pinned Python
requirements without sdists, digest-pinned images, the `/vet-dependency`
procedure. Turned into gates or added:

- **Approval records.** Any manifest change blocks unless
  `.millennium/deps/` holds a vetting record for that exact name and version.
  Agents draft records; approving one is a human action (protected path,
  CODEOWNERS in CI).
- **Lockfile diff.** New or changed transitive packages are reported and need
  approval too.
- **Release-age cooldown for every ecosystem.** Publish times come from
  proxy.golang.org and PyPI as well as npm, with a security-patch exception.
- **Install scripts block.** New packages flagged `hasInstallScript` in
  `package-lock.json` are refused.
- **Vulnerabilities.** The engine queries OSV for every locked package itself
  (no scanner to install); an unreachable OSV is a block at the final gate.
- **Integrity.** `npm audit signatures`; Go with `GOFLAGS=-mod=readonly` and the
  checksum database on.
- **CI hardening.** Actions pinned to full commit SHAs, minimal `permissions:`,
  no `pull_request_target`, update bots configured with a cooldown.
- **The plugin practises what it preaches.** The engine is stdlib-only Go and the
  helpers are stdlib-only; the plugin itself has no third-party dependencies.

## CI providers

The core command is provider-agnostic (`millennium gate --all`). Each provider
is a thin template set under `ci/<provider>/`, selected by `"ci"` in the config.
v1 ships `github-actions`; others are new template folders.

## Installing into a project

`/millennium:init` writes, after showing the user what it will change:

- `.millennium/config.json` (asks for languages, conventions, run recipe),
- an engineering-standards section in `AGENTS.md`, plus a `CLAUDE.md` that
  imports it when the project already has one,
- sandbox and permission rules protecting the paths above,
- git hooks and the CI template for the chosen provider,
- `CODEOWNERS` entries for protected paths.

## Repository layout

```
millennium-hq/                   repo root = plugin root
  .claude-plugin/plugin.json     Claude Code adapter
  agents/                        thin agent definitions that preload skills
  hooks/hooks.json               hook wiring, all calling the engine
  skills/                        portable core (Agent Skills format)
  conventions/                   convention packs
  ci/<provider>/                 CI templates
  templates/                     AGENTS.md, CODEOWNERS, config skeleton
  engine/                        Go source of the gate engine (stdlib only)
  helpers/                       stdlib-only TS and Python analysis helpers
  scripts/                       launcher, release, checksum tooling
  docs/
  AGENTS.md                      rules for developing this plugin
```

## Lessons from server-scripts

What the v1 gates fix in the earlier hooks:

- The stop hook exits 0 when `stop_hook_active` is set, so a second stop passes
  with errors still present.
- Missing toolchains (`node_modules`, `gofmt`) are skipped, which is a silent
  pass.
- Per-edit checks only see the Edit/Write tools; files changed through Bash are
  never checked. v1 checks the diff.
- Only gofmt, go vet and tsc run automatically; the function-size, nesting and
  complexity limits are prose.
- `to_posix` maps `D:\x` to the Git Bash form `/d/x`, which is wrong on WSL.

## To verify before building

- Exact sandbox settings keys for write-deny, and whether `/millennium:init` may
  write them into project settings.
- That hooks run outside the Bash tool's sandbox, so hook-owned state in
  `${CLAUDE_PLUGIN_DATA}` is out of the agents' reach.
- Hook behaviour on native Windows (Git Bash or PowerShell) for the launcher.
- Bootstrap timing: no plugin install hook is documented, so the launcher
  fetches the binary lazily (first hook call or a SessionStart hook).

## Build order

1. `AGENTS.md` for this repo, config schema, engine skeleton with the diff,
   config-integrity and fail-closed plumbing.
2. clean-architecture pack for Go (engine-native parsing), then TypeScript and
   Python helpers.
3. Hooks and protection layers; the TDD gate.
4. Supply-chain gates.
5. Agents, skills, and the tech-lead playbook.
6. react-feature-based pack, qa-tester, CI templates, `/millennium:init`.
