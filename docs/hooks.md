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
| Protected paths | every agent, the main session included | writing `.millennium/` (the config, dependency approvals), `.claude/settings.json`, `.claude/settings.local.json`, and pi's `.pi/settings.json` and `.pi/extensions/` (a project extension runs inside pi and could switch millennium's off), wherever they lie |
| Acceptance tests | the software-engineer agent | writing under `paths.acceptanceTests` (default `tests/acceptance`), in any worktree of the repository: the engineer cannot change the tests that judge its work |
| Black-box testing | the qa-tester agent | reading code inside a component, and searching (Grep, Glob) inside a component or a directory that contains one, in any worktree of the repository; the acceptance tests, anything outside the components, and documents inside them (Markdown and text, JSON and YAML such as API descriptions, images, PDFs, README) stay readable by path |
| Rule changes | every agent | running `millennium-code-qc config apply`, and shell commands that look like they write a protected path, or that put, move or remove something at `.pi` or `.claude`, which hold protected paths: such a name as an operand of `ln`, `mv`, `cp`, `rm`, `install` or `git mv` (`ln -s conf .pi`, `mv conf .claude`, `rm -rf .claude`) |

The guard checks every path a tool names twice: as written, and with its
symlinks resolved in the longest part of the path that exists (a write may
create the rest). So a symlink to a protected file, or a path through a
symlinked directory, is denied like the protected path itself. A relative
path is taken from the event's working directory, even one that no longer
exists, never from the engine's own. A path the engine cannot resolve,
such as one through a dangling symlink, below a file, or in a directory it
may not search, blocks the call with the reason, because it might lead to
a protected path. So does a path with a `..` part: the OS follows a
symlink before it takes `..`, so such a path can lead elsewhere than it
reads. Name the path without `..`, such as by its absolute path. On
Windows, so does a path that is neither absolute nor plainly relative: a
rooted one (`\repo\file`, on the current drive) or a drive-relative one
(`C:file`, from that drive's own working directory), which the OS does not
read from the session's directory. Name such a path in full.

On Windows a directory junction (`mklink /J`, which needs no privilege)
is followed like a symlink, though Go's own symlink resolution walks
through junctions without following them. A reparse point that redirects
a name but whose target the engine cannot read blocks the call with the
reason, since where a path through it leads is unknown. The engine also
spells each resolved path by its long names, so a short 8.3 name
(`PROGRA~1`) is no other spelling of a protected path. A volume mount
point, or a junction to a volume, reads as the volume's device path
(`\\?\Volume{...}\`); the engine goes on from where Windows mounts that
volume: the mount point itself for a second disk, VHD or Dev Drive
mounted at a folder such as `C:\dev`, so a repository there works as on
any drive. A volume Windows lists no drive letter or folder for, and any
other device path (`\\?\C:\...`, `\??\...`), which no other spelling of
the same file matches, blocks.

On Linux and macOS, a path through `/proc/self`, `/proc/thread-self`,
`/proc/<pid>`, `/dev/fd`, `/dev/stdin`, `/dev/stdout` or `/dev/stderr` is
unknown: each leads somewhere else for each process that opens it, so the
engine, in its own process, cannot tell where it leads for pi or a tool.
A tool's path through one blocks the call, and a protected location that
leads through one blocks every write (see below).

A protected path is protected wherever it lies: `.millennium` as any
segment of a path, and `.claude/settings.json`, `.claude/settings.local.json`,
`.pi/settings.json` and `.pi/extensions` as segments one after another
anywhere in it. So an agent in an opted-in checkout writes the rules of no
checkout: not those of the repository, of any of its worktrees (the main
checkout, a linked worktree, one moved with plain `mv` that git no longer
lists, or one whose path git cannot list clearly), nor of any other
checkout on the machine. A directory of yours that merely has such a
segment in its path is protected too.

Each segment is read as Windows opens it, on every OS: Windows strips a
name's trailing dots and spaces, and opens `name:stream` (`::$DATA`
included) as the file `name`, so `.claude\settings.local.json.`,
`.pi\extensions.\x.ts` and `settings.json::$DATA` are the protected paths
themselves. Elsewhere this denies a few more names.

A protected location reached through a symlink is protected by its real
path too. If `.pi` is a symlink to `conf`, pi loads `conf/extensions` as
the project's extensions, so a write of `conf/extensions/x.ts` is denied
like one of `.pi/extensions/x.ts`; the same holds for a symlinked
`.millennium` or `.claude`, and for a settings file that is itself a
symlink. For every write, whoever makes it, the engine resolves each
protected location of the acting checkout and of every worktree
`git worktree list` names, and denies writes inside where they lead: from
a linked worktree, the main checkout's `conf/extensions/x.ts` is denied
as well as the worktree's own.

A protected location that leads nowhere blocks nothing. A dangling
symlink, such as a committed relative link that dangles in a worktree, is
followed as it reads, and what it would lead to is protected: with `.pi`
a dangling link to `conf`, creating `conf/extensions/x.ts` is denied. A
location below a file (a file named `.claude`), and one through more
links than the OS follows in one path (40 on Linux, 63 reparse points on
Windows), such as a link to itself, reach nothing: the OS creates nothing
there, so nothing needs protecting. Each call resolves the locations
anew, so once such a location leads somewhere, that place is protected.

A location whose lead is unknown blocks every write, in every checkout,
until it can be resolved: one through a directory the engine may not
search (`chmod 000` on a worktree), a link it cannot read, or a
per-process path. Such a location might lead anywhere, the main
checkout's `conf/` included, so no write can be checked. The message
names the location and the way out: restore access to it, fix or remove
the link it leads through, or remove a linked worktree you no longer need
(`git worktree remove --force <path>`). Reads are not affected.
A worktree whose root reaches nothing is not looked at, and one whose
root cannot be resolved otherwise blocks every write (see below).

The guard acts only for a session whose own checkout is opted in (it has a
config, in the repository or a local one). A session in a checkout that is
not is not guarded at all, even when that checkout lies inside or beside
an opted-in one: a worktree checked out at a commit from before the
repository opted in, a submodule, or a nested repository. From such a
session a file tool can write the opted-in checkout's
`.millennium/config.json`, so the protection above holds for sessions in
an opted-in checkout only. Start agents in an opted-in checkout.

The role rules (acceptance tests, black box) apply in every worktree of
the repository: each path is placed in the worktree it lies in, the
innermost one where worktrees nest (Claude Code's lie inside the main
checkout's `.claude/worktrees/`), and checked there; a searched directory
that holds a whole worktree reaches all of it. They apply the acting
session's config, the rules that session is checked against, to every
worktree: another worktree's acceptance tests are those under the acting
session's `paths.acceptanceTests`, and its components those of the acting
session's config. The worktrees are those `git worktree list` names, asked
for every write (whose protected locations in each worktree may lead where
it lands) and for the qa-tester's reads, in an opted-in repository whose
git directory has linked worktrees; other reads never list them. A list
that cannot be read unambiguously, such as one with a worktree whose path
holds a newline, blocks the call with the reason, every write included,
until you remove that worktree; the list may name no more worktrees than
git keeps records for (entries under `.git/worktrees` with a `gitdir`
file git can read at least one byte of, the ones git lists). A worktree
whose root reaches nothing (its parent replaced by a dangling symlink, a
file, or a link loop) holds no file the OS can open, so it is compared
as git names it and blocks nothing; nor does one whose directory is
simply gone. A
worktree whose root cannot be resolved for any other reason, such as
`chmod 000` on the `.claude/worktrees` that holds it, a link on its way
that cannot be read, or a link to a per-process or device path, may hold
protected locations that lead anywhere, the main checkout's `conf/`
included: every write blocks, and so does every read a role rule checks
(the qa-tester's), until it can be resolved. The message names the
worktree, why it cannot be resolved, and the way out: restore access to
it (such as with `chmod`), or, if it is a linked worktree you no longer
need, remove it (`git worktree remove --force <path>`, or
`git worktree prune` once its directory is gone). Other reads are not
affected. A protected location below a root that resolves blocks while
its lead is unknown (see above).

The role rules reach only the worktrees `git worktree list` names. A
worktree moved with plain `mv` rather than `git worktree move` is listed
at the path it left (as prunable), so its new path lies outside the role
rules: the software engineer can write the acceptance tests there, and
qa-tester can read its code. Its protected paths stay protected all the
same, since they are matched wherever they lie. Move worktrees with
`git worktree move`, or run `git worktree repair` after a plain `mv`.

Every path rule matches without regard to case, on every OS: on a
case-insensitive file system (macOS, Windows, WSL on a Windows drive)
`.MILLENNIUM/config.json` is the config, and `SRC/Main.go` is code in the
`src` component. On a case-sensitive one this denies a few extra paths.
The one exception is the acceptance tests for the qa-tester, which only
allow: they match as spelled, so another spelling never allows more.

The local configs, which lie outside the repository, are matched the same
way: as written and with symlinks resolved, without regard to case, so a
symlink in the repository does not reach them. Paths are compared
absolute, so `MILLENNIUM_LOCAL_DIR`, when set, must be an absolute path; a
relative one blocks every event with a message saying so.

If the config cannot be read, every file change is denied until a human fixes
it, because the protected paths are unknown; reads stay allowed.

## What the hooks cannot do

- **Shell commands are matched best-effort.** A command can write a file in
  ways no pattern recognises. The OS sandbox is what stops that: `millennium
  config apply` writes sandbox rules that deny writes to the protected paths
  for every command, and Claude Code's sandbox already protects `.git/config`,
  `.git/hooks` and its own settings. The match is also coarse the other
  way: a command that names a protected path anywhere, and a word that
  can write (`>`, `cp`, `mv`, `ln`, `rm`, ...) anywhere, is denied, even
  when the write goes elsewhere. Split such a command in two. A word
  ending in `.pi` or `.claude` (`sub/.pi`, `~/.claude/`) is denied only as
  an operand of `ln`, `mv`, `cp`, `rm`, `install` or `git mv`, read past
  variable assignments and prefixes such as `sudo` and `env`, and in a
  script run with `sh -c` or `bash -c`, so `ls -la .claude 2>/dev/null`
  and `du -sh ~/.claude 2>&1` pass. The command is split as the shell
  splits it (control operators, subshells, quotes, redirections), but not
  every form is looked into: a command substitution inside double quotes,
  a prefix with an option value (`sudo -u x rm ...`), or a script file is
  not.
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
