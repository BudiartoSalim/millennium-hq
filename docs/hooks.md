# Hooks

The Claude Code plugin wires the engine into every session through hooks
(`plugin/hooks/hooks.json`). They run in every session where the plugin is
enabled, for the main session and every subagent alike, whether or not the
tech lead is used, and they do nothing in a repository without a config.

All hooks run `millennium-code-qc hook`, which reads the event Claude Code sends on
stdin and answers on stdout. Hooks run outside the agents' sandbox, with your
access, which is why the engine treats every repository file and git output
as untrusted.

## What each hook does

| Event | What happens |
|---|---|
| `SessionStart` | In an opted-in repository, tells the session how its work is checked, so agents do not discover the gates by tripping over them. |
| `PreToolUse` (file edits, reads, searches, shell) | Guards the call (below). A denial reaches the agent with the reason. If the hook itself fails, times out or cannot start, the tool call is blocked (`onFailure: "block"`). |
| `Stop`, `SubagentStop` | Runs the **fast tier** of `millennium-code-qc gate` in the agent's working directory (an engineer's worktree for a subagent). While anything fails, the agent is sent back with the findings, up to 3 times in a row; after that the turn ends with a warning to you that the work is **not** done. The gates are never passed. |

The counter of blocked stops is kept per session and agent in the plugin's
data directory, where sandboxed agents cannot write.

## The guard

| Rule | Applies to | Denied |
|---|---|---|
| Protected paths | every agent, the main session included | writing `.millennium/` (the config, dependency approvals), `.claude/settings.json`, `.claude/settings.local.json` |
| Acceptance tests | the engineer agent | writing under `paths.acceptanceTests` (default `tests/acceptance`): the engineer cannot change the tests that judge its work |
| Black-box testing | the qa-tester agent | reading code inside a component, and searching (Grep, Glob) inside a component or a directory that contains one; the acceptance tests, anything outside the components, and documents inside them (Markdown and text, JSON and YAML such as API descriptions, images, PDFs, README) stay readable by path |
| Rule changes | every agent | running `millennium-code-qc config apply`, and shell commands that look like they write a protected path |

If the config cannot be read, every file change is denied until a human fixes
it, because the protected paths are unknown; reads stay allowed.

## What the hooks cannot do

- **Shell commands are matched best-effort.** A command can write a file in
  ways no pattern recognises. The OS sandbox is what stops that: `millennium
  config apply` writes sandbox rules that deny writes to the protected paths
  for every command, and Claude Code's sandbox already protects `.git/config`,
  `.git/hooks` and its own settings.
- **A Stop hook that cannot run does not block.** Claude Code cannot send an
  agent back to repair a hook that will not start. If the engine is missing,
  agents can stop with unchecked work, which is why the tech lead's final
  `millennium-code-qc gate --tier full --require-config` run is the last word.
- **Committed work in an engineer's worktree.** Stop hooks check what differs
  from `HEAD`. Before merging an engineer's branch, the tech lead runs
  `millennium-code-qc scope` and the full gate against the branch's base.
- **Other harnesses** get the portable layers only: `AGENTS.md`, the skills,
  and the engine as a CLI.

## Recommended settings

Keep the sandbox on and unsandboxed retries off, so a protected path cannot be
written by a command the hooks did not recognise:

```json
{
  "sandbox": {
    "enabled": true,
    "allowUnsandboxedCommands": false,
    "filesystem": { "denyWrite": ["./.millennium"] }
  }
}
```

`millennium-code-qc config apply` writes these into the project's
`.claude/settings.json`, together with enabling the plugin for the project.

## Finding the engine

Every hook runs `plugin/scripts/millennium-code-qc`, the launcher, through the shell
(Git Bash on Windows). It runs, in order:

1. the binary named by `MILLENNIUM_CODE_QC`, if set: one you built or vetted;
2. the release named in the plugin's `code-qc.lock` for your platform, kept
   in the plugin's `.code-qc/` directory (or the plugin's data directory),
   after checking its SHA-256 against the lock, on every run;
3. otherwise it downloads that release, refuses it unless the SHA-256
   matches, and keeps it for next time.

The first session after installing or updating the plugin downloads the
engine in its SessionStart hook. Agents' sandboxed shells can read and run
the kept binary but cannot change it. A binary that no longer matches the
lock is refused, never replaced silently; delete it to download it again.
Any failure blocks, with a message saying what to do. See
[Releasing](releasing.md) for how the lock is made.
