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
| `SessionStart`, `SubagentStart` | In an opted-in repository, tells the session, and every subagent as it starts, how its work is checked, so agents do not discover the gates by tripping over them. The briefing includes the guide of each convention the repository uses (see [Convention guides](#convention-guides)). |
| `PreToolUse` (file edits, reads, searches, shell) | Guards the call (below). A denial reaches the agent with the reason. If the hook itself fails, times out or cannot start, the tool call is blocked (`onFailure: "block"`). |
| `Stop`, `SubagentStop` | Runs the **fast tier** of `millennium-code-qc gate` in the agent's working directory (an engineer's worktree for a subagent). While anything fails, the agent is sent back with the findings, up to 3 times in a row; after that the turn ends with a warning to you that the work is **not** done. The gates are never passed. |

The counter of blocked stops is kept per session and agent in the plugin's
data directory, where sandboxed agents cannot write.

## A working directory that is gone

Every event names the session's working directory, and the engine handles
it in the repository that contains that directory. When the directory no
longer exists, typically a worktree removed while a session was still in it,
the engine uses the nearest parent that does exist instead. For a worktree
under `.claude/worktrees/` that is the main checkout: the guard and the gates
apply as they do there, `.millennium/` stays protected, and the session can
`cd` back into the repository. A parent outside any git repository passes,
as any directory outside a repository does.

The engine moves up only past directories that do not exist at all, at most
32 levels, and only from an absolute path in clean form (no `.` or `..`
parts, no trailing separator); any other missing working directory blocks
every event, with a message asking you to change to a directory that
exists. A working directory that exists
but cannot be used, such as a file, a dangling symlink, or a directory git
cannot enter or refuses, blocks every event with git's message, as before.

## Convention guides

Every convention pack carries a `guide.md`: what belongs in each layer and
what reviewers look for, the part of a convention no gate can check (see
[Writing a convention](conventions/writing-a-convention.md)). The briefing
lists each component's conventions and includes their guides, so every
agent, the qa-tester included, knows the conventions without anyone pasting
them in.

Claude Code caps the context a hook adds at 10,000 characters and shows a
longer one only as a file path with a preview. So the briefing includes
whole guides while they fit, and names the rest with the command that
prints them (`millennium-code-qc convention show <name>`); a guide has at
most 8,000 characters. Keep guides short when a repository uses several
conventions.

## The guard

| Rule | Applies to | Denied |
|---|---|---|
| Protected paths | every agent, the main session included | writing `.millennium/` (the config, dependency approvals), `.claude/settings.json`, `.claude/settings.local.json`, and pi's `.pi/settings.json` and `.pi/extensions/` (a project extension runs inside pi and could switch millennium's off) |
| Acceptance tests | the software-engineer agent | writing under `paths.acceptanceTests` (default `tests/acceptance`): the engineer cannot change the tests that judge its work |
| Black-box testing | the qa-tester agent | reading code inside a component, and searching (Grep, Glob) inside a component or a directory that contains one; the acceptance tests, anything outside the components, and documents inside them (Markdown and text, JSON and YAML such as API descriptions, images, PDFs, README) stay readable by path |
| Rule changes | every agent | running `millennium-code-qc config apply`, and shell commands that look like they write a protected path |

The guard checks every path a tool names twice: as written, and with its
symlinks resolved (in the repository root, and in the longest part of the
path that exists, since a write may create the rest). So a symlink to a
protected file, or a path into the repository through a symlinked
directory, is denied like the protected path itself. A relative path is
taken from the event's working directory, even one that no longer exists,
never from the engine's own. A path outside the repository both ways is
left alone. A path the engine cannot map for any other reason, such as a
dangling symlink, a path below a file, or a directory it may not search,
blocks the call with the reason, because it might lead to a protected path.
So does a path with a `..` part: the OS follows a symlink before it takes
`..`, so such a path can lead elsewhere than it reads. Name the path
without `..`, such as by its absolute path.

The write rules (protected paths, and acceptance tests for the
software-engineer) apply in every worktree of the repository, not only the
one the agent is in: each path is also checked against the main checkout
and every linked worktree that `git worktree list` names, so an agent in
one worktree cannot change the rules of another, nor of the main checkout.
What qa-tester may read is decided in the worktree it works in.

Protected paths and the acceptance tests match without regard to case, on
every OS: on a case-insensitive file system (macOS, Windows, WSL on a
Windows drive) `.MILLENNIUM/config.json` is the config. On a
case-sensitive one this denies a few extra paths.

The local configs, which lie outside the repository, are matched the same
way: as written and with symlinks resolved, without regard to case, so a
symlink in the repository does not reach them. A path that cannot be
resolved there blocks the call.

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
  `millennium-code-qc scope` and the full gate from the commit the worktree
  was dispatched from, so committed work is checked too.
- **Other harnesses** get the portable layers only: `AGENTS.md`, the skills,
  and the engine as a CLI (see
  [Installation: skills only](install.md#other-harnesses-skills-only)). pi is
  the exception: millennium's pi extension runs the same guard, gates and
  briefing (see [pi](pi.md)), without a sandbox behind them.

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

Every hook runs `plugin/scripts/millennium-code-qc`, the launcher, with `/bin/sh`
(Git Bash's on Windows), so a launcher whose execute bit was lost in
packaging still runs. It runs, in order:

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

Agents' shells and your `!` commands run the launcher by its path, so it
must be executable. Millennium 0.3.0 and earlier shipped it without its
execute bit: running it by path fails with "Permission denied", and the
hooks then block every tool call. Update the plugin, or run
`chmod +x` on `scripts/millennium-code-qc` in the plugin's installed
directory.
