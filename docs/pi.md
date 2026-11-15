# millennium in pi

[pi](https://github.com/earendil-works/pi) is a terminal coding agent with an
extension API but no subagents and no sandbox. millennium supports it through
a pi package: the same engine, skills and agent definitions as the Claude
Code plugin, wired in by a small extension (`pi/src/`). This page covers what
works the same, what differs, and what pi cannot enforce.

Tested against pi 1.5.2.

## Install

You need pi (Node.js 22.19 or newer), git, `curl` or `wget`, and `sha256sum`
or `shasum`, plus your languages' toolchains as for Claude Code (see
[Getting started](README.md#getting-started)). On Windows, install Git for
Windows: the extension runs the engine's launcher with Git Bash.

```
pi install git:github.com/BudiartoSalim/millennium-hq
```

This installs for your user. Add `-l` to install it for the project only
(`.pi/settings.json`, which pi reads once you trust the project). To pin a
version, add `@<commit>` to the source.

The engine is fetched the same way as in Claude Code: on the first session,
the launcher downloads the release named in `plugin/code-qc.lock`, refuses it
unless its SHA-256 matches, and keeps it beside the package. To use an engine
you built, set `MILLENNIUM_CODE_QC` to its absolute path before starting pi.

## Use

Skills are pi skill commands: `/skill:<name>` where Claude Code has
`/millennium:<name>`.

1. **Opt a repository in** with `/skill:init-project` (new project) or
   `/skill:adopt-project` (existing code). Apply the proposal yourself with
   the command the agent gives you; pi's `!` prefix runs it from the session.
   `config apply` also writes Claude Code's settings, which pi ignores.
2. **Work.** Every session in an opted-in repository is checked: the guard
   blocks tool calls that break the rules, and an agent that finishes while
   the fast gates fail is sent back with the findings.
3. **Lead a change** with `/skill:tech-lead <task>`. In pi the tech lead runs
   in your session; it starts the other agents with the `millennium_agent`
   tool.

## How it works

The extension translates pi's events into the engine's hook protocol (the
format Claude Code's hooks use) and runs `millennium-code-qc hook` through
the plugin's launcher, exactly as the Claude Code hooks do.

| pi event | What happens |
|---|---|
| `session_start` | The engine briefs an opted-in session (the same text as Claude Code's SessionStart hook). The briefing, plus how to read the skills in pi, becomes a `millennium` section of every run's system prompt. |
| `tool_call` | The guard checks `read`, `edit`, `write`, `grep`, `find`, `ls`, `bash` and `powershell`, including calls a codemode script makes. A call the guard cannot check (no engine, a timeout, unexpected output) is blocked. |
| `agent_before_settle` | When an agent finishes, the fast tier of the gates runs. While it fails, the findings go back to the agent and pi continues the run, up to 3 times in a row; then you get a warning that the work is **not** done. |

The guard resolves each tool's path the way pi does (`@` prefix, `~`,
`file://` URLs, relative paths) and writes the absolute path back into the
call, so the tool touches the file that was checked.

pi's `ls` is guarded like a search: the qa-tester agent may not list a
component's files any more than it may search them.

### Agents: the `millennium_agent` tool

pi has no subagents, so the extension adds one tool that runs millennium's
agents as separate pi processes (`pi --print --mode json`). The agents come
from the same files as in Claude Code (`plugin/agents/*.md`): each process
gets the agent's instructions and preloaded skills as an appended system
prompt, the pi tools matching the agent's Claude Code tools, and its role.

- **Fresh context.** An agent sees only the task text.
- **Parallel.** Up to 4 tasks in one call run at once.
- **Worktrees.** The agents Claude Code isolates (software-engineer,
  correctness-reviewer, qa-tester) get a git worktree at
  `.pi/worktrees/<id>` on a branch `millennium/<id>`, created from `HEAD`.
  Commit before starting them; `config apply` git-ignores `.pi/worktrees/`.
- **Roles.** Each process carries `MILLENNIUM_AGENT` and `MILLENNIUM_AGENT_ID`
  in its environment, and the guard applies that role's rules: the engineer
  cannot change acceptance tests; the qa-tester cannot read code.
- **Resume.** The result names each agent's id; `resume` with that id
  continues the agent with its context (Claude Code's SendMessage), one task
  at a time. Sessions are kept in `~/.pi/agent/millennium/state/agents/`.
- **Every agent is guarded.** Each process loads this extension with
  `--extension`, whatever your pi settings say, and gets no agent tool of its
  own.
- **The model** is your session's model.

The tech-lead agent is not offered: in pi the tech lead is the `tech-lead`
skill in your session.

## Differences from Claude Code

| | Claude Code | pi |
|---|---|---|
| Skills | `/millennium:<skill>` | `/skill:<skill>` |
| Agents | subagents | the `millennium_agent` tool (separate pi processes) |
| Protected paths for shell commands | the OS sandbox denies writes | best-effort command matching only |
| Web access for agents | WebFetch, WebSearch | none built in; agents use `curl` |
| Engine command in the skills | `${CLAUDE_PLUGIN_ROOT}` is filled in | the session's `millennium` section names the command |
| Background agents | yes | the tool waits for its tasks to finish |

The engine treats `.pi/settings.json` and `.pi/extensions/` as protected
paths in every harness: a project extension runs inside pi and could switch
millennium's off.

## What pi cannot enforce

- **No sandbox.** pi does not restrict what a shell command can write, so
  `.millennium/`, `.pi/settings.json` and `.pi/extensions/` are protected from
  shell commands only by pattern matching, which a determined command can
  evade. The edit and write tools are blocked reliably. For hard guarantees,
  run pi in a container or VM with those paths read-only (see pi's
  `docs/containerization.md`). The tech lead's final full gate
  (`millennium-code-qc gate --tier full --require-config`) still decides
  whether work is done.
- **Your own extensions and settings.** An extension you install runs inside
  pi with full access and can change any tool call after the guard has
  checked it. Files outside the repository, such as `~/.pi/agent/`, are not
  protected.
- **Only built-in tools are guarded.** A tool another package adds that
  writes files is not checked (as in Claude Code, where only the matched
  tools are).
- **Gates that cannot run do not block finishing.** If the engine cannot run
  when an agent finishes, you are warned and the work is marked unchecked,
  because pi cannot send an agent back to fix a missing engine.

## Testing the extension

`scripts/verify.sh` type-checks the extension and runs its tests
(`node --test`, Node's standard library, with fakes for pi and the engine),
and the engine's self-audit requires every line of it to run in a test.

End-to-end runs against a real pi use pi's scripted test model (`fauxProvider`
from `@earendil-works/pi-ai`) loaded as a second extension, with
`MILLENNIUM_CODE_QC` set to a local engine build. They are run by hand: pi is
not a dependency of this repository. Run pi with stdin closed (`< /dev/null`):
in print mode it waits for piped stdin to end.
