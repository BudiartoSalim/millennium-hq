# millennium documentation

millennium makes quality and security the default outcome of agent-driven
development. Rules that can be checked mechanically are enforced by
`millennium-code-qc`, a code quality engine the agents cannot talk their way
past; judgement calls go to fresh-context reviewer agents. These docs call it
"the engine".

**Status: early development.** Everything described here is built: the
gate engine, the hooks, `config apply`, the agents and skills, and the
release pipeline. The first engine release (`code-qc/v0.1.0`) has not been
published yet; until it is, build the engine yourself (step 2 below).

## Guides

| Document | What it covers |
|---|---|
| [Getting started](#getting-started) | Install the plugin and opt a repository in |
| [Configuration](configuration.md) | Every setting of `.millennium/config.json`, single projects and monorepos |
| [Commands](cli.md) | `gate`, `audit`, `discover`, `scope`, `config apply` and the rest, exit codes, output formats |
| [Checks](checks.md) | What each check enforces, and how to fix its findings |
| [Clean Architecture](conventions/clean-architecture.md) | The layers, their import rules, and how to map an existing layout |
| [Feature-based React](conventions/react-feature-based.md) | Shared code, isolated features and the app layer, and how to map an existing layout |
| [Hooks](hooks.md) | What the plugin's hooks enforce in every session, and their limits |
| [Agents and skills](agents.md) | The tech-lead workflow, each agent and skill, and the reviewer report format |
| [Releasing](releasing.md) | How engine releases are built, locked and published |
| [Design](design.md) | Why things are the way they are, and what is planned |

## Getting started

You need Claude Code, git, `curl` or `wget`, and `sha256sum` or `shasum`,
plus the toolchains of the languages you use: Go; `node` and the project's
dependencies for TypeScript (TypeScript 6 or older); Python 3.10 or newer;
`bash` and `shellcheck` for shell (or `"shell": {"lint": "off"}`). On Windows,
install Git for Windows: the hooks run through Git Bash.

1. **Install the plugin** in Claude Code:

   ```
   /plugin marketplace add BudiartoSalim/millennium-hq
   /plugin install millennium@millennium-hq
   ```

2. **The engine.** Once a release exists, the plugin's launcher downloads the
   engine for your platform on first use and refuses it unless its SHA-256
   matches the plugin's `code-qc.lock`; nothing to do. Before the first
   release, build it from a clone of this repository (Go 1.25 or newer):

   ```
   cd code-qc && go build -o millennium-code-qc .
   ```

   and start Claude Code with `MILLENNIUM_CODE_QC` set to the binary's
   absolute path. Without an engine every hook blocks with a message saying
   so.

3. **Opt a repository in.** In the repository, run `/millennium:adopt-project` for
   existing code or `/millennium:init-project` for a new project. The agent looks at
   the project, drafts `millennium.proposal.json` with you, and shows what the
   rules would find today. Then you apply it yourself, with the command the
   agent gives you (Claude Code's `!` prefix works):

   ```
   ! "<plugin>/scripts/millennium-code-qc" config apply
   ```

   It shows every file it writes and every way it loosens the rules, and asks
   you to confirm. Agents cannot run it. A new session picks up the gates.

4. **Work.** Every session in the repository is now checked: the hooks guard
   tool calls and send an agent back while its work fails the gates. To run
   a change through the full workflow (spec, parallel engineers, reviewers,
   QA, final gate):

   ```
   /millennium:tech-lead add a cart total endpoint that applies the active discount
   ```

   See [Agents and skills](agents.md).

5. **From your terminal**, the same engine checks changes and audits code
   (put a built binary on your `PATH`, or use the launcher's full path):

   ```
   millennium-code-qc gate                     # this change, against HEAD
   millennium-code-qc gate --base main         # a whole branch
   millennium-code-qc audit --path services/game-control
   millennium-code-qc config explain services/game-control/api/internal/domain/server.go
   ```

   See [Commands](cli.md) and [Checks](checks.md).
