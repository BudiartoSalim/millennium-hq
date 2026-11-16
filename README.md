# millennium-hq

**Quality and security for agent-written code, enforced by checks the agents
cannot work around.**

millennium-hq is a plugin for AI coding agents. It turns one agent into a
small engineering team (a tech lead, engineers working in parallel,
fresh-context reviewers and a black-box tester) and puts a deterministic
gate engine behind them. When an agent says it is done, the engine checks
the work: build, tests and coverage of the changed lines, architecture
rules, complexity limits, secrets, risky constructs and the supply chain.
Work that fails goes back to the agent. The rules live in your repository,
agents can only propose changes to them, and you decide.

It supports Claude Code fully, pi through an extension, and any harness that
reads Agent Skills (Codex, OpenCode, Gemini CLI, ...) with the skills and the
engine as a command.

- [Why](#why)
- [Features](#features)
- [How it works](#how-it-works)
- [Install](#install)
- [Use](#use)
- [Configuration](#configuration)
- [FAQ](#faq)
- [Limitations](#limitations)
- [Documentation](#documentation)

## Why

Coding agents write a lot of code quickly, and you are asked to trust their
summary of it. Prompts and instruction files help, but an agent under
pressure skips tests, weakens a config, suppresses a warning or reviews its
own work kindly. millennium-hq moves everything that can be checked
mechanically out of the prompt and into a gate the agent cannot argue with.
It saves human attention for the judgement calls, which go to reviewers
who never saw the author's reasoning.

Three principles run through it:

- **Gates block, they do not warn.** A finding stops the work.
- **Fail closed.** A check that cannot run (a missing tool, an unreadable
  file, a registry that does not answer) blocks with a message saying why,
  never passes silently.
- **Agents never change the rules they are checked against.** They draft a
  proposal, and you apply it.

## Features

### A gate engine agents cannot talk past

`millennium-code-qc` is a single, dependency-free Go binary. It checks the
lines a change touched; `audit` checks the whole codebase. Seven gates:

| Gate | What it enforces |
|---|---|
| `toolchain` | The tools the checks need are installed. A missing tool blocks instead of skipping the check. |
| `structure` | Function limits (by default 50 lines, nesting depth 3, cognitive complexity 15) and the layer import rules of your conventions. |
| `shell` | `bash -n` and shellcheck on changed scripts. |
| `hygiene` | No secrets or credentials, no risky constructs (`eval`, shell execution, TLS verification off, unsafe deserialisation, raw HTML, TypeScript `any`), no suppression without a reason, no TODO without an issue link. |
| `build` | gofmt and `go vet`, the project's own `tsc` with strict mode not weakened, and mypy if you enable it. |
| `tests` | Changed logic comes with changed tests, the suites pass, and every changed line runs in a test. |
| `deps` | Every new package version is approved, versions and GitHub Actions and container images are pinned, npm install scripts are off, releases are at least 7 days old, OSV advisories, npm registry signatures. |

The **fast tier** (no test runs, no network) runs every time an agent
finishes. The **full tier** is the tech lead's final gate and what you run
yourself.

### Hooks that guard every session

In Claude Code and pi, the engine runs from hooks in every session in an
opted-in repository, whether or not you use the workflow:

- **When an agent finishes**, the fast gates run. While they fail, the agent
  is sent back with the findings, up to three times, and then you are
  warned that the work is not done.
- **On every tool call**, the guard denies:
  - writes to the rules (`.millennium/`) and the harness's settings;
  - an engineer editing the acceptance tests that judge its work;
  - the black-box tester reading the code it tests.
- **At session start**, the agent is told how the repository is checked,
  so it does not find out by tripping over the gates.
- **The OS sandbox.** In Claude Code, `config apply` writes sandbox rules,
  so a shell command cannot write the rules either.

### A team of agents

`/millennium:tech-lead <task>` runs a change from request to finished work:

1. **Design first.** The lead studies the problem before proposing anything,
   as in a design discussion with a tech lead. For an open-ended request
   ("a message broker with delayed delivery, like SQS"), it checks how the
   reference system really behaves, surveys how others build it, tests
   uncertain claims with throwaway spikes, and brings two or three options
   with trade-offs and a recommendation. You decide.
2. A **spec** with acceptance criteria and the chosen approach. If the change
   crosses a trust boundary, the security engineer adds a **threat model**.
3. **Work packages** with disjoint paths, implemented by up to three
   **engineers** in parallel, each in its own git worktree. Meanwhile, the
   QA tester writes acceptance tests from the spec.
4. **Integration**, with each branch checked against the paths it was given.
5. A **review panel**, each reviewer seeing only the spec and the diff:
   - correctness and maintainability on every change;
   - security and performance when the change carries those risks;
   - acceptance and exploratory testing.
6. **Triage.** Every finding is fixed, deferred or rejected, with a reason
   recorded in a decision log.
7. **The full gate.** The change is done only when it passes, and you get a
   summary.

| Agent | Role |
|---|---|
| `software-engineer` | Implements one work package with its unit tests, in its own worktree, or researches one design question for the lead. |
| `correctness-reviewer` | Checks the change against the spec: edge cases, error paths, test quality. Proves findings with failing tests. |
| `security-engineer` | Security review of the diff, threat models, and security research across the codebase. |
| `maintainability-reviewer` | Architecture fit, naming, cohesion, duplication, readability. |
| `performance-reviewer` | Complexity at real sizes, queries, memory, blocking I/O, limits. |
| `qa-tester` | Black-box acceptance, smoke and exploratory testing, without reading the implementation. |
| `tech-lead` | The same playbook as a background agent. |

You see every decision and can overrule any of them.

### Skills you invoke

| Skill | What it does |
|---|---|
| `/millennium:adopt-project` | Onboards an existing codebase. It discovers modules and languages, maps your layout onto a convention, drafts the config, and shows what the rules would find today. |
| `/millennium:init-project` | Sets up a new project: languages, conventions, layout, and the config. |
| `/millennium:tech-lead <task>` | Leads a change through the workflow above. |
| `/millennium:solution-design <problem>` | A design discussion on its own: research, options with trade-offs, and a recommendation, without building anything. Works in any repository. |
| `/millennium:vet-dependency <ecosystem> <name>@<version>` | Vets a package (need, age, maintainers, advisories, install scripts, transitive packages) and drafts its approval for you to apply. |

Nine more skills hold the agents' playbooks: engineering standards, the
review protocol and each review type, threat modelling, security research,
and QA. Every skill uses the portable Agent Skills format.

### Conventions and monorepos

- **Conventions are data, not code.** A convention is a pack: a
  `pack.json` with the rules the gates enforce (layers, what each may
  import, which need tests, function limits) and a `guide.md` every agent is
  given. Clean Architecture and feature-based React are built in. Your own
  convention, or a variation of a built-in one, is a pack in
  `.millennium/conventions/`: no engine change, no release. Run
  `millennium-code-qc convention new <name> --from <pack>` to start from a
  copy.
- **Agents know your conventions without being told.** The guides of the
  conventions a repository uses are part of every session's and every
  subagent's briefing.
- **Map, don't restructure.** You can map a convention's layers onto the
  layout you already have, and give a component its own limits, with a
  reason.
- **One config for a monorepo:** components map paths to languages and
  conventions, and the most specific path wins.
  `millennium-code-qc config explain <file>` shows which rules apply to a
  file and why.
- **Existing code is held to "no worse".** The gates check what a change
  touches, and a function that was already over a limit may stay over it
  but must not get worse. So you can adopt it on a large codebase today, and
  `audit` measures the whole codebase when you want to refactor.
- **Local mode** keeps the config on your machine, for repositories you
  contribute to but do not own.

### A verified engine

There is nothing to build. The plugin's launcher downloads the engine
release for your platform (Linux, macOS and Windows; amd64 and arm64), and
refuses it unless its SHA-256 matches the hash locked in the plugin. It
checks the hash again on every run. Releases are reproducible: the release
workflow rebuilds the tag and publishes only binaries that match the
reviewed lock.

### Languages

Go, TypeScript (Node and React), Python, and shell.

## How it works

```
 you ──> /millennium:tech-lead <task>
          │  research, options, your decision
          │  spec, work packages
          ├──> engineers (parallel worktrees) ──┐
          │                                     │ every tool call: guard
          │                                     │ every finish: fast gates
          ├──> reviewers + QA (fresh context) <─┘
          │  triage, decision log
          └──> full gate ──> done, with a summary for you

 .millennium/config.json   your rules; agents propose, you apply
 millennium-code-qc        the engine behind the hooks and the gates
```

The workflow is optional. Without the tech lead, a plain session in an
opted-in repository is still guarded and gated.

## Install

| Harness | What is enforced | Install |
|---|---|---|
| **Claude Code** | Everything: guard, gates, OS sandbox rules, agents | `/plugin marketplace add BudiartoSalim/millennium-hq`<br>`/plugin install millennium@millennium-hq` |
| **pi** | Guard, gates and agents; pi has no OS sandbox | `pi install git:github.com/BudiartoSalim/millennium-hq` |
| **Codex, OpenCode, Gemini CLI, ...** | Nothing: the skills and the engine as a command | Copy `plugin/skills/*` into the harness's skills directory |

**Requirements:**
- git, `curl` or `wget`, and `sha256sum` or `shasum`.
- The toolchains of the languages you use:
  - Go;
  - Node and the project's installed dependencies for TypeScript (TypeScript
    6 or older);
  - Python 3.10 or newer;
  - bash and shellcheck for shell.
- On Windows, Git for Windows.
- Network access to github.com the first time, to download the engine.

After installing, start a new session; there is no special way to start one.
Repositories without a config are left alone, so a user-wide install is
safe. You can also keep the plugin off everywhere except opted-in
repositories.

The [installation guide](docs/install.md) covers each harness in detail:
- user or project installs, and keeping the plugin off where you don't use it;
- updating and removing;
- the sandbox;
- how it works alongside your own CLAUDE.md and skills;
- offline use;
- what a skills-only setup gives up, and how to make up for it.

## Use

Commands below are for Claude Code. In pi, use `/skill:<name>` instead of
`/millennium:<name>`.

**1. Opt a repository in.**

```
/millennium:adopt-project        existing code
/millennium:init-project         a new project
```

The agent drafts `millennium.proposal.json` with you and shows what the
rules would find. Then you apply it yourself, with the command it gives you:

```
! "<plugin>/scripts/millennium-code-qc" config apply
```

`config apply` lists every file it writes and every way the proposal
loosens the rules, then asks you to confirm. It writes:
- `.millennium/config.json`;
- sandbox rules in `.claude/settings.json`;
- a "Quality gates" section in `AGENTS.md`;
- `.gitignore` entries.

Commit them and start a new session.

**2. Work as usual.** Every session is now guarded and gated, whether you ask
for a quick fix or a feature.

**3. Lead larger changes** through the full workflow:

```
/millennium:tech-lead add a cart total endpoint that applies the active discount
```

**4. Approve dependencies.** When the `deps` gate reports an unapproved
package, vet it with `/millennium:vet-dependency npm express@4.21.2`. It
drafts the approval into a proposal, and you apply it.

**5. Check from your terminal**, or in CI or a pre-commit hook:

```
millennium-code-qc gate                          # this change, against HEAD
millennium-code-qc gate --base main              # a whole branch
millennium-code-qc audit --path services/api     # existing code, for a refactor
millennium-code-qc config explain src/domain/order.go
```

Exit status 0 means pass. Everything else blocks. Add `--format json` for
machine-readable reports. The launcher is at
`<plugin>/scripts/millennium-code-qc`; link it onto your `PATH` if you use it
often.

## Configuration

A single project:

```json
{
  "version": 1,
  "languages": ["go"],
  "conventions": ["clean-architecture"]
}
```

A monorepo:

```json
{
  "version": 1,
  "components": [
    { "paths": ["services/*/api"], "languages": ["go"], "conventions": ["clean-architecture"] },
    { "paths": ["web"], "languages": ["typescript"], "conventions": ["react-feature-based"],
      "test": { "command": ["npm", "run", "test:coverage"], "lcov": "lcov.info" } }
  ],
  "exclude": ["**/node_modules/**", "**/dist/**"]
}
```

Every typo is an error, never a silently ignored setting. A rule can be
switched off, but only with a written reason, and every report lists it;
rules that report a check failing to run cannot be switched off. A JSON
Schema is in `schema/` for editor completion. See [Configuration](docs/configuration.md).

## FAQ

**Does it slow down sessions in my other repositories?**
No. Outside an opted-in repository, the hooks only check for a config and
exit. If you want the plugin completely absent there, disable it for your
user and let `config apply` enable it per repository; see
[Install](docs/install.md#user-or-project-install).

**My CLAUDE.md says something different. Which wins?**
For anything the engine enforces, the gates win, always. For workflow and
judgement, such as "commit straight to main", the model weighs both, and
nothing checks it. Keep your instructions consistent with the config; see
[Alongside your own instructions](docs/install.md#alongside-your-own-instructions).

**What if a gate is wrong?**
The agents are told never to work around a gate and to bring the case to
you instead. Then change the config through a proposal, or waive one line
with a reason (`millennium:allow <rule> -- <reason>`), which reviewers see.

**Can an agent just edit the config?**
Not in Claude Code with the sandbox on: the guard denies it and the
sandbox denies shell writes. In pi, shell writes are caught by pattern
matching only. In a skills-only setup nothing stops it, so review changes
to `.millennium/`.

**Does it need the network?**
It needs github.com once, to download the engine. The full tier's
supply-chain checks also query the npm registry, the Go module proxy, PyPI
and OSV. To work offline, set `MILLENNIUM_CODE_QC` to an engine binary
you built or vetted.

**Does it work on Windows?**
Yes, through Git Bash. That path has had less testing than Linux and
macOS.

## Limitations

- **Early stage.** The engine has a full test suite and audits its own
  source. The agents and skills have had little real-world use yet, so
  expect rough edges, and please report them.
- **Lockfiles read:** `package-lock.json`, `go.sum` and `requirements*.txt`.
  pnpm, yarn, uv, poetry and Pipfile lockfiles get the pinning checks but
  not the approvals.
- **TypeScript 7** removed the compiler API the TypeScript checks use, so
  they need TypeScript 6 or older.
- **Function limits do not apply to shell scripts yet**; the
  maintainability reviewer covers them.
- **Shell commands are matched best-effort.** Claude Code's OS sandbox is
  the real protection for the rules. pi has none, and other harnesses get
  no enforcement at all.

## Documentation

| | |
|---|---|
| [Installation](docs/install.md) | Claude Code, pi, and skills only |
| [Getting started](docs/README.md#getting-started) | The short path, and the index of all docs |
| [Configuration](docs/configuration.md) | Every setting, monorepos, test recipes, dependency approvals, local mode |
| [Commands](docs/cli.md) | `gate`, `audit`, `discover`, `scope`, `config` and exit codes |
| [Checks](docs/checks.md) | Every rule, what it enforces, and how to fix it |
| [Hooks](docs/hooks.md) | What runs in every session, and its limits |
| [Agents and skills](docs/agents.md) | The workflow, each agent and skill, the reviewer report format |
| [pi](docs/pi.md) | Using millennium-hq in pi |
| [Conventions](docs/conventions/) | Clean Architecture and feature-based React, and [writing your own](docs/conventions/writing-a-convention.md) |
| [Design](docs/design.md) | Why things are the way they are |

## Contributing

millennium-hq practises what it enforces: the engine audits its own source
on every check. See [AGENTS.md](AGENTS.md) for the layout and the rules, and
run `bash scripts/verify.sh` before you send a change.

## License

[MIT](LICENSE)
