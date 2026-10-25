# millennium documentation

millennium makes quality and security the default outcome of agent-driven
development. Rules that can be checked mechanically are enforced by a gate
engine the agents cannot talk their way past; judgement calls go to
fresh-context reviewer agents.

**Status: early development.** What exists today is the gate engine, a
command-line tool you can build and run yourself. The Claude Code plugin around
it (agents, skills, hooks, `config apply`) is not built yet; see the build
order in [design.md](design.md).

## Guides

| Document | What it covers |
|---|---|
| [Getting started](#getting-started) | Build the engine and check a repository |
| [Configuration](configuration.md) | Every setting of `.millennium/config.json`, single projects and monorepos |
| [Commands](cli.md) | `gate`, `audit`, `config validate`, `config explain`, exit codes, output formats |
| [Checks](checks.md) | What each check enforces, and how to fix its findings |
| [Clean Architecture](conventions/clean-architecture.md) | The layers, their import rules, and how to map an existing layout |
| [Design](design.md) | Why things are the way they are, and what is planned |

## Getting started

You need git and Go 1.25 or newer to build the engine. Release binaries, which
need no Go, are planned.

1. **Build the engine** from a clone of this repository:

   ```
   cd engine
   go build -o millennium .
   ```

   Put the resulting `millennium` binary somewhere on your `PATH`.

2. **Opt a repository in** by adding `.millennium/config.json` at its root. The
   smallest config for a Go service:

   ```json
   {
     "version": 1,
     "languages": ["go"],
     "conventions": ["clean-architecture"]
   }
   ```

   Check it with `millennium config validate`. For a monorepo, see
   [Configuration: monorepos](configuration.md#monorepos-components).

3. **Check your changes** before committing:

   ```
   millennium gate
   ```

   It compares the working tree with `HEAD` (staged, unstaged and untracked
   files alike) and exits 0 when everything passes, 2 when anything blocks.

4. **Audit existing code** to see what a refactor would need to fix:

   ```
   millennium audit
   millennium audit --path services/game-control
   ```

5. **When a finding surprises you**, ask which rules apply to the file:

   ```
   millennium config explain services/game-control/api/internal/domain/server.go
   ```

Until the plugin exists, nothing runs these automatically. Run `millennium gate`
yourself, or from your own git hook or CI if you want enforcement there; both
are outside what millennium ships.
