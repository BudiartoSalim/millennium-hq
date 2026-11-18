# millennium: design

Status: agreed in planning on 2026-10-25. Items marked proposed are designed
but not yet confirmed; they are listed again under
[Open decisions](#open-decisions). User documentation for what is built lives
in [docs/README.md](README.md). Platform details that shaped the design are under
[Verified platform details](#verified-platform-details).

## Purpose

millennium is a plugin for AI coding agents, Claude Code first, that makes
quality and security the default outcome of agent-driven development. Its
selling point is that the quality measurement itself is hard to game: rules that
can be checked mechanically are checked by deterministic gates the agents cannot
talk their way past, and the judgement calls that remain go to fresh-context
reviewers whose findings the tech lead must explicitly dispose of.

It generalises the per-repo setup in `server-scripts` (CLAUDE.md, `.claude/rules`,
hooks, `/vet-dependency`) into something any project can install.

## Naming

`millennium` is the product; `millennium-code-qc` is one tool inside it, the
code quality engine (`code-qc/`). The split leaves room for checks that are not
about code: a later tool sits beside it as `millennium-<name>`, with its own
directory, lock and tags, under the same plugin and config.

| Names the product | Names this tool |
|---|---|
| Plugin `millennium`, marketplace `millennium-hq` | Command `millennium-code-qc`, Go module of the same name |
| Skills `/millennium:*`, agents `millennium:*` | Directory `code-qc/`, launcher `plugin/scripts/millennium-code-qc` |
| `.millennium/`, `millennium.proposal.json` | `plugin/code-qc.lock`, tags `code-qc/v*`, release files `millennium-code-qc-<os>-<arch>` |
| `millennium:allow`, `MILLENNIUM_LOCAL_DIR`, `MILLENNIUM_STATE_DIR` | `MILLENNIUM_CODE_QC` |

The engine also holds what is not strictly code quality: the `deps` gate, and
the agent enforcement and rule changes (`hook`, `config apply`, `scope`). They
stay in it until a second tool exists and shows what they must share.

Skills, agents and gates are named for what they do, not with a bare word a
sibling could also claim: `tech-lead` not `lead`, `software-engineer` not
`engineer` beside `security-engineer`, `init-project` not `init`, and the
`hygiene` gate not `code` in a tool where every gate checks code. Every
reviewer of a change is `<topic>-reviewer` with a `<topic>-review` skill;
`security-engineer` is the exception because it does more than review.

## Principles

1. **Gate what can be gated.** Every rule is labelled either *gated* (a
   deterministic check enforces it) or *reviewed* (a reviewer judges it). Prose
   is for what no tool can decide.
2. **Fail closed.** A missing tool, an unreadable config, or a network check that
   could not run is a block, never a silent pass.
3. **Agents propose, humans decide.** No agent can change the rules it is
   checked against in a project using the plugin: config and other protected
   files change only through a command the user runs. This repository is the
   exception: here agents may change `.millennium/` when the user asks (see
   `AGENTS.md`).
4. **Thin and portable.** Users install Claude Code (or pi), git, and the
   toolchains of the languages they already use. Nothing else is required.
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

- Harness: Claude Code, and pi through an extension (added 2026-11-13; see
  "pi" below).
- Languages: TypeScript (Node backend and React), Go, Python, and shell (bash),
  which much infrastructure automation depends on.
- Conventions: Clean Architecture for backend code, feature-based structure for
  React, required unit tests.
- Focus: the agent workflow and the in-session gates.
- **Out of scope:** CI templates and git hooks. The engine is a plain CLI, so a
  project that wants CI enforcement can call `millennium-code-qc gate --require-config`
  from its own pipeline; that is the user's choice and not something v1 ships
  or supports. The tech lead's final CI-equivalent run is the last gate.

## Roles

| Agent | Skills / modes | Context | Tools |
|---|---|---|---|
| tech-lead | `tech-lead` | Full | All |
| software-engineer (one or more, in parallel) | `engineering-standards` + active packs; `solution-design` for research briefs | Its work package or research brief + standards | Edit/Write/Bash, web, own git worktree |
| correctness-reviewer | `correctness-review` | Spec + diff (incl. tests) | Read-only on the change, own worktree for proof-of-bug tests |
| qa-tester | `smoke`, `acceptance`, `exploratory` | Spec + run recipe; no implementation source | Bash (run the app, HTTP), browser for web |
| security-engineer | `threat-model`, `security-review`, `security-research` | Spec + diff (+ codebase for research) | Read-only |
| maintainability-reviewer | `maintainability-review` + pack checklists | Spec + diff + conventions | Read-only |
| performance-reviewer | `performance-review` | Spec + diff | Read-only |

Notes:

- **The tech lead is a skill, not a mandatory layer.** `/millennium:tech-lead <task>`
  loads it into the main session, so the user sees and can overrule every
  triage decision. The same skill can be preloaded by a `tech-lead` agent for
  background runs. Sessions that do not invoke it work normally; the gates
  still apply.
- **Agents are split by tools and permissions, persona is a mode.** An agent
  definition is a template that can run as many parallel instances, so one
  `security-engineer` with three modes avoids a bottleneck without a separate
  researcher persona. qa-tester is separate from correctness-reviewer because it runs the
  software and must stay black-box.
- **correctness-reviewer owns test quality.** The engineer writes its own unit tests, so
  `correctness-review` checks them: do the assertions prove the spec, which
  requirements and edge cases have no test, would a test pass whatever the code
  did. The maintainability reviewer covers test readability only. Every finding
  carries a concrete failure scenario, ideally a failing test.
- **qa-tester is black-box.** A PreToolUse hook denies Read/Grep/Glob on source
  paths when `agent_type` is qa-tester. Bash cannot be path-filtered reliably,
  so this is best-effort; it protects test independence, not a security
  boundary.
- **Design before the spec.** The lead works out how to build a change with
  the user before writing the spec, like a design discussion: for an
  open-ended request it studies the reference system, surveys the approaches
  and prior art, tests uncertain claims with throwaway spikes outside the
  repository, and proposes options with trade-offs. The method lives in the
  `solution-design` skill, so the lead, an engineer given a research brief,
  or the user alone (no opt-in needed) can run it. Research goes to engineers
  rather than a researcher agent: they already have web access and a
  worktree for spikes, and a brief keeps their output to a design note. The
  chosen approach is part of the spec, so reviewers check the code against
  the decision.
- **When a gate seems wrong, the lead escalates.** The playbook forbids working
  around a gate; the lead brings the case to the user. The stop hook enforces
  the same: repeated blocked attempts escalate to the user instead of passing.

## Workflow

```
understand and design (lead, with the user; solution-design): reference
  behaviour, approaches others use, spikes, options with trade-offs
  -> the user decides
  -> spec + acceptance criteria + the chosen approach (lead, with the user)
  -> [threat-model when the change touches auth, input, secrets, crypto, permissions]
  -> lead splits the work into packages (see Parallel work)
  -> engineers, in parallel: implement with unit tests, each in its own worktree
  -> lead merges the branches
  -> gates: tests required, format/lint/types, architecture, complexity,
            changed-line coverage, dependencies, secrets
  -> qa-tester smoke            (fail: straight back to the engineer)
  -> in parallel:
       qa-tester acceptance + exploratory
       correctness-reviewer correctness-review   (always)
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
- **Ownership is gated.** At merge time the engine checks each package's
  diff, from the commit its worktree was dispatched from, against the
  package's declared paths; a change outside them blocks, so two
  engineers cannot silently edit the same file.
- **Integrate, then review.** The lead checks each package on what its
  engineer changed since the worktree was dispatched (a worktree dispatched
  after other packages merged holds their changes too, which are not this
  package's to answer for), merges the branches one at a time and runs the
  gates on the integrated result; a failure goes back to the package's
  engineer. The review panel normally sees the integrated diff once; a large
  change can be reviewed per package.
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
pi offers them: its extension runs the hooks' layer, but pi has no sandbox,
so the second layer is missing there (see "pi" below).

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
/millennium:init-project or /millennium:adopt-project
  -> the engine discovers the project; an agent drafts millennium.proposal.json
     (an unprotected, git-ignored file that does nothing by itself)
  -> the user runs: millennium-code-qc config apply
       validates the proposal, shows the diff, and lists every change that
       loosens the rules (a convention removed, a check disabled, a language
       dropped) before asking for confirmation
  -> protected files are written: the config, the AGENTS.md section, the
     project's .claude/settings.json (plugin enabled, sandbox and permission
     rules)
```

`config apply` writes protected paths, so it fails inside an agent's sandbox;
it works from the user's own terminal and from Claude Code's `!` prefix. Dependency approval records in `.millennium/deps/` use
the same path.

**Disabling a check needs a reason (built).** A config may switch a check off
only with a written reason, and every report lists the disabled checks with
their reasons, so nothing is silently off:

```json
"disable": [
  { "check": "complexity", "reason": "upstream style; 80-line handlers are the norm here" }
]
```

A disable can be limited to paths, so a local convention that conflicts with
one rule does not cost every other check on those files (as `exclude` would)
or leave a finding open: `{ "check": "shell/strict-mode", "paths":
["tests/**"], "reason": "..." }`. It hides only that rule's findings about
files the paths match as whole paths (not, as with `exclude`, anything inside
a directory whose name matches, which an agent could create to escape a
check); a finding about no file stays. Per-rule exceptions
live in the config, which the user applies, rather than in inline waivers an
agent can write: `config apply` lists each newly covered path, or a rule
widened to everywhere, as a loosening. Inline waivers stay limited to
`secret` and `banned-construct`, where the exception is about one line.

## Adopting an existing project

`/millennium:adopt-project` (built) onboards a project that already has code:

1. **Mode:** committed (`.millennium/config.json` in the repo, for projects the
   user owns) or local (see below).
2. **Discovery (engine, deterministic):** `millennium-code-qc discover` reports module
   roots (`go.mod`, `package.json`, `pyproject.toml`, `setup.py`,
   `requirements.txt`), code and test files per language, React use, code
   outside any module, and how many files each pack's default layout already
   places in a layer. It reads file names and `package.json` only, and drafts
   a proposal that is validated as a config.
3. **Matching:** the agent confirms fits with `config explain`, maps layers
   onto a differing layout with `layers` overrides, or leaves a module without
   a convention, with a reason. (Drafting whole custom packs, an import graph,
   and running a project's own lint commands through the gate are not built.)
4. **Review and apply:** `millennium-code-qc audit --proposal <file>` shows what the
   rules find in today's code without applying anything; the user reviews it
   and runs `millennium-code-qc config apply`. `/millennium:init-project` is the same flow for
   a new project, with an optional layout skeleton.

**Existing code does not block adoption (built).** Gates look at what a change
touches: new code meets every rule, existing functions that are modified must
not get worse (longer, deeper, more complex), and only new imports are held to
the layer rules.

**Auditing existing code (built).** `millennium-code-qc audit [--path PATH]` runs every
check over existing code with no allowance for code that was already over a
limit, and summarises findings per rule. It is the starting point for
refactoring: the tech lead turns the report into work packages for parallel
engineers, and the existing-code rule keeps each improvement from slipping
back. Hooks never run it.

**Local mode, for projects the user does not own (built).** The config lives on the
user's machine at
`<user config dir>/millennium/projects/<repo-key>/config.json`, not in the repo
(not the plugin's data directory, which an uninstall deletes).
`<repo-key>` derives from git's common directory, so every worktree of a repo
(including parallel engineers') resolves to the same config. A committed config
always wins over a local one. Agents cannot write there; only the user creates
or changes it, with `millennium-code-qc config apply --local`. Local mode is not
shared across machines; `millennium-code-qc config export --local` and applying the
export elsewhere move it.

## Monorepos: components (built)

Agreed over per-directory configs: one root file keeps the whole policy in one
place to review and protect, and a pattern such as `services/*/api` covers new
services automatically, where a forgotten per-directory file would leave a new
service unchecked. Nested overrides can be added later without breaking
existing configs. `millennium-code-qc config explain <file>` shows which component,
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
  length, nesting and cognitive complexity, comments, calls, and the file's
  runtime code (the lines that hold runtime tokens, and a SHA-256 digest of
  the runtime token sequence), which the tests gate uses so that comments,
  whitespace and erased TypeScript types need no test and no coverage. Each
  language's own scanner or compiler classifies, erring toward runtime: Go's
  `go/scanner`, the TypeScript compiler's comment-free `transpileModule`
  output and source map (plus the syntax it can drop that still runs, such
  as value imports and `const enum`), and Python's `tokenize`. Runtime facts
  are known or unknown; unknown (an analysis failure, a helper answer
  without them, content a helper cannot number as git does) counts every
  line as runtime and every file as changed. Lines are physical, as git
  numbers them: Go's `//line` directives move none. Go is analysed in
  the engine (`go/ast`); TypeScript and Python by two small helpers of ours (a
  JS file using the project's own `typescript`, a stdlib Python script using
  `ast`), run with the project's runtime, because a correct TS or Python parser
  in Go would be a large source of bugs. A new language is a new analyzer
  producing the same facts.
- **Convention packs** are rules over those facts, written as data, with a
  guide for the agents. A project adds its own without an engine release
  (see Convention packs). The engine only changes when a convention needs a
  new kind of rule; that rule type is then available to every pack.

### Checks

Every change (hooks and the lead's runs):

| Check | How |
|---|---|
| Config validation | Strict JSON: unknown fields, duplicate keys, trailing data, symlinks and oversized files are errors |
| Change detection | git, NUL-separated output, special files skipped, the sandbox's placeholders left out (below), any other listed path that is gone or cannot be inspected blocking, fsmonitor disabled |
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
| Ownership | Each package's change, from the commit its worktree was dispatched from, stays inside its package's paths; protected paths untouched |

Claude Code's sandbox mounts a device over each protected path that does
not exist while a sandboxed command runs. Inside the sandbox git lists a
device, which change detection skips as a special file; outside it, where
hooks run, git lists an empty untracked file that vanishes when the command
ends, often while the gates run. So an untracked path that is empty or gone
is left out when it has a protected name at any depth (shell profiles, git
and editor config, `.mcp.json`) or sits directly in a `.claude` directory,
whose protected set grows between releases. Tool config such as `.npmrc` is
not exempt: even empty, it changes how a tool behaves. Claude Code's
settings files directly in `.claude` are exempt too, although they are
protected paths: an empty one holds no settings, agents cannot write them
(the guard and the sandbox both refuse), and excluding them would bring the
false blocks back for every project that has none.

What exempts a file in a tested layer depends only on what agents cannot
change: the config's `exclude` and the layer the config and packs assign.
A generated-code marker is file content, which any agent can write, so in a
tested layer it exempts the file from neither the tests gate nor the limits;
a finding on a marked file there says so and points to `exclude`. Outside
tested layers the marker still exempts a file from the limits, a residual
risk accepted because only the limits are at stake there.

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
- The engine has its own tags (`code-qc/v0.1.0`), so a plugin release does not
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
  job), actions pinned to full commit SHAs, and tag protection on `code-qc/v*`;
  build-provenance attestation is optional.
- Users with Go installed can build from source instead.
- Binaries stay out of the plugin's top-level `bin/`, which claude.ai and Cowork
  refuse to install; skills reference the launcher through `${CLAUDE_PLUGIN_ROOT}`.
- pi installs the whole repository as a git package (`pi install
  git:github.com/BudiartoSalim/millennium-hq`); the root `package.json` only
  names the extension and the skills, and has no dependencies or scripts,
  because pi runs `npm install` in the clone. The same launcher and lock
  fetch the engine.

## pi

Decided with the user on 2026-11-13. pi has an extension API with the hook
points millennium needs, but no subagents and no sandbox.

- **One adapter, the same engine.** The extension (`pi/src/`, TypeScript, Node
  standard library only) translates pi's `tool_call`, `agent_before_settle` and
  `session_start` events into the engine's hook protocol (Claude Code's format)
  and runs the launcher. Every decision stays in the engine.
- **Agents: millennium's own tool**, not pi's example subagent extension
  (third-party code) and not a single-session workflow (which would lose
  fresh-context review and black-box QA). `millennium_agent` starts each agent
  as a `pi --print --mode json` process from the same `plugin/agents/*.md`, with
  the agent's skills appended to its system prompt, its tools mapped to pi's,
  its role in its environment for the guard, and a worktree under
  `.pi/worktrees/` when the agent is isolated in Claude Code. Agents load the
  extension explicitly and get no agent tool themselves.
- **No sandbox: accepted and documented.** Shell writes to protected paths are
  caught by pattern matching only; the docs recommend a container for hard
  guarantees. Tamper detection after the fact was considered and rejected: it
  adds engine work and someone with shell access could forge its record.
- **Protected in every harness:** `.pi/settings.json` and `.pi/extensions/`,
  since a project extension runs inside pi and could switch the guard off.
- **Skills stay single-source.** Instead of pi copies, the session's
  `millennium` system prompt section says how to read the Claude Code terms
  (`${CLAUDE_PLUGIN_ROOT}`, `$ARGUMENTS`, `/millennium:`, agents, SendMessage).
- **Type checking without dependencies.** The extension declares the parts of
  pi's API and of Node it uses (`pi-api.ts`, `node.d.ts`) instead of depending
  on pi's packages or `@types/node`; tests run against the real Node, and the
  extension was run end to end against pi 1.5.2 with its scripted test model.

## Convention packs

The engine is convention-agnostic: a convention is data (a **pack**), and
nothing in the engine names one. Not every project follows the same
conventions, so a project can add its own without an engine release.
Decided with the user on 2026-11-15.

### A pack

```
<name>/
  pack.json   the enforced rules: languages, layers and their import rules,
              tested and isolated layers, default layer paths per language,
              default function limits, discovery hints
  guide.md    the soft rules, for agents: what belongs in each layer, naming,
              what reviewers look for
```

- **pack.json** (format 1): `name`, `languages`, `layers` (`name`,
  `mayImport`, `thirdParty`, `tested`, `isolated`), `paths` (language ->
  layer -> patterns, `composition` reserved), `limits` (`functionLines`,
  `nesting`, `cognitive`; omitted fields take the engine's defaults of 50, 3
  and 15), and `suggest.dependencies` (package names that make `discover`
  suggest the pack for a module). Read strictly, like the config: unknown
  fields, duplicate keys and trailing data are errors.
- **guide.md** is required and at most 8,000 characters. It is what agents
  are told about the convention, so it has to be short enough to always be
  in their context (see Guides below).

### Where packs come from

| Source | Location | Who changes it |
|---|---|---|
| Built in | embedded in the engine (`infrastructure/packs/builtin/<name>/`) | an engine release |
| Project | `.millennium/conventions/<name>/` | the user, through `config apply` |
| Local mode | `<local config dir>/conventions/<name>/` | the user, through `config apply --local` |

Project packs sit inside `.millennium/`, so they have exactly the config's
protection: the guard denies agents' writes, Claude Code's sandbox denies
shell writes, and changes arrive only as proposals the user applies.

- **No extending.** To vary a convention, copy it under a new name
  (`millennium-code-qc convention new <name> --from <pack>`) and edit the
  copy. One pack is the whole rule set, so there are no merge rules to
  reason about.
- **No shadowing.** A project pack with a built-in pack's name is an error.
- **Fail closed.** A pack that cannot be read, is too large, is a symlink,
  has unknown fields, or whose folder name differs from its `name` blocks
  every check with a message, as a broken config does.

### Using packs in the config

- A component's `conventions` must name packs that exist (built in, or a
  project pack); an unknown name is a config error. Each pack must govern at
  least one of the component's languages.
- **Limits:** a file's limits come from its component's `limits`, field by
  field, else the strictest value among its conventions' packs, else the
  engine defaults. A component `limits` override needs a `reason`. A
  single-project config takes `limits` at the top level.

### Proposals and `config apply`

A proposal adds or replaces project packs with
`"packs": {"<name>": "<folder relative to the proposal>"}`. The folder holds
`pack.json` and `guide.md`; `convention new` writes one at
`millennium.proposal/conventions/<name>/`, git-ignored like the proposal.
The confirmation digest covers the packs' files. `config apply` writes them
to `.millennium/conventions/<name>/` and lists what they loosen next to the
config's own loosenings:

- a layer removed, or a language removed;
- a layer gaining `mayImport` entries or `thirdParty`, or losing `tested` or
  `isolated`;
- layer paths changed;
- a limit raised, in a pack or a component.

A pack the proposal does not name is left as it is. Removing one is a manual
step, after no component uses it.

### Guides reach every agent

The guides of the conventions a repository uses are part of the session
briefing, with nothing for the user to set up:

- **Claude Code:** the SessionStart hook (the main session) and a
  SubagentStart hook (every subagent, the qa-tester included: knowing the
  layout does not expose the implementation, and it helps with acceptance
  test structure).
- **pi:** the briefing section of every run's system prompt; agents started
  by `millennium_agent` get it through their own session start.

Claude Code caps a hook's context at 10,000 characters and replaces anything
longer with a file path and a preview. So the briefing includes whole guides
while they fit, and names the rest with the command that prints them
(`millennium-code-qc convention show <name>`). The skills no longer carry
convention-specific prose: they tell agents to follow the guides.

### Commands

- `convention list`: every available pack and where it comes from.
- `convention show <name>`: the pack's rules, effective limits and guide.
- `convention new <name> [--from <pack>]`: a draft pack to edit, copied from
  an existing one (default `clean-architecture`), with every limit written
  out so the knobs are visible.

### What still needs the engine

A pack can express only the rule types the engine knows: layer imports,
third-party imports, tested layers, isolated units, and function limits. A
new kind of rule (naming, required files, calls banned in a layer, every
`http.Server` setting timeouts) is added to the engine once, and is then
available to every pack as data. Rules stay declarative: no scripts in
packs.

### Built-in packs

- **clean-architecture** (Go, TypeScript, Python backends): `domain` ->
  `application` (ports + use-cases) -> `infrastructure` (adapters) and
  `interface` (delivery), dependencies pointing inward, `main` as the only
  composition root. Ported from the `server-scripts` rules.
- **react-feature-based**: `app/` (routes, providers), `features/<name>/`
  (components, hooks, api, types), shared `components/`, `lib/`, `hooks/`.
  Imports flow shared -> features -> app; features never import each other
  (an `isolated` layer: each match of `src/features/*` is a unit no sibling
  may import). Shared code and features both need unit tests, components
  included; keeping components thin keeps that cheap. Suggested by
  `discover` for modules that depend on `react`.

Candidates for later packs: hexagonal, Feature-Sliced Design.

## Testing

- **Unit tests required (gated).** Changed production code in the layers a
  convention marks as tested (`domain`, `application`, React logic modules)
  must come with added or changed tests in the same change, and its changed
  lines must be covered by the suite. Writing the tests first is welcome but
  not enforced (`"testing": "tests-required"`).
- **Test quality (reviewed).** correctness-reviewer `correctness-review`, see Roles.
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

- **Approval records (built).** A lockfile change blocks unless
  `.millennium/deps/` holds a vetting record for each new name and version.
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
  .claude-plugin/marketplace.json  this repository as the millennium-hq marketplace
  package.json, .npmrc           this repository as a pi package (manifest only)
  pi/                            the pi extension (TypeScript, Node stdlib only)
  plugin/                        what users install
    .claude-plugin/plugin.json   Claude Code adapter
    agents/                      thin agent definitions that preload skills
    hooks/hooks.json             hook wiring, all calling the launcher
    skills/                      portable core (Agent Skills format)
    scripts/millennium-code-qc           launcher: verifies and runs the locked engine
    code-qc.lock                  engine version and per-platform SHA-256
  code-qc/                        Go source of the gate engine (stdlib only);
                                 convention packs and the TS and Python helpers
                                 are embedded in it, so they cannot be edited
                                 on disk
  schema/                        JSON Schema of the project config
  scripts/                       verify, file-mode check, launcher tests, reproducible release builds
  .github/workflows/             the engine release workflow
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

- **Windows** is supported through Git Bash (the hooks run in shell form,
  starting the launcher with `/bin/sh`, and the launcher recognises Git Bash's
  `uname`), but has not been tried on a
  real Windows machine. Without Git Bash, Claude Code runs shell-form hooks
  in PowerShell and the launcher cannot start: the guard then blocks every
  tool call (it fails closed), while stop hooks cannot block.
- **Lockfiles read**: `package-lock.json` v2/v3, `go.sum` and
  `requirements*.txt`. `pnpm-lock.yaml`, `yarn.lock`, `uv.lock`,
  `poetry.lock` and `Pipfile.lock` get pinning checks only, no approvals.
- **Shell scripts without an extension** are found by their shebang in the
  gates, but `millennium-code-qc discover` counts files by extension only.
- **pi** has no sandbox, so shell commands are kept off protected paths by
  pattern matching only; an extension the user installs can change a tool
  call after the guard; only pi's built-in tools are guarded; `millennium_agent`
  waits for its agents rather than running them in the background. pi on
  Windows is untested.

## Approved dependencies

- **`typescript@6.0.3`, a test-only fixture (approved by the user,
  2026-10-31).** The TypeScript analyzer's tests need a TypeScript compiler,
  so it is pinned in `scripts/fixtures/typescript/package-lock.json`
  (Microsoft, Apache-2.0, no dependencies, no install scripts, published
  2026-04-16). It is installed outside the repository by
  `scripts/setup-fixtures.sh` and never shipped. Its record is
  `.millennium/deps/npm/typescript@6.0.3.json`. Since 2026-11-13 the same
  version is the pi extension's dev dependency (`pi/package-lock.json`), for
  type checking only; `verify.sh` links the fixture's install there, and pi
  never installs dev dependencies.

## Pending

- Publish the engine release `code-qc/v0.2.0`, which adds convention packs.
  `plugin/code-qc.lock` already names it, so it must be published before
  users update the plugin.

## Open decisions

None.

## Verified platform details

- The sandbox write-deny key is `sandbox.filesystem.denyWrite`, and network
  hosts are allowed with `sandbox.network.allowedDomains`; `config apply`
  writes both into the project's `.claude/settings.json`.
- Hooks run outside the Bash tool's sandbox, so hook-owned state is out of
  the agents' reach.
- Commands run with Claude Code's `!` prefix are unsandboxed in most
  sessions, so the user can run `config apply` without leaving the session.
- Exec-form hooks need a real executable on Windows; shell-form hooks run
  through Git Bash there, so the hooks use shell form.
- No plugin install hook exists: the SessionStart hook downloads the engine
  on the first session.
- Subagent worktrees branch from the default branch unless
  `worktree.baseRef` is `"head"`, which `config apply` sets.
- GitHub serves release assets for a tag containing a slash at the plain path
  (`releases/download/code-qc/v0.1.0/<asset>`), so the lock's URL needs no
  escaping; checked against a public project that tags this way.
- Binaries built with the official `go1.26.10` tarball, which the workflow
  uses, are byte-identical to those from a local install of the same version.
- pi 1.5.2 (checked end to end with its scripted test model): `tool_call`
  handlers may mutate `event.input` and see codemode's nested calls;
  `agent_before_settle` returning `continue: true` with a `custom_message` entry
  sends the agent back; a `before_agent_start` handler adding to
  `systemPromptOptions.sections` reaches the model and the transcript, while a
  returned `systemPrompt` reaches only the model; `--session <path>` creates
  the session file when missing and continues it later; tool parameters may
  be plain JSON Schema; `pi install` of a git package runs `npm install
  --omit=dev` at the clone's root; print mode waits for piped stdin to end.

## Build order

1. **Done:** this repo's `AGENTS.md`, the config schema, and the engine
   skeleton (config loading, change detection, the toolchain gate, the report
   model, fail-closed plumbing).
2. **Done:** components, `config explain`, the Go analyzer, the TypeScript
   and Python analyzer helpers, the clean-architecture pack for all three
   (embedded in the engine), function limits with the existing-code rule,
   `millennium-code-qc audit`, and the user docs in `docs/`. The engine audits its own
   source, and its Python helper, in `scripts/verify.sh`.
3. **Done:** shell support: `bash -n`, strict mode, shellcheck on touched
   lines (`--norc`), shebang detection for extensionless scripts, and a
   line-based shell analyzer for comments and risky constructs.
4. Hooks and protection layers; `config apply`; the tests-required gate.
   **4a done:** the `hygiene` gate (secrets in any text file, banned constructs,
   suppressions without a reason, TODOs without a link, `millennium:allow`
   waivers), with an analysis cache so each file version is parsed once.
   **4b done:** the `build` gate (gofmt and go vet per touched module, the
   project's own tsc, tsconfig strictness through `tsc --showConfig`,
   optional mypy).
   **4c done:** the `tests` gate (changed logic in tested layers needs changed
   tests; the full tier runs the suites and requires every changed executable
   line to run), tiers (`--tier fast|full`), test recipes with lcov, and packs
   that declare the languages they govern.
   **4d done:** `disable` entries (one gate/rule, a reason, listed on every
   report; fail-closed rules cannot be disabled), gates that declare their
   rules, and undeclared rules treated as an engine bug.
   **4e done:** `millennium-code-qc hook` (PreToolUse guard for protected paths,
   acceptance tests and black-box qa-tester; Stop and SubagentStop run the
   fast tier, three attempts then an escalation to the user; SessionStart
   context), `onFailure: "block"` on the guard, `millennium-code-qc scope` for
   engineer branches, and `plugin/hooks/hooks.json` with a first launcher.
   Verified: hooks run outside the sandbox, the sandbox already protects
   `.git/config` and `.git/hooks`, `!` commands are unsandboxed, and Stop
   hooks cannot block when they fail to run.
   **4f done:** `millennium-code-qc config apply` (strict proposal parsing, the
   loosenings listed, confirmation by a typed yes at a terminal or a
   matching `--confirm` digest, piped input refused; writes the config,
   merged project settings, the AGENTS.md section and .gitignore) and
   `config export`. Local mode is built with it: local configs under the user
   config directory, keyed by git's common directory, used when nothing is
   committed, protected by the hooks.
5. Supply-chain gates. **Done:** the `deps` gate (approval records, one
   file per package version, covering transitive packages; install scripts;
   pinning of package.json, requirements, `.npmrc`, workflow actions and
   container images on touched lines; in the full tier the release-age
   cooldown from npm, the Go proxy and PyPI, OSV for every locked package,
   `go env` integrity and `npm audit signatures`); approvals carried in
   proposals and written by `config apply`. A failing registry blocks.
   `go.sum` `/go.mod`-only lines need no approval (they never build).
6. Agents, skills, and the tech-lead playbook. **Done:** seven agents and
   eleven skills (lead playbook, engineering standards, a shared review
   protocol with a JSON report, correctness, security review, threat model,
   security research, maintainability, performance, qa-testing modes,
   vet-dependency). `config apply` also sets `worktree.baseRef: "head"` (so
   engineer worktrees start from the lead's commit), ignores
   `.claude/worktrees/`, and allows the registries the full tier contacts
   through the sandbox. The session-start context names the launcher, since
   the plugin has no `bin/`. qa-tester may read documents inside components.
7. react-feature-based pack, qa-tester, `/millennium:init-project`, `/millennium:adopt-project`,
   local mode. **Done:** the react-feature-based pack with `isolated` layers
   (features may not import each other, aliases included), `millennium
   discover`, `millennium-code-qc audit --proposal` as a dry run, and the `init-project` and `adopt-project` skills. Local mode was built in step 4f, qa-tester in step 6.
8. `plugin/` layout, launcher and engine lock, release workflow. **Done:**
   the marketplace file, the launcher (download, SHA-256 check on every run,
   kept beside the plugin where sandboxed agents can run but not change it),
   `code-qc.lock`, reproducible builds for six platforms
   (`scripts/release-build.sh`, verified byte-identical across build caches
   and checkout paths), and a release workflow without third-party actions
   that publishes only binaries matching the reviewed lock. Hooks moved to
   shell form for Windows (Git Bash).
9. pi support (after the user asked for it, 2026-11-13). **Done:** the pi
   package manifest, the extension (guard, gates when an agent finishes,
   session briefing, `millennium_agent`), `.pi/settings.json` and
   `.pi/extensions/` protected, `.pi/worktrees/` git-ignored by `config
   apply`, and `MILLENNIUM_PLUGIN_ROOT` for the engine command. The extension
   is a component of this repository's config, so the self-audit type-checks
   it and runs its tests with full coverage. The first engine release,
   `code-qc/v0.1.0`, includes these engine changes.
