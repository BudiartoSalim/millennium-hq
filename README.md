# millennium

millennium is a plugin for AI coding agents that makes quality and security
the default outcome of agent-driven development. It supports Claude Code
fully, pi through an extension, and any harness that reads Agent Skills.

- **Rules agents cannot talk their way past.** A gate engine
  (`millennium-code-qc`) checks every change: build, tests and coverage of
  changed lines, layer rules of your architecture, size and complexity
  limits, shell lint, and dependency approvals. In Claude Code and pi it runs
  from hooks. An agent that finishes with failing work is sent back, and
  agents cannot edit the rules they are checked against.
- **A team, not one agent.** A tech lead writes the spec and splits the work
  across parallel engineers in their own worktrees. Fresh-context reviewers
  (correctness, security, maintainability, performance) and a black-box QA
  tester then check the result. Every finding gets a recorded decision, and
  work is done only when the full gate passes.
- **Your project's rules, in your repository.** `.millennium/config.json`
  holds languages, conventions (Clean Architecture, feature-based React),
  limits and approved dependencies. Agents can only propose changes to it,
  and you apply them.

Supported languages: Go, TypeScript (Node and React), Python, and shell.

## Install

| Harness | Enforcement | Install |
|---|---|---|
| Claude Code | Full: hooks, gates, OS sandbox rules, agents | `/plugin marketplace add BudiartoSalim/millennium-hq`<br>`/plugin install millennium@millennium-hq` |
| pi | Hooks, gates and agents; no OS sandbox | `pi install git:github.com/BudiartoSalim/millennium-hq` |
| Codex, OpenCode, Gemini CLI, ... | None: skills and the engine command only | Copy `plugin/skills/*` into the harness's skills directory |

You need git, `curl` or `wget`, `sha256sum` or `shasum`, and the toolchains
of the languages you use. On Windows, you also need Git for Windows. There is
nothing to build: on first use, the plugin downloads the engine binary for
your platform and refuses it unless its SHA-256 matches the hash locked in
the plugin.

**[Installation guide](docs/install.md)** covers each harness in detail:
user or project installs, updating, the sandbox, offline use, and what a
skills-only setup gives up and how to make up for it.

## First steps

Commands below are for Claude Code. In pi, use `/skill:<name>` instead of
`/millennium:<name>`.

1. **Opt a repository in.** Run `/millennium:adopt-project` for existing code,
   or `/millennium:init-project` for a new project. The agent drafts the
   config with you and shows what the rules would find today. Then apply it
   yourself with the command it gives you:
   `! "<plugin>/scripts/millennium-code-qc" config apply`.
2. **Work as usual.** Every session in the repository is now checked.
3. **Lead a change** through the whole workflow:

   ```
   /millennium:tech-lead add a cart total endpoint that applies the active discount
   ```

4. **From a terminal**, check a branch or audit existing code:

   ```
   millennium-code-qc gate --base main
   millennium-code-qc audit --path services/api
   ```

Repositories without a config are left alone, so a user-wide install is safe.

## Status

Early development. The engine, the Claude Code plugin and the pi package are
built and tested; the first engine release is `code-qc/v0.1.0`. Agents and
skills have had little real-world use yet, so expect rough edges and please
report them.

## Documentation

- [Installation](docs/install.md): Claude Code, pi, and skills only.
- [Documentation](docs/README.md): configuration, commands, checks,
  conventions, hooks, agents.
- [Design](docs/design.md): why things are the way they are.
- [AGENTS.md](AGENTS.md): how this repository is developed.

## License

[MIT](LICENSE)
