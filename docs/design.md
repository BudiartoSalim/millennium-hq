# millennium: design

Status: agreed in planning on 2026-10-25. Items marked proposed are designed
but not yet confirmed; they are listed again under
[Open decisions](#open-decisions). User documentation for what is built lives
in [docs/README.md](README.md). Platform details still to confirm are under
[To verify before building](#to-verify-before-building).

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
3. **Agents propose, humans decide.** No agent can change the rules it is
   checked against. Config and other protected files change only through a
   command the user runs.
4. **Thin and portable.** Users install Claude Code, git, and the toolchains of
   the languages they already use. Nothing else is required.
5. **Strict supply chain.** The `server-scripts` stance (stdlib first, vet every
   dependency, exact pins, lockfiles, no install scripts) is the default, and the
   procedural parts of it become gates.
6. **Harness-agnostic where possible.** Skills (Agent Skills format), the gate
   engine, convention packs, and the AGENTS.md template are portable. Agent
   definitions and hook wiring are a thin per-harness adapter.
7. **Independent verification.** The author of a change never certifies it alone:
   fresh-context reviewers see the spec and the diff, not the author's reasoning,
   and a significant rewrite gets a fresh review.

## Scope of v1

- Harness: Claude Code.
- Languages: TypeScript (Node backend and React), Go, Python, and shell (bash),
  which much infrastructure automation depends on.
- Conventions: Clean Architecture for backend code, feature-based structure for
  React, required unit tests.
- Focus: the agent workflow and the in-session gates.
- **Out of scope:** CI templates and git hooks. The engine is a plain CLI, so a
  project that wants CI enforcement can call `millennium gate --require-config`
  from its own pipeline; that is the user's choice and not something v1 ships
  or supports. The tech lead's final CI-equivalent run is the last gate.

## Roles

| Agent | Skills / modes | Context | Tools |
|---|---|---|---|
| tech-lead | `tech-lead` | Full | All |
| engineer (one or more, in parallel) | `engineering-standards` + active packs | Its work package + standards | Edit/Write/Bash, own git worktree |
| qa-engineer | `correctness-review` | Spec + diff (incl. tests) | Read-only on the change, own worktree for proof-of-bug tests |
| qa-tester | `smoke`, `acceptance`, `exploratory` | Spec + run recipe; no implementation source | Bash (run the app, HTTP), browser for web |
| security-engineer | `threat-model`, `security-review`, `security-research` | Spec + diff (+ codebase for research) | Read-only |
| maintainability-reviewer | `maintainability-review` + pack checklists | Spec + diff + conventions | Read-only |
| performance-reviewer | `performance-review` | Spec + diff | Read-only |

Notes:

- **The tech lead is a skill, not a mandatory layer.** `/millennium:lead <task>`
  loads it into the main session, so the user sees and can overrule every
  triage decision. The same skill can be preloaded by a `tech-lead` agent for
  background runs. Sessions that do not invoke it work normally; the gates
  still apply.
- **Agents are split by tools and permissions, persona is a mode.** An agent
  definition is a template that can run as many parallel instances, so one
  `security-engineer` with three modes avoids a bottleneck without a separate
  researcher persona. qa-tester is separate from qa-engineer because it runs the
  software and must stay black-box.
- **qa-engineer owns test quality.** The engineer writes its own unit tests, so
  `correctness-review` checks them: do the assertions prove the spec, which
  requirements and edge cases have no test, would a test pass whatever the code
  did. The maintainability reviewer covers test readability only. Every finding
  carries a concrete failure scenario, ideally a failing test.
- **qa-tester is black-box.** A PreToolUse hook denies Read/Grep/Glob on source
  paths when `agent_type` is qa-tester. Bash cannot be path-filtered reliably,
  so this is best-effort; it protects test independence, not a security
  boundary.
- **When a gate seems wrong, the lead escalates.** The playbook forbids working
  around a gate; the lead brings the case to the user. The stop hook enforces
  the same: repeated blocked attempts escalate to the user instead of passing.

## Workflow

```
spec + acceptance criteria (lead, with the user)
  -> [threat-model when the change touches auth, input, secrets, crypto, permissions]
  -> lead splits the work into packages (see Parallel work)
  -> engineers, in parallel: implement with unit tests, each in its own worktree
  -> lead merges the branches
  -> gates: tests required, format/lint/types, architecture, complexity,
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
  -> fix loop, bounded rounds: every bug fix comes with a regression test;
     a significant rewrite gets a fresh review and a re-test
  -> final gates: the CI-equivalent run (every check, against the target
     branch, config required) plus the full test suite and supply chain
  -> summary with the decision log
```

Reviewer output is structured JSON (severity, `file:line`, evidence, failure
scenario, suggested fix) so findings can be triaged and logged mechanically.

## Parallel work

The tech lead splits a task across several engineer subagents when the parts
are independent, so development is not limited to one engineer.

- **Work packages with disjoint file ownership.** The lead cuts the task by
  feature, module or Clean Architecture layer. Each package has its own slice
  of the spec, its acceptance criteria, and the list of paths it may change.
- **Contracts first.** When packages share an interface (a port, an API shape,
  a DTO), that interface lands first, from the lead or one engineer, and the
  others build against it.
- **One worktree per engineer.** Each engineer is a separate subagent in its
  own git worktree, running in the background alongside the others.
- **Ownership is gated.** At merge time the engine checks each branch's diff
  against the package's declared paths; a change outside them blocks, so two
  engineers cannot silently edit the same file.
- **Integrate, then review.** The lead merges the branches one at a time and
  runs the gates on the integrated result; a failure goes back to the
  package's engineer. The review panel normally sees the integrated diff once;
  a large change can be reviewed per package.
- **Judgement, with a cap.** Tightly coupled work stays with one engineer, and
  a concurrency limit keeps cost and machine load (parallel test suites) in
  check.

## Enforcement model

An agent with Bash on the same machine can, in principle, forge anything that
only an in-session hook checks, so enforcement is layered:

| Layer | Applies to | Does not apply to |
|---|---|---|
| **Plugin hooks** | Every Claude Code session with the plugin enabled, whether or not the tech lead is used, including every subagent's tool calls. They do nothing in a repo that is not opted in. | Sessions with the plugin disabled; other harnesses |
| **Sandbox and permission rules** | Every Claude Code session in the repo, plugin or not: they live in the project's `.claude/settings.json`, which `config apply` writes because plugins cannot ship settings. | Sessions with the sandbox off; commands the user approves to run unsandboxed; other harnesses |
| **Merge-time diff checks** | Every engineer branch the lead merges: ownership, protected paths (for example `tests/acceptance/**`), however the files were written | Changes that never go through the lead |

What each layer does:

1. **Hooks** give fast feedback, block an agent's stop while gates fail, and
   apply per-agent rules via `agent_type` (the engineer cannot edit acceptance
   tests, qa-tester cannot read source). Repeated stop attempts escalate to the
   user instead of passing (the `server-scripts` stop hook passes on the second
   attempt).
2. **The sandbox write-denies protected paths**: `.millennium/`, tool configs
   (tsconfig, lint config), `.git/config` and `.git/hooks`. Unlike hooks, this
   also covers files written through Bash. A hook additionally denies Edit and
   Write on the same paths for every agent, the main session included.
3. **Merge-time checks** hold whatever path the files took.

Every Claude Code session in an opted-in repo loads the plugin: `config apply`
enables it in the project's `.claude/settings.json`, which Claude Code itself
protects from sandboxed agents. A user can still disable it in their own local
settings; that is a human choice.

Honest limits: a command the user approves to run unsandboxed is outside the
sandbox (the setup docs recommend disabling unsandboxed commands); a loosening
the user confirms without reading goes through; other harnesses get the
portable layers only (AGENTS.md, skills) until they offer comparable hooks.

Additional rules:

- **Opt-in per repository.** Hooks do nothing in a repo without a config; once
  one exists, everything fails closed. A config missing from the working tree
  while still tracked at HEAD blocks, so deleting it cannot switch the gates
  off.
- **One exit-status contract.** The engine exits 0 only for a pass (or a repo
  that is not opted in) and 2 for everything else, errors included. Claude
  Code hooks block only on 2 and treat 1 as non-blocking, so 1 is never used.
- **Git cannot be turned against the engine.** Hooks run outside the agents'
  sandbox, and a repository's own config can name programs git runs
  (`core.fsmonitor`). The engine disables fsmonitor on every git call and
  refuses revisions that look like options; `config apply` write-protects
  `.git/config` and `.git/hooks`.
- **Hook-owned state** (stop-attempt counters, gate results) lives in
  `${CLAUDE_PLUGIN_DATA}`, outside the sandboxed Bash tool's writable area.
- **Config integrity.** The engine verifies that tool configs have not been
  weakened (required tsconfig strict flags present, lint thresholds unchanged)
  rather than trusting them.
- **Suppressions need a reason.** New `eslint-disable`, `@ts-ignore`, `nosec`,
  `# type: ignore`, `//nolint` and similar comments are blocked unless they carry
  a written justification, and every one is listed in the summary.

## Changing the rules: proposals and `config apply`

Agents can draft configuration but never make it effective.

```
/millennium:init or /millennium:adopt
  -> the engine discovers the project; an agent drafts millennium.proposal.json
     (an unprotected, git-ignored file that does nothing by itself)
  -> the user runs: millennium config apply
       validates the proposal, shows the diff, and lists every change that
       loosens the rules (a convention removed, a check disabled, a language
       dropped) before asking for confirmation
  -> protected files are written: the config, the AGENTS.md section, the
     project's .claude/settings.json (plugin enabled, sandbox and permission
     rules)
```

`config apply` writes protected paths, so it fails inside an agent's sandbox;
it works from the user's own terminal (and possibly from Claude Code's `!`
prefix, see To verify). Dependency approval records in `.millennium/deps/` use
the same path.

**Disabling a check needs a reason (Proposed).** A config may switch a check off
only with a written reason, and every report lists the disabled checks with
their reasons, so nothing is silently off:

```json
"disable": [
  { "check": "complexity", "reason": "upstream style; 80-line handlers are the norm here" }
]
```

## Adopting an existing project

The `/millennium:adopt` flow below is proposed; the existing-code rule, the
audit and local mode are agreed for v1.

`/millennium:adopt` onboards a project that already has code:

1. **Mode:** committed (`.millennium/config.json` in the repo, for projects the
   user owns) or local (see below).
2. **Discovery (engine, deterministic):** `millennium discover` reports module
   roots (`go.mod`, `package.json`, `pyproject.toml`, `requirements*.txt`),
   languages, test runners, existing lint configs, the import graph between
   directories, and how much existing code already exceeds the limits.
3. **Matching:** the engine evaluates each available pack against each module
   and counts violations; an agent proposes which pack fits which module, and
   drafts a custom pack from the observed structure where none fits. The
   project's own check commands (`npm run lint`, its golangci setup) can be
   listed so they run through the same gate.
4. **Review and apply:** the user reviews the proposal and a dry run of what
   would block today, then runs `millennium config apply`.

**Existing code does not block adoption (built).** Gates look at what a change
touches: new code meets every rule, existing functions that are modified must
not get worse (longer, deeper, more complex), and only new imports are held to
the layer rules.

**Auditing existing code (built).** `millennium audit [--path PATH]` runs every
check over existing code with no allowance for code that was already over a
limit, and summarises findings per rule. It is the starting point for
refactoring: the tech lead turns the report into work packages for parallel
engineers, and the existing-code rule keeps each improvement from slipping
back. Hooks never run it.

**Local mode, for projects the user does not own (agreed for v1).** The config lives on the
user's machine at
`${CLAUDE_PLUGIN_DATA}/projects/<repo-key>/config.json`, not in the repo.
`<repo-key>` derives from git's common directory, so every worktree of a repo
(including parallel engineers') resolves to the same config. A committed config
always wins over a local one, and the report says the local one was ignored.
Agents cannot write there; only the user creates or changes it. Local mode is
not shared across machines and is deleted on uninstall unless the plugin is
removed with `--keep-data`, so it needs export and import commands.

## Monorepos: components (built)

Agreed over per-directory configs: one root file keeps the whole policy in one
place to review and protect, and a pattern such as `services/*/api` covers new
services automatically, where a forgotten per-directory file would leave a new
service unchecked. Nested overrides can be added later without breaking
existing configs. `millennium config explain <file>` shows which component,
layer and limits apply to any file.

A config can list components, each mapping paths to languages, conventions and,
where the layout differs from the pack's default, layer paths:

```json
{
  "version": 1,
  "components": [
    { "paths": ["services/*/api"], "languages": ["go"],
      "conventions": ["clean-architecture"] },
    { "paths": ["services/identity/api", "services/ai-agent/api"], "languages": ["typescript"],
      "conventions": ["clean-architecture"] },
    { "paths": ["services/kokoro-tts/app"], "languages": ["python"],
      "conventions": ["clean-architecture"],
      "layers": { "domain": ["*/domain.py"], "application": ["*/service.py"] } },
    { "paths": ["services/game-dashboard", "services/porto-webapp/web"], "languages": ["typescript"],
      "conventions": ["react-feature-based"] },
    { "paths": ["services/websearch/api"], "languages": ["go"], "conventions": [],
      "reason": "older flat package, not the template" }
  ],
  "exclude": ["**/node_modules/**", "**/dist/**"]
}
```

- Every changed file belongs to exactly one component; the most specific path
  wins, and two equally specific matches are a config error, not a guess.
- Layer paths are relative to the component root.
- A file outside every component still gets the checks that apply to any code
  (secrets, suppressions, TODOs, dependencies), but no convention rules.
- A single-project repo keeps top-level `languages` and `conventions` and no
  `components`.

## Gate engine

### Language: Go, stdlib only

The engine runs on every user's machine, so its runtime is a hard requirement
for every user regardless of project language.

| Option | User must install | Problem |
|---|---|---|
| Bash | Nothing on Linux/WSL | macOS ships bash 3.2 (no `mapfile`, no associative arrays); no JSON parsing without jq; hard to keep readable and tested |
| Python | Python 3.11+ | Not preinstalled on Windows or current macOS |
| Node | Node | Claude Code's native installer no longer requires it |
| **Go binary** | **Nothing** | Must be built and distributed per OS/arch |

Most checks parse structured data (JSON config and hook input, lockfiles,
source code, coverage reports, registry responses); in Bash that would need
extra tools such as jq and curl on every machine, or the security-sensitive
logic written three times in each project's runtime. Go compiles to one static
binary with no runtime, and its standard library parses Go source natively.

### Engine design: analyzers and packs

The engine separates two concerns, so new languages and conventions are
additions, not rewrites:

- **Language analyzers** turn code into facts: imports per file, functions with
  length, nesting and cognitive complexity, comments, calls. Go is analysed in
  the engine (`go/ast`); TypeScript and Python by two small helpers of ours (a
  JS file using the project's own `typescript`, a stdlib Python script using
  `ast`), run with the project's runtime, because a correct TS or Python parser
  in Go would be a large source of bugs. A new language is a new analyzer
  producing the same facts.
- **Convention packs** are rules over those facts, written as data. A new
  convention is a new pack folder. The engine only changes when a convention
  needs a new kind of rule; that rule type is then available to every pack.

### Checks

Every change (hooks and the lead's runs):

| Check | How |
|---|---|
| Config validation | Strict JSON: unknown fields, duplicate keys, trailing data, symlinks and oversized files are errors |
| Change detection | git, NUL-separated output, special files skipped, fsmonitor disabled |
| Toolchain | Required tools present; a missing one blocks |
| Format and types | `gofmt`, `go vet`, `tsc --noEmit`, optional mypy, run with the project's own tools |
| Config integrity | tsconfig (comments and `extends` chains included) and lint thresholds not weakened |
| Architecture | Layer import rules from the active packs |
| Complexity | Function length, nesting depth, cognitive complexity |
| Banned APIs | `eval`, `shell=True`, `InsecureSkipVerify`, string-built `exec`, ... from real parsing, so comments and strings do not count |
| Suppressions | Need a written reason |
| TODOs | Need a linked follow-up |
| Secrets | High-confidence patterns, binary files skipped, bounded reads |
| Tests required | Changed code in tested layers comes with changed tests |
| Changed-line coverage | Changed lines covered by the suite |
| Ownership | Each engineer's branch stays inside its package's paths; protected paths untouched |

Hooks:

| Check | How |
|---|---|
| Hook input | JSON on stdin: tool, file, calling agent |
| Per-agent rules | Engineer cannot edit acceptance tests; qa-tester cannot read source; edit content checked for new suppressions |
| Protected paths | Edit/Write on protected paths denied for every agent |
| Stop gating | Block "done" while gates fail; escalate after repeated attempts |

Supply chain: see [Supply chain](#supply-chain).

The analysis tools per language:

| | Go | TypeScript | Python |
|---|---|---|---|
| Parser | `go/ast` in the engine | TypeScript compiler API, via a small JS helper run with the project's own `typescript` | `ast`, via a stdlib-only helper run with the project's Python |
| Format / vet / types | `gofmt`, `go vet` | `tsc --noEmit` with verified strict flags | optional mypy (`"typeCheck": "mypy" \| "off"`); when on, a missing mypy blocks |
| Coverage | `go test -cover` | Node's built-in test coverage | own `sys.monitoring` helper on 3.12+; coverage.py (vetted) below 3.12, blocking with an install hint when absent |

Initial limits come from `server-scripts`: a function fits on one screen
(about 50 lines), nests at most three levels, and stays under cognitive
complexity 15.

### Config: `.millennium/config.json`

JSON because Go's standard library parses it; the JSON Schema in
`schema/millennium.schema.json` gives editors autocompletion and validation,
which compensates for the lack of comments. Only `version` and `languages` are
required; `run.healthUrl` must be a loopback address so qa-tester can never be
pointed at a shared deployment.

```json
{
  "$schema": "https://.../millennium.schema.json",
  "version": 1,
  "languages": ["go", "typescript", "python"],
  "conventions": ["clean-architecture", "react-feature-based"],
  "testing": "tests-required",
  "dependencyPolicy": "strict",
  "python": { "typeCheck": "off" },
  "paths": {
    "acceptanceTests": "tests/acceptance"
  },
  "run": {
    "start": "...",
    "healthUrl": "http://127.0.0.1:8080/health"
  }
}
```

### Distribution and releases

- Reproducible release builds (`CGO_ENABLED=0`, `-trimpath`, pinned Go version)
  for linux/darwin/windows on amd64/arm64, from this repository; no separate
  release repository.
- The engine has its own tags (`engine/v0.1.0`), so a plugin release does not
  force an engine release.
- The shipped plugin carries a lock file with the engine version and the
  SHA-256 of each platform's binary. A launcher script detects the OS and
  architecture itself (users never pick a build), downloads the matching
  release binary into `${CLAUDE_PLUGIN_DATA}` on first use, and refuses
  anything whose hash does not match.
- Hashes first, then the release: a reviewed PR records the new version and
  hashes; the tag is pushed; the release workflow rebuilds from the tag and
  fails if any binary's hash differs from the committed one, which also proves
  every release was built from the reviewed source.
- The release workflow has minimal permissions (write only in the publishing
  job), actions pinned to full commit SHAs, and tag protection on `engine/v*`;
  build-provenance attestation is optional.
- Users with Go installed can build from source instead.
- Binaries stay out of the plugin's top-level `bin/`, which claude.ai and Cowork
  refuse to install; skills reference the launcher through `${CLAUDE_PLUGIN_ROOT}`.

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

A project can also have its own pack that extends a shipped one. For
`server-scripts`, several prose rules become gated checks that way: the HTTP
delivery package is named `web`, every `http.Server` sets timeouts, bearer
tokens are compared with `ConstantTimeCompare`, no barrel `index.ts` files.

Future packs (hexagonal, feature-sliced, a test-first testing profile) are new
folders, not engine changes.

## Testing

- **Unit tests required (gated).** Changed production code in the layers a
  convention marks as tested (`domain`, `application`, React logic modules)
  must come with added or changed tests in the same change, and its changed
  lines must be covered by the suite. Writing the tests first is welcome but
  not enforced (`"testing": "tests-required"`).
- **Test quality (reviewed).** qa-engineer `correctness-review`, see Roles.
- **Acceptance tests.** qa-tester writes black-box regression tests in the
  developed project's `tests/acceptance/`, committed with the project and part
  of its test suite. The engineer cannot change them (agent hook + merge-time
  diff check). API and CLI acceptance tests use the language's built-in runner;
  browser end-to-end tests need Playwright and are opt-in under the dependency
  policy.
- **Built-in runners first:** `go test`, `node:test`, `unittest`.

## Supply chain

Carried over from `server-scripts`: stdlib first, exact pins, committed
lockfiles, `npm ci` with `ignore-scripts`, `min-release-age`, hash-pinned Python
requirements without sdists, digest-pinned images, the `/vet-dependency`
procedure. Turned into gates or added:

- **Approval records.** Any manifest change blocks unless
  `.millennium/deps/` holds a vetting record for that exact name and version.
  Agents draft records; approving one goes through `config apply`.
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
- **The plugin practises what it preaches.** The engine is stdlib-only Go and the
  helpers are stdlib-only; the plugin itself has no third-party dependencies.

## Repository layout

```
millennium-hq/
  plugin/                        what users install (planned; see below)
    .claude-plugin/plugin.json   Claude Code adapter
    agents/                      thin agent definitions that preload skills
    hooks/hooks.json             hook wiring, all calling the engine
    skills/                      portable core (Agent Skills format)
    conventions/                 convention packs
    helpers/                     stdlib-only TS and Python analysis helpers
    launcher, engine lock        download and verify the engine binary
  engine/                        Go source of the gate engine (stdlib only)
  schema/                        JSON Schema of the project config
  templates/                     AGENTS.md section, config skeleton
  scripts/                       verify, release and checksum tooling
  docs/
  AGENTS.md                      rules for developing this plugin
```

The shipped plugin lives in `plugin/` rather than at the repo root, so users
install only what runs, and the repo's own `CLAUDE.md` (which imports
`AGENTS.md`, because a `CLAUDE.md` above the repo disables the `AGENTS.md`
fallback) does not trigger `claude plugin validate`'s warning about a
`CLAUDE.md` at the plugin root.

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
- `stop-check.sh` calls `git diff` and `git ls-files` without disabling
  `core.fsmonitor`, so a repository config could make the hook run a program
  outside the sandbox.
- `to_posix` maps `D:\x` to the Git Bash form `/d/x`, which is wrong on WSL.

## Shell support

Agreed for v1. Shell files are `.sh` files and extensionless files with a bash
or sh shebang (such as `bin/*`).

- `bash -n` on every changed script.
- shellcheck, **on by default and required (blocking) when on**, with
  `"shell": { "lint": "off" }` for projects that cannot install it. It catches
  the bugs that break shell (unquoted expansions, word splitting, ignored exit
  codes) and nothing in a standard library replaces it.
- Rules that need no parser: a required `set -euo pipefail` (or the variant a
  pack names), banned patterns such as `eval` and `curl ... | sh`, and
  `# shellcheck disable=` needing a reason like every suppression.
- Function length, nesting and complexity for shell need a parser; the known
  Go shell parser is a third-party module, so they wait for a tokenizer of our
  own after v1. Until then the maintainability reviewer covers them.

## Known limitations

- **TypeScript 7.** The TypeScript analyzer parses with the project's own
  `typescript` package through the classic compiler API. TypeScript 7 (the
  native port, July 2026) no longer ships that API; projects on it block with
  a message saying so. Supporting them means moving to TypeScript 7's AST API
  once it is stable, or letting a project point the helper at a TypeScript 6
  install.

## Pending your approval

- **TypeScript test fixture.** The TypeScript analyzer's tests need a
  TypeScript compiler, so `typescript@6.0.3` is a test-only fixture pinned in
  `scripts/fixtures/typescript/package-lock.json` (Microsoft, Apache-2.0, no
  dependencies, no install scripts, published 2026-04-16). It is installed
  outside the repository by `scripts/setup-fixtures.sh` and never shipped.
  AGENTS.md requires approval for any dependency, so this needs a yes.

## Open decisions

1. **`/millennium:adopt`** and **disabling a check needs a reason**: designed
   above, not yet confirmed.

## To verify before building

- Exact sandbox settings keys for write-deny, and that `config apply` may write
  them into the project's `.claude/settings.json`.
- That hooks run outside the Bash tool's sandbox, so hook-owned state in
  `${CLAUDE_PLUGIN_DATA}` is out of the agents' reach.
- Whether commands run with Claude Code's `!` prefix are sandboxed, which
  decides whether the user can run `config apply` without leaving the session.
- Hook behaviour on native Windows (Git Bash or PowerShell) for the launcher.
- Bootstrap timing: no plugin install hook is documented, so the launcher
  fetches the binary lazily (first hook call or a SessionStart hook).

## Build order

1. **Done:** this repo's `AGENTS.md`, the config schema, and the engine
   skeleton (config loading, change detection, the toolchain gate, the report
   model, fail-closed plumbing).
2. **Done:** components, `config explain`, the Go analyzer, the TypeScript
   and Python analyzer helpers, the clean-architecture pack for all three
   (embedded in the engine), function limits with the existing-code rule,
   `millennium audit`, and the user docs in `docs/`. The engine audits its own
   source, and its Python helper, in `scripts/verify.sh`.
3. **Done:** shell support: `bash -n`, strict mode, shellcheck on touched
   lines (`--norc`), shebang detection for extensionless scripts, and a
   line-based shell analyzer for comments and risky constructs.
4. Hooks and protection layers; `config apply`; the tests-required gate.
   **4a done:** the `code` gate (secrets in any text file, banned constructs,
   suppressions without a reason, TODOs without a link, `millennium:allow`
   waivers), with an analysis cache so each file version is parsed once.
5. Supply-chain gates.
6. Agents, skills, and the tech-lead playbook.
7. react-feature-based pack, qa-tester, `/millennium:init`, `/millennium:adopt`,
   local mode.
8. `plugin/` layout, launcher and engine lock, release workflow.
